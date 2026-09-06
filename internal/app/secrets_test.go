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
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/settings"
	"github.com/oops1/gogit/internal/ui/unlock"
	"github.com/oops1/gogit/internal/vault"
)

func newSecretsTestView(t *testing.T, a *App) *settings.View {
	t.Helper()
	view, err := settings.NewView(a.Engine(), a.languages, settings.Model{})
	if err != nil {
		t.Fatal(err)
	}
	return view
}

func stubSecretsDefaultSlotKind(t *testing.T, kind vault.SlotKind) {
	t.Helper()
	prev := secretsDefaultSlotKind
	secretsDefaultSlotKind = func() vault.SlotKind { return kind }
	t.Cleanup(func() { secretsDefaultSlotKind = prev })
}

func stubCreateVaultFileWithPassword(t *testing.T, capturedKind *vault.SlotKind) {
	t.Helper()
	prev := createVaultFile
	createVaultFile = func(ctx context.Context, opts vault.Options, first vault.Unlocker) (*vault.Vault, error) {
		if capturedKind != nil {
			*capturedKind = first.Kind()
		}
		return vault.Create(ctx, opts, vault.NewPasswordUnlocker([]byte(testVaultPassword), vault.TestSlotParams()))
	}
	t.Cleanup(func() { createVaultFile = prev })
}

func TestWireSecretsViewInitialRefreshDoesNotCreateAMissingStore(t *testing.T) {
	a := newTestApp(t)
	view := newSecretsTestView(t, a)

	a.wireSecretsView(view)
	secretsWG.Wait()

	if _, err := os.Stat(a.paths.VaultFile()); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("vault file must not be created just by opening settings, stat err = %v", err)
	}
}

func TestCreateSecretsVaultPicksTheUnlockerMatchingTheDefaultSlotKind(t *testing.T) {
	for _, kind := range []vault.SlotKind{vault.SlotDPAPI, vault.SlotSecretService, vault.SlotPassword} {
		t.Run(string(kind), func(t *testing.T) {
			a := newTestApp(t)
			stubSecretsDefaultSlotKind(t, kind)
			var gotKind vault.SlotKind
			stubCreateVaultFileWithPassword(t, &gotKind)
			if kind == vault.SlotPassword {
				stubUnlockDialog(t, a, []unlock.Result{{Password: []byte(testVaultPassword)}})
			}

			v, err := a.createSecretsVault(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = v.Close() })

			if gotKind != kind {
				t.Fatalf("unlocker kind = %v, want %v", gotKind, kind)
			}
			if v != a.vaultInst {
				t.Fatal("the created vault must become the app's vault instance")
			}
		})
	}
}

func TestCreateSecretsVaultPropagatesSetupCancelledWhenThePasswordPromptIsCancelled(t *testing.T) {
	a := newTestApp(t)
	stubSecretsDefaultSlotKind(t, vault.SlotPassword)
	stubUnlockDialog(t, a, nil)

	_, err := a.createSecretsVault(context.Background())
	if !errors.Is(err, ErrSecretsSetupCancelled) {
		t.Fatalf("err = %v, want %v", err, ErrSecretsSetupCancelled)
	}
}

func TestCreateSecretsVaultReturnsTheVaultAlreadyOpenedByAConcurrentCaller(t *testing.T) {
	a := newTestApp(t)
	existing := createTestVault(t, filepath.Join(t.TempDir(), "other-vault.bin"))
	a.vaultInst = existing

	stubSecretsDefaultSlotKind(t, vault.SlotPassword)
	stubCreateVaultFileWithPassword(t, nil)
	stubUnlockDialog(t, a, []unlock.Result{{Password: []byte(testVaultPassword)}})

	v, err := a.createSecretsVault(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if v != existing {
		t.Fatal("createSecretsVault must return the vault instance the app already holds")
	}
}

func TestCreateSecretsVaultPropagatesTheUnderlyingCreateError(t *testing.T) {
	a := newTestApp(t)
	stubSecretsDefaultSlotKind(t, vault.SlotDPAPI)
	prev := createVaultFile
	wantErr := errors.New("boom")
	createVaultFile = func(context.Context, vault.Options, vault.Unlocker) (*vault.Vault, error) {
		return nil, wantErr
	}
	t.Cleanup(func() { createVaultFile = prev })

	_, err := a.createSecretsVault(context.Background())
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
}

func TestSecretsVaultReportsUnavailableWhenTheFileIsCorrupted(t *testing.T) {
	a := newTestApp(t)
	if err := os.WriteFile(a.paths.VaultFile(), []byte("not a vault"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := a.secretsVault(context.Background(), true); !errors.Is(err, ErrSecretsStoreUnavailable) {
		t.Fatalf("err = %v, want %v", err, ErrSecretsStoreUnavailable)
	}
	if _, err := a.secretsVault(context.Background(), false); !errors.Is(err, ErrSecretsStoreUnavailable) {
		t.Fatalf("err = %v, want %v", err, ErrSecretsStoreUnavailable)
	}
}

func TestSecretsVaultReturnsNilWithoutErrorWhenMissingAndNotCreating(t *testing.T) {
	a := newTestApp(t)
	v, err := a.secretsVault(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if v != nil {
		t.Fatal("no vault must be created when createIfMissing is false")
	}
}

func TestSecretsVaultPropagatesAStatErrorThatIsNotNotExist(t *testing.T) {
	a := newTestApp(t)
	a.paths = config.Paths{Dir: "bad\x00dir"}

	_, err := a.secretsVault(context.Background(), true)
	if err == nil || errors.Is(err, ErrSecretsStoreUnavailable) || errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err = %v, want a raw stat error", err)
	}
}

func TestEnsureSecretsUnlockedSucceedsSilentlyWhenASilentUnlockerMatches(t *testing.T) {
	a := newTestApp(t)
	v := createTestVault(t, a.paths.VaultFile())
	v.Lock()

	prev := secretsSilentUnlockers
	secretsSilentUnlockers = func() []vault.Unlocker {
		return []vault.Unlocker{vault.NewPasswordUnlocker([]byte(testVaultPassword), vault.SlotParams{})}
	}
	t.Cleanup(func() { secretsSilentUnlockers = prev })

	if err := a.ensureSecretsUnlocked(context.Background(), v, false); err != nil {
		t.Fatal(err)
	}
	if v.Locked() {
		t.Fatal("the vault must be unlocked silently")
	}
}

func TestEnsureSecretsUnlockedLeavesTheVaultLockedWithoutPromptingWhenNotAllowed(t *testing.T) {
	a := newTestApp(t)
	failIfCredentialsDialogOpens(t)
	v := createTestVault(t, a.paths.VaultFile())
	v.Lock()

	if err := a.ensureSecretsUnlocked(context.Background(), v, false); err != nil {
		t.Fatal(err)
	}
	if !v.Locked() {
		t.Fatal("the vault must stay locked when prompting is not allowed")
	}
}

func TestEnsureSecretsUnlockedPropagatesARealUnlockError(t *testing.T) {
	a := newTestApp(t)
	v := createTestVault(t, a.paths.VaultFile())
	_ = v.Close()
	stubUnlockDialog(t, a, []unlock.Result{{Password: []byte(testVaultPassword)}})

	err := a.ensureSecretsUnlocked(context.Background(), v, true)
	if !errors.Is(err, vault.ErrClosed) {
		t.Fatalf("err = %v, want %v", err, vault.ErrClosed)
	}
}

func TestEnsureSecretsUnlockedFallsBackToThePasswordDialog(t *testing.T) {
	a := newTestApp(t)
	v := createTestVault(t, a.paths.VaultFile())
	v.Lock()
	stubUnlockDialog(t, a, []unlock.Result{{Password: []byte(testVaultPassword)}})

	if err := a.ensureSecretsUnlocked(context.Background(), v, true); err != nil {
		t.Fatal(err)
	}
	if v.Locked() {
		t.Fatal("the vault must be unlocked via the password dialog")
	}
}

func TestEnsureSecretsUnlockedReportsCancelledWhenTheDialogIsCancelled(t *testing.T) {
	a := newTestApp(t)
	v := createTestVault(t, a.paths.VaultFile())
	v.Lock()
	stubUnlockDialog(t, a, nil)

	err := a.ensureSecretsUnlocked(context.Background(), v, true)
	if !errors.Is(err, ErrSecretsUnlockCancelled) {
		t.Fatalf("err = %v, want %v", err, ErrSecretsUnlockCancelled)
	}
	if !v.Locked() {
		t.Fatal("the vault must stay locked when the dialog is cancelled")
	}
}

func TestOnAddCredentialStoresANewCredentialInTheVault(t *testing.T) {
	a := newTestApp(t)
	v := createTestVault(t, a.paths.VaultFile())
	a.vaultInst = v
	view := newSecretsTestView(t, a)
	a.wireSecretsView(view)
	secretsWG.Wait()

	view.OnAddCredential("example.com", "alice", []byte("token"))
	secretsWG.Wait()

	got, ok := v.Credential("example.com")
	if !ok {
		t.Fatal("credential must be stored")
	}
	if got.Username != "alice" || string(got.Secret) != "token" {
		t.Fatalf("credential = %+v", got)
	}
}

func TestOnAddCredentialUnlocksALockedVaultViaThePasswordDialog(t *testing.T) {
	a := newTestApp(t)
	v := createTestVault(t, a.paths.VaultFile())
	v.Lock()
	a.vaultInst = v
	stubUnlockDialog(t, a, []unlock.Result{{Password: []byte(testVaultPassword)}})
	view := newSecretsTestView(t, a)
	a.wireSecretsView(view)
	secretsWG.Wait()

	view.OnAddCredential("example.com", "alice", []byte("token"))
	secretsWG.Wait()

	if v.Locked() {
		t.Fatal("the vault must end up unlocked")
	}
	if _, ok := v.Credential("example.com"); !ok {
		t.Fatal("credential must be stored once unlocked")
	}
}

func TestOnAddCredentialLeavesTheStoreLockedAndDoesNotLogWhenCancelled(t *testing.T) {
	a := newTestApp(t)
	v := createTestVault(t, a.paths.VaultFile())
	v.Lock()
	a.vaultInst = v
	var buf bytes.Buffer
	a.log = slog.New(slog.NewTextHandler(&buf, nil))
	stubUnlockDialog(t, a, nil)
	view := newSecretsTestView(t, a)
	a.wireSecretsView(view)
	secretsWG.Wait()

	view.OnAddCredential("example.com", "alice", []byte("token"))
	secretsWG.Wait()

	if !v.Locked() {
		t.Fatal("the vault must stay locked")
	}
	if _, ok := v.Credential("example.com"); ok {
		t.Fatal("no credential must be stored while locked")
	}
	if buf.Len() != 0 {
		t.Fatalf("errors must surface via the status line, not the log: %s", buf.String())
	}
}

func TestOnAddCredentialCreatesAVaultFileWhenNoneExists(t *testing.T) {
	a := newTestApp(t)
	stubSecretsDefaultSlotKind(t, vault.SlotPassword)
	stubUnlockDialog(t, a, []unlock.Result{{Password: []byte(testVaultPassword)}})
	view := newSecretsTestView(t, a)
	a.wireSecretsView(view)
	secretsWG.Wait()

	view.OnAddCredential("example.com", "alice", []byte("token"))
	secretsWG.Wait()

	if _, err := os.Stat(a.paths.VaultFile()); err != nil {
		t.Fatalf("vault file must be created: %v", err)
	}
	if a.vaultInst == nil {
		t.Fatal("the app must hold the created vault instance")
	}
	got, ok := a.vaultInst.Credential("example.com")
	if !ok || got.Username != "alice" {
		t.Fatalf("credential = %+v, ok = %v", got, ok)
	}
}

func TestOnRemoveCredentialDeletesFromTheVault(t *testing.T) {
	a := newTestApp(t)
	v := createTestVault(t, a.paths.VaultFile())
	if err := v.SetCredential(vault.Credential{Resource: "example.com", Username: "alice", Secret: []byte("token")}); err != nil {
		t.Fatal(err)
	}
	a.vaultInst = v
	view := newSecretsTestView(t, a)
	a.wireSecretsView(view)
	secretsWG.Wait()

	view.OnRemoveCredential("example.com")
	secretsWG.Wait()

	if len(v.Resources()) != 0 {
		t.Fatalf("resources = %v, want none", v.Resources())
	}
}

func TestOnAddKeyStoresANewSSHKeyInTheVault(t *testing.T) {
	a := newTestApp(t)
	v := createTestVault(t, a.paths.VaultFile())
	a.vaultInst = v
	view := newSecretsTestView(t, a)
	a.wireSecretsView(view)
	secretsWG.Wait()

	view.OnAddKey("example.com", "/keys/id_ed25519", []byte("p4ss"))
	secretsWG.Wait()

	got, ok := v.SSHKey("example.com")
	if !ok {
		t.Fatal("key must be stored")
	}
	if got.Path != "/keys/id_ed25519" || string(got.Passphrase) != "p4ss" {
		t.Fatalf("key = %+v", got)
	}
}

func TestOnRemoveKeyDeletesFromTheVault(t *testing.T) {
	a := newTestApp(t)
	v := createTestVault(t, a.paths.VaultFile())
	if err := v.SetSSHKey(vault.SSHKey{Host: "example.com", Path: "/keys/id_ed25519"}); err != nil {
		t.Fatal(err)
	}
	a.vaultInst = v
	view := newSecretsTestView(t, a)
	a.wireSecretsView(view)
	secretsWG.Wait()

	view.OnRemoveKey("example.com")
	secretsWG.Wait()

	if len(v.Hosts()) != 0 {
		t.Fatalf("hosts = %v, want none", v.Hosts())
	}
}

func TestOnSetMasterPasswordAddsANewPasswordSlot(t *testing.T) {
	a := newTestApp(t)
	v := createTestVault(t, a.paths.VaultFile())
	a.vaultInst = v
	before := len(v.Slots())
	stubUnlockDialog(t, a, []unlock.Result{{Password: []byte("a brand new master password")}})
	view := newSecretsTestView(t, a)
	a.wireSecretsView(view)
	secretsWG.Wait()

	view.OnSetMasterPassword()
	secretsWG.Wait()

	if len(v.Slots()) != before+1 {
		t.Fatalf("slots = %d, want %d", len(v.Slots()), before+1)
	}
}

func TestOnSetMasterPasswordDoesNothingWhenTheNewPasswordPromptIsCancelled(t *testing.T) {
	a := newTestApp(t)
	v := createTestVault(t, a.paths.VaultFile())
	a.vaultInst = v
	before := len(v.Slots())
	stubUnlockDialog(t, a, nil)
	view := newSecretsTestView(t, a)
	a.wireSecretsView(view)
	secretsWG.Wait()

	view.OnSetMasterPassword()
	secretsWG.Wait()

	if len(v.Slots()) != before {
		t.Fatalf("slots = %d, want unchanged %d", len(v.Slots()), before)
	}
}

func TestOnUnlockSecretsUnlocksAnExistingLockedVault(t *testing.T) {
	a := newTestApp(t)
	v := createTestVault(t, a.paths.VaultFile())
	v.Lock()
	a.vaultInst = v
	stubUnlockDialog(t, a, []unlock.Result{{Password: []byte(testVaultPassword)}})
	view := newSecretsTestView(t, a)
	a.wireSecretsView(view)
	secretsWG.Wait()

	view.OnUnlockSecrets()
	secretsWG.Wait()

	if v.Locked() {
		t.Fatal("the vault must end up unlocked")
	}
}

func TestOnUnlockSecretsDoesNothingWhenNoStoreExists(t *testing.T) {
	a := newTestApp(t)
	failIfCredentialsDialogOpens(t)
	view := newSecretsTestView(t, a)
	a.wireSecretsView(view)
	secretsWG.Wait()

	view.OnUnlockSecrets()
	secretsWG.Wait()

	if _, err := os.Stat(a.paths.VaultFile()); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("unlocking must not create a vault file")
	}
}

func TestOnBrowseKeyFileCallbackShowsTheNativeFileDialog(t *testing.T) {
	a := newTestApp(t)
	view := newSecretsTestView(t, a)
	a.wireSecretsView(view)
	secretsWG.Wait()

	prev := showOpenFileDialog
	called := false
	showOpenFileDialog = func(eng widget.ModalShower, opts widget.FileDialogOptions, cb func(string, bool)) *widget.FileDialog {
		called = true
		return nil
	}
	t.Cleanup(func() { showOpenFileDialog = prev })

	view.OnBrowseKeyFile()
	if !called {
		t.Fatal("clicking Browse must show the native open-file dialog")
	}
}

func TestOnSetMasterPasswordLeavesTheVaultLockedWhenTheUnlockPromptIsCancelled(t *testing.T) {
	a := newTestApp(t)
	v := createTestVault(t, a.paths.VaultFile())
	v.Lock()
	a.vaultInst = v
	before := len(v.Slots())
	stubUnlockDialog(t, a, nil)
	view := newSecretsTestView(t, a)
	a.wireSecretsView(view)
	secretsWG.Wait()

	view.OnSetMasterPassword()
	secretsWG.Wait()

	if len(v.Slots()) != before {
		t.Fatalf("slots = %d, want unchanged %d", len(v.Slots()), before)
	}
	if !v.Locked() {
		t.Fatal("the vault must stay locked")
	}
}

func TestBrowseKeyFileShowsTheNativeFileDialog(t *testing.T) {
	a := newTestApp(t)
	view := newSecretsTestView(t, a)

	prev := showOpenFileDialog
	called := false
	showOpenFileDialog = func(eng widget.ModalShower, opts widget.FileDialogOptions, cb func(string, bool)) *widget.FileDialog {
		called = true
		cb("/keys/id_ed25519", true)
		return nil
	}
	t.Cleanup(func() { showOpenFileDialog = prev })

	a.browseKeyFile(view)
	if !called {
		t.Fatal("expected the native open-file dialog to be shown")
	}
}

func TestBrowseKeyFileIgnoresACancelledDialog(t *testing.T) {
	a := newTestApp(t)
	view := newSecretsTestView(t, a)

	prev := showOpenFileDialog
	showOpenFileDialog = func(eng widget.ModalShower, opts widget.FileDialogOptions, cb func(string, bool)) *widget.FileDialog {
		cb("", false)
		return nil
	}
	t.Cleanup(func() { showOpenFileDialog = prev })

	a.browseKeyFile(view)
}

func TestSecretsStatusTextCoversLockedEmptyErrorAndReadyStates(t *testing.T) {
	a := newTestApp(t)
	_ = a
	v := createTestVault(t, filepath.Join(t.TempDir(), "status-vault.bin"))

	if got, want := secretsStatusText(nil, nil), i18n.T("Dialog.Settings.Secrets.Empty"); got != want {
		t.Fatalf("no store = %q, want %q", got, want)
	}

	v.Lock()
	if got, want := secretsStatusText(v, nil), i18n.T("Dialog.Settings.Secrets.Locked"); got != want {
		t.Fatalf("locked = %q, want %q", got, want)
	}

	if err := v.Unlock(context.Background(), vault.NewPasswordUnlocker([]byte(testVaultPassword), vault.SlotParams{})); err != nil {
		t.Fatal(err)
	}
	if got, want := secretsStatusText(v, nil), i18n.T("Dialog.Settings.Secrets.Empty"); got != want {
		t.Fatalf("unlocked empty = %q, want %q", got, want)
	}

	if err := v.SetCredential(vault.Credential{Resource: "example.com", Username: "a", Secret: []byte("s")}); err != nil {
		t.Fatal(err)
	}
	if got := secretsStatusText(v, nil); got != "" {
		t.Fatalf("unlocked with data = %q, want empty", got)
	}

	if got, want := secretsStatusText(v, vault.ErrWrongKey), i18n.T("Dialog.Unlock.Wrong"); got != want {
		t.Fatalf("wrong key = %q, want %q", got, want)
	}
	if got, want := secretsStatusText(v, vault.ErrLocked), i18n.T("Dialog.Settings.Secrets.Locked"); got != want {
		t.Fatalf("locked err = %q, want %q", got, want)
	}
	if got, want := secretsStatusText(v, errors.New("boom")), i18n.T("Dialog.Settings.Secrets.Error"); got != want {
		t.Fatalf("generic error = %q, want %q", got, want)
	}
}

func TestShowOpenFileDialogDefaultOpensAModalFileDialog(t *testing.T) {
	a := newTestApp(t)

	dlg := showOpenFileDialog(a.Engine(), widget.FileDialogOptions{}, func(string, bool) {})
	if dlg == nil {
		t.Fatal("expected the default implementation to return the shown file dialog")
	}
	if !dlg.Dialog().IsModal() {
		t.Fatal("the default implementation must show the file dialog modally")
	}
}

func TestSecretsDefaultSlotKindDefaultReturnsAKnownKind(t *testing.T) {
	switch got := secretsDefaultSlotKind(); got {
	case vault.SlotDPAPI, vault.SlotSecretService, vault.SlotPassword:
	default:
		t.Fatalf("secretsDefaultSlotKind() = %v, want a known slot kind", got)
	}
}
