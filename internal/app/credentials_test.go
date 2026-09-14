package app

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
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

const testResource = "https://example.com/org/repo.git"

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
	erased    []credential.Answer
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

func (f *fakeCredentialHelper) Erase(_ context.Context, _ credential.Query, a credential.Answer) error {
	f.erased = append(f.erased, credential.Answer{Username: a.Username, Password: append([]byte(nil), a.Password...)})
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
		return chain, credential.Query{Protocol: "https", Host: "example.com"}
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

func setVaultCredential(t *testing.T, v *vault.Vault, resource, username, secret string) {
	t.Helper()
	if err := v.SetCredential(vault.Credential{Resource: resource, Username: username, Secret: []byte(secret)}); err != nil {
		t.Fatal(err)
	}
}

func feedbackOf(t *testing.T, src transport.CredentialSource) transport.CredentialFeedback {
	t.Helper()
	feedback, ok := src.(transport.CredentialFeedback)
	if !ok {
		t.Fatal("the app credential source must accept feedback from the transport")
	}
	return feedback
}

func captureLog(a *App) *bytes.Buffer {
	var buf bytes.Buffer
	a.log = slog.New(slog.NewTextHandler(&buf, nil))
	return &buf
}

func TestCredentialsSourceReturnsVaultCredentialWithoutShowingADialog(t *testing.T) {
	a := newTestApp(t)
	failIfCredentialsDialogOpens(t)
	v := createTestVault(t, a.paths.VaultFile())
	a.vaultInst = v
	setVaultCredential(t, v, testResource, "alice", "token")

	got, err := a.credentialSource().Credentials(context.Background(), testResource, false)
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
	setVaultCredential(t, v, "https://example.com", "generic", "generic-token")
	setVaultCredential(t, v, "https://example.com/org", "specific", "specific-token")

	got, err := a.credentialSource().Credentials(context.Background(), testResource, false)
	if err != nil {
		t.Fatal(err)
	}
	if got.Username != "specific" {
		t.Fatalf("username = %q, want the longest matching prefix", got.Username)
	}
}

func TestCredentialsSourceDoesNotOfferAnHTTPSPasswordToPlainHTTPOrAnotherPort(t *testing.T) {
	for _, resource := range []string{"http://example.com/org/repo.git", "http://example.com:8080/org/repo.git", "https://example.com:8443/org/repo.git"} {
		t.Run(resource, func(t *testing.T) {
			a := newTestApp(t)
			v := createTestVault(t, a.paths.VaultFile())
			a.vaultInst = v
			setVaultCredential(t, v, testResource, "alice", "https-only")
			opened := false
			stubCredentialsDialog(t, a, func(credentials.Request) (credentials.Result, bool) {
				opened = true
				return credentials.Result{}, false
			})

			_, err := a.credentialSource().Credentials(context.Background(), resource, false)
			if !errors.Is(err, transport.ErrNoCredentials) || !opened {
				t.Fatalf("err = %v, dialog opened = %v, want the dialog instead of the https entry", err, opened)
			}
		})
	}
}

func TestCredentialsSourceUsesAHostEntryForTheUserNamedInTheURL(t *testing.T) {
	a := newTestApp(t)
	failIfCredentialsDialogOpens(t)
	v := createTestVault(t, a.paths.VaultFile())
	a.vaultInst = v
	setVaultCredential(t, v, "https://example.com", "bob", "bobs-token")

	got, err := a.credentialSource().Credentials(context.Background(), "https://bob@example.com/org/repo.git", false)
	if err != nil {
		t.Fatal(err)
	}
	if got.Username != "bob" || string(got.Password) != "bobs-token" {
		t.Fatalf("credentials = %+v", got)
	}
}

func TestCredentialsSourceIgnoresAHostEntryOfAnotherUserAndPrefillsTheURLUser(t *testing.T) {
	a := newTestApp(t)
	v := createTestVault(t, a.paths.VaultFile())
	a.vaultInst = v
	setVaultCredential(t, v, "https://example.com", "alice", "alices-token")
	var seenUsername string
	stubCredentialsDialog(t, a, func(req credentials.Request) (credentials.Result, bool) {
		seenUsername = req.Username
		return credentials.Result{Username: "bob", Secret: []byte("typed")}, true
	})

	got, err := a.credentialSource().Credentials(context.Background(), "https://bob@example.com/org/repo.git", false)
	if err != nil {
		t.Fatal(err)
	}
	if string(got.Password) != "typed" || seenUsername != "bob" {
		t.Fatalf("credentials = %+v, dialog username = %q", got, seenUsername)
	}
}

func TestCredentialsSourceFindsALegacyHostPathEntryAndMovesItToTheURLKeyOnApproval(t *testing.T) {
	a := newTestApp(t)
	failIfCredentialsDialogOpens(t)
	v := createTestVault(t, a.paths.VaultFile())
	a.vaultInst = v
	setVaultCredential(t, v, "example.com/org", "alice", "old-token")

	src := a.credentialSource()
	got, err := src.Credentials(context.Background(), testResource, false)
	if err != nil {
		t.Fatal(err)
	}
	if got.Username != "alice" || string(got.Password) != "old-token" {
		t.Fatalf("credentials = %+v", got)
	}
	if !slices.Equal(v.Resources(), []string{"example.com/org"}) {
		t.Fatalf("resources = %v, the entry must not move before the server accepts it", v.Resources())
	}
	feedbackOf(t, src).Approve(context.Background(), testResource, got)
	if !slices.Equal(v.Resources(), []string{"https://example.com/org"}) {
		t.Fatalf("resources = %v, want the legacy entry moved under its https key", v.Resources())
	}
	moved, ok := v.Credential(testResource)
	if !ok || moved.Username != "alice" || string(moved.Secret) != "old-token" {
		t.Fatalf("moved = %+v, ok = %v", moved, ok)
	}
}

func TestCredentialsSourceDoesNotUseLegacyEntriesForHTTPPortsOrUsers(t *testing.T) {
	for _, resource := range []string{"http://example.com/org/repo.git", "https://example.com:8443/org/repo.git", "https://bob@example.com/org/repo.git"} {
		t.Run(resource, func(t *testing.T) {
			a := newTestApp(t)
			v := createTestVault(t, a.paths.VaultFile())
			a.vaultInst = v
			setVaultCredential(t, v, "example.com/org", "bob", "legacy")
			stubCredentialsDialog(t, a, func(credentials.Request) (credentials.Result, bool) {
				return credentials.Result{}, false
			})

			_, err := a.credentialSource().Credentials(context.Background(), resource, false)
			if !errors.Is(err, transport.ErrNoCredentials) {
				t.Fatalf("err = %v, want the dialog instead of the legacy entry", err)
			}
		})
	}
}

func TestCredentialsSourceRejectedLegacyEntryIsRemovedNotMoved(t *testing.T) {
	a := newTestApp(t)
	v := createTestVault(t, a.paths.VaultFile())
	a.vaultInst = v
	setVaultCredential(t, v, "example.com/org", "alice", "old-token")

	src := a.credentialSource()
	got, err := src.Credentials(context.Background(), testResource, false)
	if err != nil {
		t.Fatal(err)
	}
	feedbackOf(t, src).Reject(context.Background(), testResource, got)
	if len(v.Resources()) != 0 {
		t.Fatalf("resources = %v, want the rejected entry gone", v.Resources())
	}
}

func TestCredentialsSourceLogsWhenALegacyEntryCannotBeMoved(t *testing.T) {
	a := newTestApp(t)
	buf := captureLog(a)
	v := createTestVault(t, a.paths.VaultFile())
	a.vaultInst = v
	setVaultCredential(t, v, "example.com/org", "alice", "old-token")

	src := a.credentialSource()
	got, err := src.Credentials(context.Background(), testResource, false)
	if err != nil {
		t.Fatal(err)
	}
	v.Lock()
	feedbackOf(t, src).Approve(context.Background(), testResource, got)
	if buf.Len() == 0 {
		t.Fatal("a failed move must be logged")
	}
}

func TestCredentialsSourceFeedbackWithoutAVaultAnyMoreIsANoop(t *testing.T) {
	for _, approve := range []bool{true, false} {
		a := newTestApp(t)
		v := createTestVault(t, a.paths.VaultFile())
		a.vaultInst = v
		setVaultCredential(t, v, "example.com/org", "alice", "old-token")

		src := a.credentialSource()
		got, err := src.Credentials(context.Background(), testResource, false)
		if err != nil {
			t.Fatal(err)
		}
		_ = v.Close()
		a.vaultInst = nil
		if err := os.Remove(a.paths.VaultFile()); err != nil {
			t.Fatal(err)
		}
		if approve {
			feedbackOf(t, src).Approve(context.Background(), testResource, got)
		} else {
			feedbackOf(t, src).Reject(context.Background(), testResource, got)
		}
		if a.vaultInst != nil {
			t.Fatal("feedback must not recreate the secret store")
		}
	}
}

func TestCredentialsSourceRejectRemovesTheVaultEntryAndTheRetryPrefillsItsUsername(t *testing.T) {
	a := newTestApp(t)
	v := createTestVault(t, a.paths.VaultFile())
	a.vaultInst = v
	setVaultCredential(t, v, testResource, "alice", "stale")
	var seenUsername string
	var seenRetry bool
	stubCredentialsDialog(t, a, func(req credentials.Request) (credentials.Result, bool) {
		seenUsername = req.Username
		seenRetry = req.Retry
		return credentials.Result{Username: "alice", Secret: []byte("fresh")}, true
	})

	src := a.credentialSource()
	stale, err := src.Credentials(context.Background(), testResource, false)
	if err != nil {
		t.Fatal(err)
	}
	feedbackOf(t, src).Reject(context.Background(), testResource, stale)
	if _, ok := v.Credential(testResource); ok {
		t.Fatal("the rejected vault entry must be removed")
	}
	got, err := src.Credentials(context.Background(), testResource, true)
	if err != nil {
		t.Fatal(err)
	}
	if !seenRetry || seenUsername != "alice" {
		t.Fatalf("dialog retry = %v, username = %q, want a retry prefilled with alice", seenRetry, seenUsername)
	}
	if string(got.Password) != "fresh" {
		t.Fatalf("password = %q, want the freshly entered one", got.Password)
	}
}

func TestCredentialsSourceRejectKeepsAVaultEntryChangedSinceItWasSupplied(t *testing.T) {
	a := newTestApp(t)
	v := createTestVault(t, a.paths.VaultFile())
	a.vaultInst = v
	setVaultCredential(t, v, testResource, "alice", "stale")

	src := a.credentialSource()
	stale, err := src.Credentials(context.Background(), testResource, false)
	if err != nil {
		t.Fatal(err)
	}
	setVaultCredential(t, v, testResource, "alice", "updated")
	feedbackOf(t, src).Reject(context.Background(), testResource, stale)
	kept, ok := v.Credential(testResource)
	if !ok || string(kept.Secret) != "updated" {
		t.Fatalf("kept = %+v, ok = %v, want the updated entry to survive", kept, ok)
	}
}

func TestCredentialsSourceRejectKeepsAShorterEntryWhenTheSuppliedOneIsAlreadyGone(t *testing.T) {
	a := newTestApp(t)
	v := createTestVault(t, a.paths.VaultFile())
	a.vaultInst = v
	setVaultCredential(t, v, "https://example.com", "alice", "stale")
	setVaultCredential(t, v, testResource, "alice", "stale")

	src := a.credentialSource()
	stale, err := src.Credentials(context.Background(), testResource, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := v.DeleteCredential(testResource); err != nil {
		t.Fatal(err)
	}
	feedbackOf(t, src).Reject(context.Background(), testResource, stale)
	if !slices.Equal(v.Resources(), []string{"https://example.com"}) {
		t.Fatalf("resources = %v, want the host entry kept", v.Resources())
	}
}

func TestCredentialsSourceLogsWhenARejectedVaultEntryCannotBeRemoved(t *testing.T) {
	a := newTestApp(t)
	buf := captureLog(a)
	v := createTestVault(t, a.paths.VaultFile())
	a.vaultInst = v
	setVaultCredential(t, v, testResource, "alice", "stale")

	src := a.credentialSource()
	stale, err := src.Credentials(context.Background(), testResource, false)
	if err != nil {
		t.Fatal(err)
	}
	blockFileWrites(t, a.paths.VaultFile())
	feedbackOf(t, src).Reject(context.Background(), testResource, stale)
	if buf.Len() == 0 {
		t.Fatal("a failed removal must be logged")
	}
}

func TestCredentialsSourceIgnoresFeedbackForCredentialsItDidNotSupply(t *testing.T) {
	a := newTestApp(t)
	v := createTestVault(t, a.paths.VaultFile())
	a.vaultInst = v
	setVaultCredential(t, v, testResource, "alice", "token")

	src := a.credentialSource()
	if _, err := src.Credentials(context.Background(), testResource, false); err != nil {
		t.Fatal(err)
	}
	feedback := feedbackOf(t, src)
	feedback.Reject(context.Background(), testResource, transport.Credentials{Username: "alice", Password: []byte("other")})
	feedback.Reject(context.Background(), testResource, transport.Credentials{Username: "mallory", Password: []byte("token")})
	feedback.Reject(context.Background(), "https://other.example/repo.git", transport.Credentials{Username: "alice", Password: []byte("token")})
	if _, ok := v.Credential(testResource); !ok {
		t.Fatal("feedback about a credential the source did not supply must not touch the vault")
	}
}

func TestCredentialsSourceForgetsAnEarlierSupplyWhenAskedAgain(t *testing.T) {
	a := newTestApp(t)
	a.cfg.Git.CredentialSource = config.CredentialSourceHelper
	first := &fakeCredentialHelper{name: "manager", hasAnswer: true, answer: credential.Answer{Username: "alice", Password: []byte("first")}}
	stubCredentialChain(t, first)

	src := a.credentialSource()
	earlier, err := src.Credentials(context.Background(), testResource, false)
	if err != nil {
		t.Fatal(err)
	}
	first.answer = credential.Answer{Username: "alice", Password: []byte("second")}
	later, err := src.Credentials(context.Background(), testResource, false)
	if err != nil {
		t.Fatal(err)
	}
	feedback := feedbackOf(t, src)
	feedback.Reject(context.Background(), testResource, earlier)
	if len(first.erased) != 0 {
		t.Fatalf("erased = %+v, feedback about the replaced supply must be ignored", first.erased)
	}
	feedback.Reject(context.Background(), testResource, later)
	if len(first.erased) != 1 || string(first.erased[0].Password) != "second" {
		t.Fatalf("erased = %+v, want the latest supply", first.erased)
	}
}

func TestCredentialsSourceWithoutAVaultShowsTheDialog(t *testing.T) {
	a := newTestApp(t)
	stubCredentialsDialog(t, a, func(credentials.Request) (credentials.Result, bool) {
		return credentials.Result{Username: "bob", Secret: []byte("hunter2")}, true
	})

	got, err := a.credentialSource().Credentials(context.Background(), testResource, false)
	if err != nil {
		t.Fatal(err)
	}
	if got.Username != "bob" || string(got.Password) != "hunter2" {
		t.Fatalf("credentials = %+v", got)
	}
}

func TestCredentialsSourceShowsTheFullResourceInTheDialog(t *testing.T) {
	a := newTestApp(t)
	var seen credentials.Request
	stubCredentialsDialog(t, a, func(req credentials.Request) (credentials.Result, bool) {
		seen = req
		return credentials.Result{}, false
	})

	_, _ = a.credentialSource().Credentials(context.Background(), "http://example.com:8080/repo.git", false)
	if seen.Resource != "http://example.com:8080/repo.git" || seen.Username != "" || seen.Retry {
		t.Fatalf("request = %+v", seen)
	}
}

func TestCredentialsSourceUserCancelReturnsErrNoCredentials(t *testing.T) {
	a := newTestApp(t)
	stubCredentialsDialog(t, a, func(credentials.Request) (credentials.Result, bool) {
		return credentials.Result{}, false
	})

	_, err := a.credentialSource().Credentials(context.Background(), testResource, false)
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
		_, err := a.credentialSource().Credentials(ctx, testResource, false)
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

	_, err := a.credentialSource().Credentials(context.Background(), testResource, false)
	if !errors.Is(err, transport.ErrNoCredentials) {
		t.Fatalf("err = %v, want ErrNoCredentials", err)
	}
}

func rememberAndApprove(t *testing.T, a *App) transport.Credentials {
	t.Helper()
	src := a.credentialSource()
	got, err := src.Credentials(context.Background(), testResource, false)
	if err != nil {
		t.Fatal("a failed remember must not fail the credential exchange itself")
	}
	if string(got.Password) != "hunter2" {
		t.Fatal("the returned credentials must still be usable")
	}
	feedbackOf(t, src).Approve(context.Background(), testResource, got)
	return got
}

func stubRememberedDialogAnswer(t *testing.T, a *App) {
	t.Helper()
	stubCredentialsDialog(t, a, func(credentials.Request) (credentials.Result, bool) {
		return credentials.Result{Username: "bob", Secret: []byte("hunter2"), Remember: true}, true
	})
}

func TestCredentialsSourceRememberSavesOnlyAfterTheServerAcceptsTheCredential(t *testing.T) {
	a := newTestApp(t)
	v := createTestVault(t, a.paths.VaultFile())
	a.vaultInst = v
	stubRememberedDialogAnswer(t, a)

	src := a.credentialSource()
	got, err := src.Credentials(context.Background(), testResource, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Resources()) != 0 {
		t.Fatalf("resources = %v, nothing may be saved before the server accepts the credential", v.Resources())
	}
	feedbackOf(t, src).Approve(context.Background(), testResource, got)
	saved, ok := v.Credential(testResource)
	if !ok || saved.Username != "bob" || string(saved.Secret) != "hunter2" {
		t.Fatalf("saved = %+v, ok=%v", saved, ok)
	}
}

func TestCredentialsSourceRememberedButRejectedCredentialIsNeverSaved(t *testing.T) {
	a := newTestApp(t)
	v := createTestVault(t, a.paths.VaultFile())
	a.vaultInst = v
	stubRememberedDialogAnswer(t, a)

	src := a.credentialSource()
	got, err := src.Credentials(context.Background(), testResource, false)
	if err != nil {
		t.Fatal(err)
	}
	feedbackOf(t, src).Reject(context.Background(), testResource, got)
	feedbackOf(t, src).Approve(context.Background(), testResource, got)
	if len(v.Resources()) != 0 {
		t.Fatalf("resources = %v, a rejected credential must not be saved", v.Resources())
	}
}

func TestCredentialsSourceApprovedButNotRememberedCredentialIsNotSaved(t *testing.T) {
	a := newTestApp(t)
	v := createTestVault(t, a.paths.VaultFile())
	a.vaultInst = v
	stubCredentialsDialog(t, a, func(credentials.Request) (credentials.Result, bool) {
		return credentials.Result{Username: "bob", Secret: []byte("hunter2")}, true
	})

	src := a.credentialSource()
	got, err := src.Credentials(context.Background(), testResource, false)
	if err != nil {
		t.Fatal(err)
	}
	feedbackOf(t, src).Approve(context.Background(), testResource, got)
	if len(v.Resources()) != 0 {
		t.Fatalf("resources = %v, want nothing saved", v.Resources())
	}
}

func TestCredentialsSourceRememberUnlocksALockedVaultFirst(t *testing.T) {
	a := newTestApp(t)
	v := createTestVault(t, a.paths.VaultFile())
	v.Lock()
	stubRememberedDialogAnswer(t, a)
	stubUnlockDialog(t, a, []unlock.Result{{Password: []byte(testVaultPassword)}})

	rememberAndApprove(t, a)
	reopened, err := vault.Open(vault.Options{Path: a.paths.VaultFile()})
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.Unlock(context.Background(), vault.NewPasswordUnlocker([]byte(testVaultPassword), vault.SlotParams{})); err != nil {
		t.Fatal(err)
	}
	saved, ok := reopened.Credential(testResource)
	if !ok || saved.Username != "bob" {
		t.Fatalf("credential was not persisted: %+v, ok=%v", saved, ok)
	}
}

func TestCredentialsSourceRememberRetriesAfterAWrongPassword(t *testing.T) {
	a := newTestApp(t)
	v := createTestVault(t, a.paths.VaultFile())
	v.Lock()
	stubRememberedDialogAnswer(t, a)
	stubUnlockDialog(t, a, []unlock.Result{{Password: []byte("wrong")}, {Password: []byte(testVaultPassword)}})

	rememberAndApprove(t, a)
	if a.vaultInst.Locked() {
		t.Fatal("the app's vault instance must remain unlocked after a successful retry")
	}
	saved, ok := a.vaultInst.Credential(testResource)
	if !ok || saved.Username != "bob" {
		t.Fatal("credential was not saved after the retry")
	}
}

func TestCredentialsSourceRememberGivesUpWhenUnlockIsCancelled(t *testing.T) {
	a := newTestApp(t)
	v := createTestVault(t, a.paths.VaultFile())
	v.Lock()
	stubRememberedDialogAnswer(t, a)
	stubUnlockDialog(t, a, nil)

	rememberAndApprove(t, a)
	if !a.vaultInst.Locked() {
		t.Fatal("the app's vault instance must remain locked when the user cancels unlocking")
	}
}

func TestCredentialsSourceWithoutASetUpVaultRememberIsANoop(t *testing.T) {
	a := newTestApp(t)
	stubRememberedDialogAnswer(t, a)

	rememberAndApprove(t, a)
	if _, err := os.Stat(a.paths.VaultFile()); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("remembering must not create a secret store")
	}
}

func TestCredentialsSourceRememberFailsWhenTheVaultErrorsForAnUnexpectedReason(t *testing.T) {
	a := newTestApp(t)
	buf := captureLog(a)
	v := createTestVault(t, a.paths.VaultFile())
	if err := v.Close(); err != nil {
		t.Fatal(err)
	}
	a.vaultInst = v
	stubRememberedDialogAnswer(t, a)
	stubUnlockDialog(t, a, []unlock.Result{{Password: []byte(testVaultPassword)}})

	rememberAndApprove(t, a)
	if buf.Len() == 0 {
		t.Fatal("a failed remember must be logged")
	}
}

func TestCredentialsSourceApproveLogsWhenTheUnlockedVaultCannotBeWritten(t *testing.T) {
	a := newTestApp(t)
	buf := captureLog(a)
	v := createTestVault(t, a.paths.VaultFile())
	a.vaultInst = v
	stubRememberedDialogAnswer(t, a)
	blockFileWrites(t, a.paths.VaultFile())

	rememberAndApprove(t, a)
	if len(v.Resources()) != 0 {
		t.Fatalf("resources = %v, want nothing saved", v.Resources())
	}
	if buf.Len() == 0 {
		t.Fatal("a failed save must be logged")
	}
}

func TestAskUnlockPasswordDialogConstructionFailureGivesUp(t *testing.T) {
	a := newTestApp(t)
	v := createTestVault(t, a.paths.VaultFile())
	v.Lock()
	stubRememberedDialogAnswer(t, a)
	prev := newUnlockView
	newUnlockView = func(widget.ModalShower, unlock.Request) (*unlock.View, error) {
		return nil, errors.New("boom")
	}
	t.Cleanup(func() { newUnlockView = prev })

	rememberAndApprove(t, a)
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

func TestCredentialsSourceHelperModeReturnsTheChainAnswerAndStoresItBackOnlyOnApproval(t *testing.T) {
	a := newTestApp(t)
	a.cfg.Git.CredentialSource = config.CredentialSourceHelper
	failIfCredentialsDialogOpens(t)
	helper := &fakeCredentialHelper{name: "manager", hasAnswer: true, answer: credential.Answer{Username: "alice", Password: []byte("token")}}
	stubCredentialChain(t, helper)

	src := a.credentialSource()
	got, err := src.Credentials(context.Background(), testResource, false)
	if err != nil {
		t.Fatal(err)
	}
	if got.Username != "alice" || string(got.Password) != "token" {
		t.Fatalf("credentials = %+v", got)
	}
	if len(helper.stored) != 0 {
		t.Fatal("a helper credential must not be stored back before the server accepts it")
	}
	feedbackOf(t, src).Approve(context.Background(), testResource, got)
	if len(helper.stored) != 1 || helper.stored[0].Username != "alice" || string(helper.stored[0].Password) != "token" {
		t.Fatalf("stored = %+v, want the approved helper credential", helper.stored)
	}
	if len(helper.erased) != 0 {
		t.Fatalf("erased = %+v, want none", helper.erased)
	}
}

func TestCredentialsSourceHelperModeIgnoresTheVaultEntirely(t *testing.T) {
	a := newTestApp(t)
	a.cfg.Git.CredentialSource = config.CredentialSourceHelper
	v := createTestVault(t, a.paths.VaultFile())
	a.vaultInst = v
	setVaultCredential(t, v, testResource, "vault-user", "vault-secret")
	helper := &fakeCredentialHelper{name: "manager"}
	stubCredentialChain(t, helper)
	stubCredentialsDialog(t, a, func(credentials.Request) (credentials.Result, bool) {
		return credentials.Result{Username: "bob", Secret: []byte("hunter2")}, true
	})

	got, err := a.credentialSource().Credentials(context.Background(), testResource, false)
	if err != nil {
		t.Fatal(err)
	}
	if got.Username != "bob" || string(got.Password) != "hunter2" {
		t.Fatalf("credentials = %+v, want the dialog's answer, not the vault's", got)
	}
}

func TestCredentialsSourceHelperModeRejectErasesExactlyTheFailedCredentialAndTheRetryPrefillsIt(t *testing.T) {
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

	src := a.credentialSource()
	stale, err := src.Credentials(context.Background(), testResource, false)
	if err != nil {
		t.Fatal(err)
	}
	feedbackOf(t, src).Reject(context.Background(), testResource, stale)
	if len(helper.erased) != 1 || helper.erased[0].Username != "alice" || string(helper.erased[0].Password) != "stale" {
		t.Fatalf("erased = %+v, want exactly alice with the stale password", helper.erased)
	}
	got, err := src.Credentials(context.Background(), testResource, true)
	if err != nil {
		t.Fatal(err)
	}
	if !seenRetry || seenUsername != "alice" {
		t.Fatalf("dialog retry = %v, username = %q", seenRetry, seenUsername)
	}
	if len(helper.erased) != 1 {
		t.Fatalf("erased = %+v, the retry prompt must not erase anything by itself", helper.erased)
	}
	if string(got.Password) != "fresh" {
		t.Fatalf("password = %q, want the freshly entered one", got.Password)
	}
}

func TestCredentialsSourceRejectOfADialogCredentialErasesNothing(t *testing.T) {
	a := newTestApp(t)
	a.cfg.Git.CredentialSource = config.CredentialSourceHelper
	helper := &fakeCredentialHelper{name: "manager"}
	stubCredentialChain(t, helper)
	stubRememberedDialogAnswer(t, a)

	src := a.credentialSource()
	got, err := src.Credentials(context.Background(), testResource, false)
	if err != nil {
		t.Fatal(err)
	}
	feedbackOf(t, src).Reject(context.Background(), testResource, got)
	if len(helper.erased) != 0 || len(helper.stored) != 0 {
		t.Fatalf("erased = %+v, stored = %+v, want no helper traffic for a typed credential", helper.erased, helper.stored)
	}
}

func TestCredentialsSourceHelperModeRememberStoresToTheChainOnApprovalNotTheVault(t *testing.T) {
	a := newTestApp(t)
	a.cfg.Git.CredentialSource = config.CredentialSourceHelper
	helper := &fakeCredentialHelper{name: "manager"}
	stubCredentialChain(t, helper)
	stubRememberedDialogAnswer(t, a)

	src := a.credentialSource()
	got, err := src.Credentials(context.Background(), testResource, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(helper.stored) != 0 {
		t.Fatal("nothing may be stored before the server accepts the credential")
	}
	feedbackOf(t, src).Approve(context.Background(), testResource, got)
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

	got, err := a.credentialSource().Credentials(context.Background(), testResource, false)
	if err != nil {
		t.Fatal(err)
	}
	if got.Username != "alice" || string(got.Password) != "token" {
		t.Fatalf("credentials = %+v", got)
	}
}

func TestCredentialsSourceLogsAHelperQueryFailureAndAsksTheUser(t *testing.T) {
	a := newTestApp(t)
	a.cfg.Git.CredentialSource = config.CredentialSourceHelper
	buf := captureLog(a)
	helper := &fakeCredentialHelper{name: "manager", getErr: errors.New("boom")}
	stubCredentialChain(t, helper)
	stubCredentialsDialog(t, a, func(credentials.Request) (credentials.Result, bool) {
		return credentials.Result{Username: "bob", Secret: []byte("typed")}, true
	})

	got, err := a.credentialSource().Credentials(context.Background(), testResource, false)
	if err != nil {
		t.Fatal(err)
	}
	if string(got.Password) != "typed" {
		t.Fatalf("credentials = %+v, want the dialog answer", got)
	}
	if buf.Len() == 0 {
		t.Fatal("a helper failure must be logged")
	}
}

func TestCredentialsSourceVaultThenHelperModePrefersTheVaultOverTheChain(t *testing.T) {
	a := newTestApp(t)
	a.cfg.Git.CredentialSource = config.CredentialSourceVaultThenHelper
	failIfCredentialsDialogOpens(t)
	v := createTestVault(t, a.paths.VaultFile())
	a.vaultInst = v
	setVaultCredential(t, v, testResource, "vault-user", "vault-secret")
	helper := &fakeCredentialHelper{name: "manager", hasAnswer: true, answer: credential.Answer{Username: "helper-user", Password: []byte("helper-secret")}}
	stubCredentialChain(t, helper)

	src := a.credentialSource()
	got, err := src.Credentials(context.Background(), testResource, false)
	if err != nil {
		t.Fatal(err)
	}
	if got.Username != "vault-user" || string(got.Password) != "vault-secret" {
		t.Fatalf("credentials = %+v, want the vault's entry to take priority", got)
	}
	feedbackOf(t, src).Approve(context.Background(), testResource, got)
	if len(helper.stored) != 0 {
		t.Fatalf("stored = %+v, a vault credential must not be pushed into the helpers", helper.stored)
	}
}

func TestCredentialsSourceVaultThenHelperModeRememberStillSavesToTheVault(t *testing.T) {
	a := newTestApp(t)
	a.cfg.Git.CredentialSource = config.CredentialSourceVaultThenHelper
	v := createTestVault(t, a.paths.VaultFile())
	a.vaultInst = v
	helper := &fakeCredentialHelper{name: "manager"}
	stubCredentialChain(t, helper)
	stubRememberedDialogAnswer(t, a)

	rememberAndApprove(t, a)
	saved, ok := v.Credential(testResource)
	if !ok || saved.Username != "bob" || string(saved.Secret) != "hunter2" {
		t.Fatalf("saved = %+v, ok=%v", saved, ok)
	}
	if len(helper.stored) != 0 {
		t.Fatal("vault+helper mode must not also store the remembered credential into the helper chain")
	}
}

func TestCredentialsSourceHelperModeRejectLogsWarnWhenEraseFails(t *testing.T) {
	a := newTestApp(t)
	a.cfg.Git.CredentialSource = config.CredentialSourceHelper
	buf := captureLog(a)
	helper := &fakeCredentialHelper{name: "manager", hasAnswer: true, answer: credential.Answer{Username: "alice", Password: []byte("stale")}, eraseErr: errors.New("boom")}
	stubCredentialChain(t, helper)

	src := a.credentialSource()
	got, err := src.Credentials(context.Background(), testResource, false)
	if err != nil {
		t.Fatal(err)
	}
	feedbackOf(t, src).Reject(context.Background(), testResource, got)
	if len(helper.erased) != 1 {
		t.Fatalf("erased = %+v, want 1", helper.erased)
	}
	if buf.Len() == 0 {
		t.Fatal("an erase failure must be logged")
	}
}

func TestCredentialsSourceHelperModeApproveLogsWarnWhenStoreFails(t *testing.T) {
	a := newTestApp(t)
	a.cfg.Git.CredentialSource = config.CredentialSourceHelper
	buf := captureLog(a)
	helper := &fakeCredentialHelper{name: "manager", storeErr: errors.New("boom")}
	stubCredentialChain(t, helper)
	stubRememberedDialogAnswer(t, a)

	rememberAndApprove(t, a)
	if len(helper.stored) != 1 {
		t.Fatalf("stored = %+v, want 1 attempt", helper.stored)
	}
	if buf.Len() == 0 {
		t.Fatal("a store failure must be logged")
	}
}

func TestCredentialsSourceVaultThenHelperModeRejectErasesFromTheChainOnly(t *testing.T) {
	a := newTestApp(t)
	a.cfg.Git.CredentialSource = config.CredentialSourceVaultThenHelper
	v := createTestVault(t, a.paths.VaultFile())
	a.vaultInst = v
	setVaultCredential(t, v, "https://other.example", "carol", "unrelated")
	helper := &fakeCredentialHelper{name: "manager", hasAnswer: true, answer: credential.Answer{Username: "alice", Password: []byte("stale")}}
	stubCredentialChain(t, helper)

	src := a.credentialSource()
	got, err := src.Credentials(context.Background(), testResource, false)
	if err != nil {
		t.Fatal(err)
	}
	feedbackOf(t, src).Reject(context.Background(), testResource, got)
	if len(helper.erased) != 1 {
		t.Fatalf("erased = %+v, want 1", helper.erased)
	}
	if !slices.Equal(v.Resources(), []string{"https://other.example"}) {
		t.Fatalf("resources = %v, the vault must be left alone", v.Resources())
	}
}

func TestVaultLookupsForAURLResource(t *testing.T) {
	tests := []struct {
		resource string
		want     []vaultLookup
	}{
		{testResource, []vaultLookup{{key: testResource}, {key: "example.com/org/repo.git", legacy: true}}},
		{"https://bob@example.com:8443/repo.git", []vaultLookup{{key: "https://bob@example.com:8443/repo.git"}, {key: "https://example.com:8443/repo.git", user: "bob"}}},
		{"http://example.com/repo.git", []vaultLookup{{key: "http://example.com/repo.git"}}},
		{"", []vaultLookup{{key: ""}}},
	}
	for _, test := range tests {
		if got := vaultLookupsFor(test.resource); !slices.Equal(got, test.want) {
			t.Fatalf("vaultLookupsFor(%q) = %+v, want %+v", test.resource, got, test.want)
		}
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

	chain, q := defaultCredentialChainAndQuery(a, testResource)
	if len(chain) != 1 {
		t.Fatalf("chain = %+v, want 1 helper", chain)
	}
	if q.Protocol != "https" || q.Host != "example.com" {
		t.Fatalf("query = %+v, want https on example.com", q)
	}
}

func TestDefaultCredentialChainAndQueryKeepsTheSchemePortAndUserOfTheResource(t *testing.T) {
	isolateGitConfig(t)
	dir := t.TempDir()
	target := filepath.Join(dir, "repo")
	initTestRepoWithBranch(t, target, "main")
	appendGitConfig(t, target, "[credential \"http://example.com:8080\"]\n\thelper = store\n[credential \"https://example.com\"]\n\thelper = store --file=/elsewhere\n")

	a := newTestApp(t)
	a.setOpened(openTestRepository(t, target))

	chain, q := defaultCredentialChainAndQuery(a, "http://bob@example.com:8080/org/repo.git")
	if len(chain) != 1 || chain[0].Name() != "store" {
		t.Fatalf("chain = %+v, want only the helper configured for http://example.com:8080", chain)
	}
	want := credential.Query{Protocol: "http", Host: "example.com:8080", Username: "bob"}
	if q != want {
		t.Fatalf("query = %+v, want %+v", q, want)
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

			_, _ = a.credentialSource().Credentials(context.Background(), testResource, false)
			if want := i18n.T(tc.key); seenTarget != want {
				t.Fatalf("mode %q: RememberTarget = %q, want %q", tc.mode, seenTarget, want)
			}
		})
	}
}
