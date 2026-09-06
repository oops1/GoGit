package app

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/config"
	gitconfig "github.com/oops1/gogit/internal/gitcore/config"
	"github.com/oops1/gogit/internal/gitcore/credential"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/gitcore/transport"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/credentials"
	"github.com/oops1/gogit/internal/ui/unlock"
	"github.com/oops1/gogit/internal/vault"
)

func loadRawConfig(t *testing.T, content string) *gitconfig.Config {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", "")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := gitconfig.Load(gitconfig.Options{GitDir: dir, NoSystem: true})
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

type fakeCredentialHelper struct {
	name      string
	answer    credential.Answer
	hasAnswer bool
	getErr    error
	stored    []credential.Answer
	storeErr  error
	erased    int
	eraseErr  error
}

func (f *fakeCredentialHelper) Name() string { return f.name }

func (f *fakeCredentialHelper) Get(context.Context, credential.Query) (credential.Answer, bool, error) {
	if f.getErr != nil {
		return credential.Answer{}, false, f.getErr
	}
	if !f.hasAnswer {
		return credential.Answer{}, false, nil
	}
	return credential.Answer{
		Username: f.answer.Username,
		Password: append([]byte(nil), f.answer.Password...),
	}, true, nil
}

func (f *fakeCredentialHelper) Store(_ context.Context, _ credential.Query, a credential.Answer) error {
	f.stored = append(f.stored, credential.Answer{Username: a.Username, Password: append([]byte(nil), a.Password...)})
	return f.storeErr
}

func (f *fakeCredentialHelper) Erase(context.Context, credential.Query) error {
	f.erased++
	return f.eraseErr
}

func stubCredentialChain(t *testing.T, helpers ...*fakeCredentialHelper) {
	t.Helper()
	prev := buildCredentialChain
	chain := make(credential.Chain, len(helpers))
	for i, h := range helpers {
		chain[i] = h
	}
	buildCredentialChain = func(*App, string) (credential.Chain, credential.Query) {
		return chain, credential.Query{Host: "example.com"}
	}
	t.Cleanup(func() { buildCredentialChain = prev })
}

const testVaultPassword = "correct horse battery staple"

func failIfCredentialsDialogOpens(t *testing.T) {
	t.Helper()
	prev := newCredentialsView
	newCredentialsView = func(widget.ModalShower, credentials.Request) (*credentials.View, error) {
		t.Fatal("credentials dialog must not open")
		return nil, nil
	}
	t.Cleanup(func() { newCredentialsView = prev })
}

func stubCredentialsDialog(t *testing.T, a *App, respond func(req credentials.Request) (credentials.Result, bool)) {
	t.Helper()
	prev := newCredentialsView
	newCredentialsView = func(eng widget.ModalShower, req credentials.Request) (*credentials.View, error) {
		view, err := prev(eng, req)
		if err != nil {
			return nil, err
		}
		result, ok := respond(req)
		a.Post(func() {
			if ok {
				view.OnOK(result)
			} else {
				view.OnCancel()
			}
		})
		return view, nil
	}
	t.Cleanup(func() { newCredentialsView = prev })
}

func stubUnlockDialog(t *testing.T, a *App, attempts []unlock.Result) {
	t.Helper()
	prev := newUnlockView
	call := 0
	newUnlockView = func(eng widget.ModalShower, req unlock.Request) (*unlock.View, error) {
		view, err := prev(eng, req)
		if err != nil {
			return nil, err
		}
		var result unlock.Result
		cancelled := call >= len(attempts) || attempts[call].Password == nil
		if !cancelled {
			result = attempts[call]
		}
		call++
		a.Post(func() {
			if cancelled {
				view.OnCancel()
			} else {
				view.OnOK(result)
			}
		})
		return view, nil
	}
	t.Cleanup(func() { newUnlockView = prev })
}

func createTestVault(t *testing.T, path string) *vault.Vault {
	t.Helper()
	v, err := vault.Create(context.Background(), vault.Options{Path: path}, vault.NewPasswordUnlocker([]byte(testVaultPassword), vault.TestSlotParams()))
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestCredentialsSourceReturnsVaultCredentialWithoutShowingADialog(t *testing.T) {
	a := newTestApp(t)
	failIfCredentialsDialogOpens(t)
	v := createTestVault(t, a.paths.VaultFile())
	a.vaultInst = v
	if err := v.SetCredential(vault.Credential{Resource: "example.com/org/repo", Username: "alice", Secret: []byte("token")}); err != nil {
		t.Fatal(err)
	}

	src := a.credentialSource()
	got, err := src.Credentials(context.Background(), "example.com/org/repo", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Username != "alice" || string(got.Password) != "token" {
		t.Fatalf("credentials = %+v", got)
	}
}

func TestCredentialsSourceMatchesTheLongestResourcePrefix(t *testing.T) {
	a := newTestApp(t)
	failIfCredentialsDialogOpens(t)
	v := createTestVault(t, a.paths.VaultFile())
	a.vaultInst = v
	if err := v.SetCredential(vault.Credential{Resource: "example.com", Username: "generic", Secret: []byte("generic-token")}); err != nil {
		t.Fatal(err)
	}
	if err := v.SetCredential(vault.Credential{Resource: "example.com/org", Username: "specific", Secret: []byte("specific-token")}); err != nil {
		t.Fatal(err)
	}

	src := a.credentialSource()
	got, err := src.Credentials(context.Background(), "example.com/org/repo", false)
	if err != nil {
		t.Fatal(err)
	}
	if got.Username != "specific" {
		t.Fatalf("username = %q, want the longest matching prefix", got.Username)
	}
}

func TestCredentialsSourceRetrySkipsTheVaultButPrefillsTheUsername(t *testing.T) {
	a := newTestApp(t)
	v := createTestVault(t, a.paths.VaultFile())
	a.vaultInst = v
	if err := v.SetCredential(vault.Credential{Resource: "example.com", Username: "alice", Secret: []byte("stale")}); err != nil {
		t.Fatal(err)
	}
	var seenUsername string
	var seenRetry bool
	stubCredentialsDialog(t, a, func(req credentials.Request) (credentials.Result, bool) {
		seenUsername = req.Username
		seenRetry = req.Retry
		return credentials.Result{Username: "alice", Secret: []byte("fresh")}, true
	})

	src := a.credentialSource()
	got, err := src.Credentials(context.Background(), "example.com", true)
	if err != nil {
		t.Fatal(err)
	}
	if !seenRetry {
		t.Fatal("dialog must be told this is a retry")
	}
	if seenUsername != "alice" {
		t.Fatalf("dialog username = %q, want the vault's username", seenUsername)
	}
	if string(got.Password) != "fresh" {
		t.Fatalf("password = %q, want the freshly entered one", got.Password)
	}
}

func TestCredentialsSourceWithoutAVaultShowsTheDialog(t *testing.T) {
	a := newTestApp(t)
	stubCredentialsDialog(t, a, func(credentials.Request) (credentials.Result, bool) {
		return credentials.Result{Username: "bob", Secret: []byte("hunter2")}, true
	})

	got, err := a.credentialSource().Credentials(context.Background(), "example.com", false)
	if err != nil {
		t.Fatal(err)
	}
	if got.Username != "bob" || string(got.Password) != "hunter2" {
		t.Fatalf("credentials = %+v", got)
	}
}

func TestCredentialsSourceUserCancelReturnsErrNoCredentials(t *testing.T) {
	a := newTestApp(t)
	stubCredentialsDialog(t, a, func(credentials.Request) (credentials.Result, bool) {
		return credentials.Result{}, false
	})

	_, err := a.credentialSource().Credentials(context.Background(), "example.com", false)
	if !errors.Is(err, transport.ErrNoCredentials) {
		t.Fatalf("err = %v, want ErrNoCredentials", err)
	}
}

func TestCredentialsSourceContextCancelReturnsErrNoCredentials(t *testing.T) {
	a := newTestApp(t)
	prev := newCredentialsView
	opened := make(chan struct{})
	newCredentialsView = func(eng widget.ModalShower, req credentials.Request) (*credentials.View, error) {
		view, err := prev(eng, req)
		close(opened)
		return view, err
	}
	t.Cleanup(func() { newCredentialsView = prev })

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := a.credentialSource().Credentials(ctx, "example.com", false)
		done <- err
	}()
	<-opened
	cancel()
	err := <-done
	if !errors.Is(err, transport.ErrNoCredentials) {
		t.Fatalf("err = %v, want ErrNoCredentials", err)
	}
}

func TestCredentialsSourceDialogConstructionFailureReturnsErrNoCredentials(t *testing.T) {
	a := newTestApp(t)
	prev := newCredentialsView
	newCredentialsView = func(widget.ModalShower, credentials.Request) (*credentials.View, error) {
		return nil, errors.New("boom")
	}
	t.Cleanup(func() { newCredentialsView = prev })

	_, err := a.credentialSource().Credentials(context.Background(), "example.com", false)
	if !errors.Is(err, transport.ErrNoCredentials) {
		t.Fatalf("err = %v, want ErrNoCredentials", err)
	}
}

func TestCredentialsSourceRememberSavesToAnAlreadyUnlockedVault(t *testing.T) {
	a := newTestApp(t)
	v := createTestVault(t, a.paths.VaultFile())
	a.vaultInst = v
	stubCredentialsDialog(t, a, func(credentials.Request) (credentials.Result, bool) {
		return credentials.Result{Username: "bob", Secret: []byte("hunter2"), Remember: true}, true
	})

	got, err := a.credentialSource().Credentials(context.Background(), "example.com", false)
	if err != nil {
		t.Fatal(err)
	}
	if string(got.Password) != "hunter2" {
		t.Fatal("the returned credentials must still be usable")
	}
	saved, ok := v.Credential("example.com")
	if !ok || saved.Username != "bob" || string(saved.Secret) != "hunter2" {
		t.Fatalf("saved = %+v, ok=%v", saved, ok)
	}
}

func TestCredentialsSourceRememberUnlocksALockedVaultFirst(t *testing.T) {
	a := newTestApp(t)
	v := createTestVault(t, a.paths.VaultFile())
	v.Lock()
	stubCredentialsDialog(t, a, func(credentials.Request) (credentials.Result, bool) {
		return credentials.Result{Username: "bob", Secret: []byte("hunter2"), Remember: true}, true
	})
	stubUnlockDialog(t, a, []unlock.Result{{Password: []byte(testVaultPassword)}})

	if _, err := a.credentialSource().Credentials(context.Background(), "example.com", false); err != nil {
		t.Fatal(err)
	}
	reopened, err := vault.Open(vault.Options{Path: a.paths.VaultFile()})
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.Unlock(context.Background(), vault.NewPasswordUnlocker([]byte(testVaultPassword), vault.SlotParams{})); err != nil {
		t.Fatal(err)
	}
	saved, ok := reopened.Credential("example.com")
	if !ok || saved.Username != "bob" {
		t.Fatalf("credential was not persisted: %+v, ok=%v", saved, ok)
	}
}

func TestCredentialsSourceRememberRetriesAfterAWrongPassword(t *testing.T) {
	a := newTestApp(t)
	v := createTestVault(t, a.paths.VaultFile())
	v.Lock()
	stubCredentialsDialog(t, a, func(credentials.Request) (credentials.Result, bool) {
		return credentials.Result{Username: "bob", Secret: []byte("hunter2"), Remember: true}, true
	})
	stubUnlockDialog(t, a, []unlock.Result{{Password: []byte("wrong")}, {Password: []byte(testVaultPassword)}})

	if _, err := a.credentialSource().Credentials(context.Background(), "example.com", false); err != nil {
		t.Fatal(err)
	}
	if a.vaultInst.Locked() {
		t.Fatal("the app's vault instance must remain unlocked after a successful retry")
	}
	saved, ok := a.vaultInst.Credential("example.com")
	if !ok || saved.Username != "bob" {
		t.Fatal("credential was not saved after the retry")
	}
}

func TestCredentialsSourceRememberGivesUpWhenUnlockIsCancelled(t *testing.T) {
	a := newTestApp(t)
	v := createTestVault(t, a.paths.VaultFile())
	v.Lock()
	stubCredentialsDialog(t, a, func(credentials.Request) (credentials.Result, bool) {
		return credentials.Result{Username: "bob", Secret: []byte("hunter2"), Remember: true}, true
	})
	stubUnlockDialog(t, a, nil)

	got, err := a.credentialSource().Credentials(context.Background(), "example.com", false)
	if err != nil {
		t.Fatal("a failed remember must not fail the credential exchange itself")
	}
	if string(got.Password) != "hunter2" {
		t.Fatal("credentials must still be returned")
	}
	if !a.vaultInst.Locked() {
		t.Fatal("the app's vault instance must remain locked when the user cancels unlocking")
	}
}

func TestCredentialsSourceWithoutASetUpVaultRememberIsANoop(t *testing.T) {
	a := newTestApp(t)
	stubCredentialsDialog(t, a, func(credentials.Request) (credentials.Result, bool) {
		return credentials.Result{Username: "bob", Secret: []byte("hunter2"), Remember: true}, true
	})

	got, err := a.credentialSource().Credentials(context.Background(), "example.com", false)
	if err != nil {
		t.Fatal(err)
	}
	if string(got.Password) != "hunter2" {
		t.Fatal("credentials must still be returned even without a secret store")
	}
}

func TestCredentialsSourceRememberFailsWhenTheVaultErrorsForAnUnexpectedReason(t *testing.T) {
	a := newTestApp(t)
	v := createTestVault(t, a.paths.VaultFile())
	if err := v.Close(); err != nil {
		t.Fatal(err)
	}
	a.vaultInst = v
	stubCredentialsDialog(t, a, func(credentials.Request) (credentials.Result, bool) {
		return credentials.Result{Username: "bob", Secret: []byte("hunter2"), Remember: true}, true
	})
	stubUnlockDialog(t, a, []unlock.Result{{Password: []byte(testVaultPassword)}})

	got, err := a.credentialSource().Credentials(context.Background(), "example.com", false)
	if err != nil {
		t.Fatal("a failed remember must not fail the credential exchange itself")
	}
	if string(got.Password) != "hunter2" {
		t.Fatal("credentials must still be returned")
	}
}

func TestAskUnlockPasswordDialogConstructionFailureGivesUp(t *testing.T) {
	a := newTestApp(t)
	v := createTestVault(t, a.paths.VaultFile())
	v.Lock()
	stubCredentialsDialog(t, a, func(credentials.Request) (credentials.Result, bool) {
		return credentials.Result{Username: "bob", Secret: []byte("hunter2"), Remember: true}, true
	})
	prev := newUnlockView
	newUnlockView = func(widget.ModalShower, unlock.Request) (*unlock.View, error) {
		return nil, errors.New("boom")
	}
	t.Cleanup(func() { newUnlockView = prev })

	got, err := a.credentialSource().Credentials(context.Background(), "example.com", false)
	if err != nil {
		t.Fatal("a failed unlock dialog must not fail the credential exchange itself")
	}
	if string(got.Password) != "hunter2" {
		t.Fatal("credentials must still be returned")
	}
	if !a.vaultInst.Locked() {
		t.Fatal("the vault must remain locked when its unlock dialog cannot open")
	}
}

func TestUnlockVaultReturnsFalseWhenContextIsCancelledWhileWaiting(t *testing.T) {
	a := newTestApp(t)
	v := createTestVault(t, a.paths.VaultFile())
	v.Lock()
	prev := newUnlockView
	opened := make(chan struct{})
	newUnlockView = func(eng widget.ModalShower, req unlock.Request) (*unlock.View, error) {
		view, err := prev(eng, req)
		close(opened)
		return view, err
	}
	t.Cleanup(func() { newUnlockView = prev })

	ctx, cancel := context.WithCancel(context.Background())
	type result struct {
		ok  bool
		err error
	}
	done := make(chan result, 1)
	go func() {
		ok, err := a.unlockVault(ctx, v)
		done <- result{ok, err}
	}()
	<-opened
	cancel()
	r := <-done
	if r.ok || r.err != nil {
		t.Fatalf("unlockVault = %+v, want ok=false err=nil once the context is cancelled", r)
	}
}

func TestVaultIfOpenReportsUnexpectedErrors(t *testing.T) {
	a := newTestApp(t)
	if err := writeFile(a.paths.Dir, "vault.bin", "not a vault"); err != nil {
		t.Fatal(err)
	}
	if v := a.vaultIfOpen(); v != nil {
		t.Fatal("a corrupted vault file must not open")
	}
}

func TestCredentialsSourceHelperModeReturnsTheChainAnswerWithoutADialog(t *testing.T) {
	a := newTestApp(t)
	a.cfg.Git.CredentialSource = config.CredentialSourceHelper
	failIfCredentialsDialogOpens(t)
	helper := &fakeCredentialHelper{name: "manager", hasAnswer: true, answer: credential.Answer{Username: "alice", Password: []byte("token")}}
	stubCredentialChain(t, helper)

	got, err := a.credentialSource().Credentials(context.Background(), "example.com", false)
	if err != nil {
		t.Fatal(err)
	}
	if got.Username != "alice" || string(got.Password) != "token" {
		t.Fatalf("credentials = %+v", got)
	}
	if len(helper.stored) != 0 {
		t.Fatal("a credential returned by the helper must not be stored back into it")
	}
}

func TestCredentialsSourceHelperModeIgnoresTheVaultEntirely(t *testing.T) {
	a := newTestApp(t)
	a.cfg.Git.CredentialSource = config.CredentialSourceHelper
	v := createTestVault(t, a.paths.VaultFile())
	a.vaultInst = v
	if err := v.SetCredential(vault.Credential{Resource: "example.com", Username: "vault-user", Secret: []byte("vault-secret")}); err != nil {
		t.Fatal(err)
	}
	helper := &fakeCredentialHelper{name: "manager"}
	stubCredentialChain(t, helper)
	stubCredentialsDialog(t, a, func(credentials.Request) (credentials.Result, bool) {
		return credentials.Result{Username: "bob", Secret: []byte("hunter2")}, true
	})

	got, err := a.credentialSource().Credentials(context.Background(), "example.com", false)
	if err != nil {
		t.Fatal(err)
	}
	if got.Username != "bob" || string(got.Password) != "hunter2" {
		t.Fatalf("credentials = %+v, want the dialog's answer, not the vault's", got)
	}
}

func TestCredentialsSourceHelperModeRetryErasesTheChainAnswerAndPrefillsTheUsername(t *testing.T) {
	a := newTestApp(t)
	a.cfg.Git.CredentialSource = config.CredentialSourceHelper
	helper := &fakeCredentialHelper{name: "manager", hasAnswer: true, answer: credential.Answer{Username: "alice", Password: []byte("stale")}}
	stubCredentialChain(t, helper)
	var seenUsername string
	var seenRetry bool
	stubCredentialsDialog(t, a, func(req credentials.Request) (credentials.Result, bool) {
		seenUsername = req.Username
		seenRetry = req.Retry
		return credentials.Result{Username: "alice", Secret: []byte("fresh")}, true
	})

	got, err := a.credentialSource().Credentials(context.Background(), "example.com", true)
	if err != nil {
		t.Fatal(err)
	}
	if !seenRetry {
		t.Fatal("dialog must be told this is a retry")
	}
	if seenUsername != "alice" {
		t.Fatalf("dialog username = %q, want the stale helper answer's username", seenUsername)
	}
	if helper.erased != 1 {
		t.Fatalf("erased = %d, want 1", helper.erased)
	}
	if string(got.Password) != "fresh" {
		t.Fatalf("password = %q, want the freshly entered one", got.Password)
	}
}

func TestCredentialsSourceHelperModeRememberStoresToTheChainNotTheVault(t *testing.T) {
	a := newTestApp(t)
	a.cfg.Git.CredentialSource = config.CredentialSourceHelper
	helper := &fakeCredentialHelper{name: "manager"}
	stubCredentialChain(t, helper)
	stubCredentialsDialog(t, a, func(credentials.Request) (credentials.Result, bool) {
		return credentials.Result{Username: "bob", Secret: []byte("hunter2"), Remember: true}, true
	})

	if _, err := a.credentialSource().Credentials(context.Background(), "example.com", false); err != nil {
		t.Fatal(err)
	}
	if len(helper.stored) != 1 || helper.stored[0].Username != "bob" || string(helper.stored[0].Password) != "hunter2" {
		t.Fatalf("stored = %+v", helper.stored)
	}
	if _, err := os.Stat(a.paths.VaultFile()); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("helper mode must not create a secret store")
	}
}

func TestCredentialsSourceVaultThenHelperModeFallsBackToTheChainBeforeTheDialog(t *testing.T) {
	a := newTestApp(t)
	a.cfg.Git.CredentialSource = config.CredentialSourceVaultThenHelper
	failIfCredentialsDialogOpens(t)
	helper := &fakeCredentialHelper{name: "manager", hasAnswer: true, answer: credential.Answer{Username: "alice", Password: []byte("token")}}
	stubCredentialChain(t, helper)

	got, err := a.credentialSource().Credentials(context.Background(), "example.com", false)
	if err != nil {
		t.Fatal(err)
	}
	if got.Username != "alice" || string(got.Password) != "token" {
		t.Fatalf("credentials = %+v", got)
	}
}

func TestCredentialsSourceVaultThenHelperModePrefersTheVaultOverTheChain(t *testing.T) {
	a := newTestApp(t)
	a.cfg.Git.CredentialSource = config.CredentialSourceVaultThenHelper
	failIfCredentialsDialogOpens(t)
	v := createTestVault(t, a.paths.VaultFile())
	a.vaultInst = v
	if err := v.SetCredential(vault.Credential{Resource: "example.com", Username: "vault-user", Secret: []byte("vault-secret")}); err != nil {
		t.Fatal(err)
	}
	helper := &fakeCredentialHelper{name: "manager", hasAnswer: true, answer: credential.Answer{Username: "helper-user", Password: []byte("helper-secret")}}
	stubCredentialChain(t, helper)

	got, err := a.credentialSource().Credentials(context.Background(), "example.com", false)
	if err != nil {
		t.Fatal(err)
	}
	if got.Username != "vault-user" || string(got.Password) != "vault-secret" {
		t.Fatalf("credentials = %+v, want the vault's entry to take priority", got)
	}
}

func TestCredentialsSourceVaultThenHelperModeRememberStillSavesToTheVault(t *testing.T) {
	a := newTestApp(t)
	a.cfg.Git.CredentialSource = config.CredentialSourceVaultThenHelper
	v := createTestVault(t, a.paths.VaultFile())
	a.vaultInst = v
	helper := &fakeCredentialHelper{name: "manager"}
	stubCredentialChain(t, helper)
	stubCredentialsDialog(t, a, func(credentials.Request) (credentials.Result, bool) {
		return credentials.Result{Username: "bob", Secret: []byte("hunter2"), Remember: true}, true
	})

	if _, err := a.credentialSource().Credentials(context.Background(), "example.com", false); err != nil {
		t.Fatal(err)
	}
	saved, ok := v.Credential("example.com")
	if !ok || saved.Username != "bob" || string(saved.Secret) != "hunter2" {
		t.Fatalf("saved = %+v, ok=%v", saved, ok)
	}
	if len(helper.stored) != 0 {
		t.Fatal("vault+helper mode must not also store the remembered credential into the helper chain")
	}
}

func TestCredentialsSourceHelperModeRetryLogsWarnWhenEraseFails(t *testing.T) {
	a := newTestApp(t)
	a.cfg.Git.CredentialSource = config.CredentialSourceHelper
	var buf bytes.Buffer
	a.log = slog.New(slog.NewTextHandler(&buf, nil))
	helper := &fakeCredentialHelper{name: "manager", eraseErr: errors.New("boom")}
	stubCredentialChain(t, helper)
	stubCredentialsDialog(t, a, func(credentials.Request) (credentials.Result, bool) {
		return credentials.Result{}, false
	})

	_, _ = a.credentialSource().Credentials(context.Background(), "example.com", true)

	if helper.erased != 1 {
		t.Fatalf("erased = %d, want 1", helper.erased)
	}
	if buf.Len() == 0 {
		t.Fatal("an erase failure must be logged")
	}
}

func TestCredentialsSourceHelperModeRememberLogsWarnWhenStoreFails(t *testing.T) {
	a := newTestApp(t)
	a.cfg.Git.CredentialSource = config.CredentialSourceHelper
	var buf bytes.Buffer
	a.log = slog.New(slog.NewTextHandler(&buf, nil))
	helper := &fakeCredentialHelper{name: "manager", storeErr: errors.New("boom")}
	stubCredentialChain(t, helper)
	stubCredentialsDialog(t, a, func(credentials.Request) (credentials.Result, bool) {
		return credentials.Result{Username: "bob", Secret: []byte("hunter2"), Remember: true}, true
	})

	if _, err := a.credentialSource().Credentials(context.Background(), "example.com", false); err != nil {
		t.Fatal(err)
	}
	if len(helper.stored) != 1 {
		t.Fatalf("stored = %+v, want 1 attempt", helper.stored)
	}
	if buf.Len() == 0 {
		t.Fatal("a store failure must be logged")
	}
}

func TestCredentialsSourceVaultThenHelperModeRetryErasesFromTheChain(t *testing.T) {
	a := newTestApp(t)
	a.cfg.Git.CredentialSource = config.CredentialSourceVaultThenHelper
	v := createTestVault(t, a.paths.VaultFile())
	a.vaultInst = v
	helper := &fakeCredentialHelper{name: "manager", hasAnswer: true, answer: credential.Answer{Username: "alice", Password: []byte("stale")}}
	stubCredentialChain(t, helper)
	stubCredentialsDialog(t, a, func(credentials.Request) (credentials.Result, bool) {
		return credentials.Result{Username: "alice", Secret: []byte("fresh")}, true
	})

	if _, err := a.credentialSource().Credentials(context.Background(), "example.com", true); err != nil {
		t.Fatal(err)
	}
	if helper.erased != 1 {
		t.Fatalf("erased = %d, want 1", helper.erased)
	}
}

func TestCredentialQueryForUsesUseHTTPPathFromConfig(t *testing.T) {
	cfg := loadRawConfig(t, "[credential]\n\tusehttppath = true\n")
	q := credentialQueryFor(cfg, "https://example.com/org/repo.git")
	if q.Host != "example.com" {
		t.Fatalf("host = %q", q.Host)
	}
	if q.Path != "org/repo.git" {
		t.Fatalf("path = %q, want the URL path since usehttppath is set", q.Path)
	}
}

func TestCredentialQueryForIgnoresPathWithoutUseHTTPPath(t *testing.T) {
	cfg := loadRawConfig(t, "")
	q := credentialQueryFor(cfg, "https://example.com/org/repo.git")
	if q.Path != "" {
		t.Fatalf("path = %q, want empty without usehttppath", q.Path)
	}
}

func TestCredentialQueryForFallsBackToTheConfiguredUsername(t *testing.T) {
	cfg := loadRawConfig(t, "[credential]\n\tusername = bob\n")
	q := credentialQueryFor(cfg, "https://example.com/repo.git")
	if q.Username != "bob" {
		t.Fatalf("username = %q, want bob", q.Username)
	}
}

func TestCredentialQueryForKeepsTheUsernameFromTheURL(t *testing.T) {
	cfg := loadRawConfig(t, "[credential]\n\tusername = bob\n")
	q := credentialQueryFor(cfg, "https://alice@example.com/repo.git")
	if q.Username != "alice" {
		t.Fatalf("username = %q, want the URL's own username", q.Username)
	}
}

func TestCredentialQueryForReturnsZeroValueOnAnInvalidURL(t *testing.T) {
	cfg := loadRawConfig(t, "")
	got := credentialQueryFor(cfg, "https://")
	if got != (credential.Query{}) {
		t.Fatalf("query = %+v, want the zero value", got)
	}
}

func TestRepositoryConfigForCredentialsWithoutAnOpenRepositoryReturnsAnEmptyConfig(t *testing.T) {
	a := newTestApp(t)
	cfg := a.repositoryConfigForCredentials()
	if cfg == nil {
		t.Fatal("config must not be nil")
	}
	if _, ok := cfg.Get("credential.helper"); ok {
		t.Fatal("an empty config must not report any configured value")
	}
}

func TestRepositoryConfigForCredentialsWhenTheRepositoryCannotBeReopenedReturnsAnEmptyConfig(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "repo")
	initTestRepoWithBranch(t, target, "main")
	a := newTestApp(t)
	a.setOpened(openTestRepository(t, target))

	prev := openGitRepository
	openGitRepository = func(string, gitrepo.OpenOptions) (*gitrepo.Repository, error) {
		return nil, errors.New("boom")
	}
	t.Cleanup(func() { openGitRepository = prev })

	cfg := a.repositoryConfigForCredentials()
	if cfg == nil {
		t.Fatal("config must not be nil")
	}
}

func TestRepositoryConfigForCredentialsReadsTheOpenRepositoryConfig(t *testing.T) {
	isolateGitConfig(t)
	dir := t.TempDir()
	target := filepath.Join(dir, "repo")
	initTestRepoWithBranch(t, target, "main")
	appendGitConfig(t, target, "[credential]\n\thelper = store\n")

	a := newTestApp(t)
	a.setOpened(openTestRepository(t, target))

	cfg := a.repositoryConfigForCredentials()
	if v, ok := cfg.Get("credential.helper"); !ok || v != "store" {
		t.Fatalf("credential.helper = %q, ok=%v, want store", v, ok)
	}
}

func TestDefaultCredentialChainAndQueryUsesTheOpenRepositoryConfig(t *testing.T) {
	isolateGitConfig(t)
	dir := t.TempDir()
	target := filepath.Join(dir, "repo")
	initTestRepoWithBranch(t, target, "main")
	appendGitConfig(t, target, "[credential]\n\thelper = store\n")

	a := newTestApp(t)
	a.setOpened(openTestRepository(t, target))

	chain, q := defaultCredentialChainAndQuery(a, "example.com/org/repo")
	if len(chain) != 1 {
		t.Fatalf("chain = %+v, want 1 helper", chain)
	}
	if q.Host != "example.com" {
		t.Fatalf("host = %q, want example.com", q.Host)
	}
}

func TestDefaultCredentialChainAndQueryReturnsNoChainOnAnInvalidURL(t *testing.T) {
	a := newTestApp(t)
	chain, q := defaultCredentialChainAndQuery(a, "")
	if chain != nil {
		t.Fatalf("chain = %+v, want nil", chain)
	}
	if q != (credential.Query{}) {
		t.Fatalf("query = %+v, want the zero value", q)
	}
}

func TestRememberTargetLabelMatchesTheConfiguredCredentialSource(t *testing.T) {
	for _, tc := range []struct {
		mode string
		key  string
	}{
		{config.CredentialSourceVault, "Dialog.Credentials.Target.Vault"},
		{config.CredentialSourceVaultThenHelper, "Dialog.Credentials.Target.Vault"},
		{config.CredentialSourceHelper, "Dialog.Credentials.Target.Helper"},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			a := newTestApp(t)
			a.cfg.Git.CredentialSource = tc.mode
			var seenTarget string
			stubCredentialsDialog(t, a, func(req credentials.Request) (credentials.Result, bool) {
				seenTarget = req.RememberTarget
				return credentials.Result{}, false
			})
			stubCredentialChain(t, &fakeCredentialHelper{name: "manager"})

			_, _ = a.credentialSource().Credentials(context.Background(), "example.com", false)
			if want := i18n.T(tc.key); seenTarget != want {
				t.Fatalf("mode %q: RememberTarget = %q, want %q", tc.mode, seenTarget, want)
			}
		})
	}
}
