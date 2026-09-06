package app

import (
	"context"
	"errors"
	"testing"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/gitcore/transport"
	"github.com/oops1/gogit/internal/ui/credentials"
	"github.com/oops1/gogit/internal/ui/unlock"
	"github.com/oops1/gogit/internal/vault"
)

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
