package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/masterpassword"
	"github.com/oops1/gogit/internal/ui/unlock"
	"github.com/oops1/gogit/internal/vault"
)

func stubMasterPasswordDialog(t *testing.T, a *App, answers []masterpassword.Result) func() []masterpassword.Request {
	t.Helper()
	var (
		mu       sync.Mutex
		requests []masterpassword.Request
	)
	prev := newMasterPasswordView
	newMasterPasswordView = func(req masterpassword.Request) (*masterpassword.View, error) {
		view, err := prev(req)
		if err != nil {
			return nil, err
		}
		mu.Lock()
		call := len(requests)
		requests = append(requests, req)
		mu.Unlock()
		cancelled := call >= len(answers)
		var result masterpassword.Result
		if !cancelled {
			result = masterpassword.Result{
				Current:  append([]byte(nil), answers[call].Current...),
				Password: append([]byte(nil), answers[call].Password...),
			}
		}
		a.Post(func() {
			if cancelled {
				view.OnCancel()
			} else {
				view.OnOK(result)
			}
		})
		return view, nil
	}
	t.Cleanup(func() { newMasterPasswordView = prev })
	return func() []masterpassword.Request {
		mu.Lock()
		defer mu.Unlock()
		return append([]masterpassword.Request(nil), requests...)
	}
}

func blockFileWrites(t *testing.T, path string) {
	t.Helper()
	lock := path + ".lock"
	if err := os.Remove(lock); err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	if err := os.MkdirAll(lock, 0o700); err != nil {
		t.Fatal(err)
	}
}

func stubSilentUnlockers(t *testing.T, unlockers ...vault.Unlocker) {
	t.Helper()
	prev := secretsSilentUnlockers
	secretsSilentUnlockers = func() []vault.Unlocker { return unlockers }
	t.Cleanup(func() { secretsSilentUnlockers = prev })
}

func createKeyFileVault(t *testing.T, path, keyPath string) *vault.Vault {
	t.Helper()
	v, err := vault.Create(context.Background(), vault.Options{Path: path}, vault.NewFileUnlocker(keyPath))
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestChangeMasterPasswordAsksAgainAfterAWrongCurrentPassword(t *testing.T) {
	a := newTestApp(t)
	v := createTestVault(t, a.paths.VaultFile())
	requests := stubMasterPasswordDialog(t, a, []masterpassword.Result{
		{Current: []byte("not the password"), Password: []byte("first new password")},
		{Current: []byte(testVaultPassword), Password: []byte("second new password")},
	})

	if err := a.changeMasterPassword(context.Background(), v, false); err != nil {
		t.Fatal(err)
	}

	got := requests()
	if len(got) != 2 || got[0].WrongCurrent || !got[1].WrongCurrent || !got[1].Change {
		t.Fatalf("requests = %+v", got)
	}
	reopened, err := vault.Open(vault.Options{Path: a.paths.VaultFile()})
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.Unlock(context.Background(), vault.NewPasswordUnlocker([]byte("second new password"), vault.SlotParams{})); err != nil {
		t.Fatal(err)
	}
}

func TestChangeMasterPasswordReturnsOtherVaultErrors(t *testing.T) {
	a := newTestApp(t)
	v := createTestVault(t, a.paths.VaultFile())
	_ = v.Close()
	stubMasterPasswordDialog(t, a, []masterpassword.Result{{Current: []byte(testVaultPassword), Password: []byte("new password")}})
	if err := a.changeMasterPassword(context.Background(), v, false); !errors.Is(err, vault.ErrClosed) {
		t.Fatalf("err = %v, want %v", err, vault.ErrClosed)
	}
}

func TestChangeMasterPasswordAddsABackupPasswordWithoutAskingForACurrentOne(t *testing.T) {
	a := newTestApp(t)
	keyPath := filepath.Join(t.TempDir(), "vault.key")
	v := createKeyFileVault(t, a.paths.VaultFile(), keyPath)
	stubSilentUnlockers(t, vault.NewFileUnlocker(keyPath))
	requests := stubMasterPasswordDialog(t, a, []masterpassword.Result{{Password: []byte("backup password")}})

	if err := a.changeMasterPassword(context.Background(), v, true); err != nil {
		t.Fatal(err)
	}
	if got := requests(); len(got) != 1 || got[0].Change || !got[0].Backup {
		t.Fatalf("requests = %+v", got)
	}
	if !v.HasSlot(vault.SlotPassword) || !v.HasSlot(vault.SlotFile) {
		t.Fatalf("slots = %+v", v.Slots())
	}
}

func TestSetMasterPasswordDoesNotAskTwiceForAStoreJustCreatedWithAPassword(t *testing.T) {
	a := newTestApp(t)
	stubSecretsDefaultSlotKind(t, vault.SlotPassword)
	stubCreateVaultFileWithPassword(t, nil)
	requests := stubMasterPasswordDialog(t, a, []masterpassword.Result{{Password: []byte(testVaultPassword)}})
	view := newSecretsTestView(t, a)

	a.setMasterPassword(view, false)

	if got := requests(); len(got) != 1 {
		t.Fatalf("requests = %+v, want one", got)
	}
	if a.vaultInst == nil || len(a.vaultInst.Slots()) != 1 {
		t.Fatal("the created store must keep its single password slot")
	}
}

func TestCreatingAStoreWithoutAPasswordOffersABackupPassword(t *testing.T) {
	for _, accept := range []bool{true, false} {
		t.Run(map[bool]string{true: "accepted", false: "declined"}[accept], func(t *testing.T) {
			a := newTestApp(t)
			keyPath := filepath.Join(t.TempDir(), "vault.key")
			stubSecretsDefaultSlotKind(t, vault.SlotDPAPI)
			prev := createVaultFile
			createVaultFile = func(ctx context.Context, opts vault.Options, _ vault.Unlocker) (*vault.Vault, error) {
				return vault.Create(ctx, opts, vault.NewFileUnlocker(keyPath))
			}
			t.Cleanup(func() { createVaultFile = prev })
			stubSilentUnlockers(t, vault.NewFileUnlocker(keyPath))
			asked := 0
			a.askConfirm = func(_, _ string, cb func(bool)) {
				asked++
				cb(accept)
			}
			requests := stubMasterPasswordDialog(t, a, []masterpassword.Result{{Password: []byte("backup password")}})
			view := newSecretsTestView(t, a)

			a.runSecretsAction(view, true, true, func(context.Context, *vault.Vault) error { return nil })

			if asked != 1 {
				t.Fatalf("asked = %d, want 1", asked)
			}
			if got := requests(); accept != (len(got) == 1) {
				t.Fatalf("requests = %+v", got)
			}
			if a.vaultInst.HasSlot(vault.SlotPassword) != accept {
				t.Fatalf("slots = %+v", a.vaultInst.Slots())
			}
		})
	}
}

func TestAskSecretsQuestionGivesUpWhenTheContextEnds(t *testing.T) {
	a := newTestApp(t)
	a.askConfirm = func(string, string, func(bool)) {}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if a.askSecretsQuestion(ctx, "Dialog.VaultRollback.Title", "Dialog.VaultRollback.Message") {
		t.Fatal("an unanswered question must count as no")
	}
}

func TestAskNewMasterPasswordReportsADialogThatCannotOpen(t *testing.T) {
	a := newTestApp(t)
	buf := captureLog(a)
	prev := newMasterPasswordView
	newMasterPasswordView = func(masterpassword.Request) (*masterpassword.View, error) {
		return nil, errors.New("no dialog")
	}
	t.Cleanup(func() { newMasterPasswordView = prev })

	if _, ok := a.askNewMasterPassword(context.Background(), masterpassword.Request{}); ok {
		t.Fatal("expected no answer")
	}
	if !strings.Contains(buf.String(), "open master password dialog failed") {
		t.Fatalf("log = %s", buf.String())
	}
}

func TestAskNewMasterPasswordGivesUpWhenTheContextEnds(t *testing.T) {
	a := newTestApp(t)
	prev := newMasterPasswordView
	opened := make(chan struct{})
	newMasterPasswordView = func(req masterpassword.Request) (*masterpassword.View, error) {
		view, err := prev(req)
		close(opened)
		return view, err
	}
	t.Cleanup(func() { newMasterPasswordView = prev })
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan bool, 1)
	go func() {
		_, ok := a.askNewMasterPassword(ctx, masterpassword.Request{})
		done <- ok
	}()
	<-opened
	cancel()
	if <-done {
		t.Fatal("expected no answer once the context ends")
	}
}

func TestNoteVaultGenerationRemembersOnlyNewerGenerations(t *testing.T) {
	a := newTestApp(t)
	a.noteVaultGeneration(4)
	drainPostQueue(t, a)
	if got := readOnDispatcher(t, a, func() uint64 { return a.cfg.Security.VaultGeneration }); got != 4 {
		t.Fatalf("config generation = %d, want 4", got)
	}
	cfg, err := os.ReadFile(a.paths.ConfigFile())
	if err != nil || !strings.Contains(string(cfg), "vault_generation = 4") {
		t.Fatalf("config file = %s, err = %v", cfg, err)
	}

	a.noteVaultGeneration(3)
	runOnDispatcher(t, a, func() { a.cfg.Security.VaultGeneration = 9 })
	a.noteVaultGeneration(6)
	drainPostQueue(t, a)
	if got := readOnDispatcher(t, a, func() uint64 { return a.cfg.Security.VaultGeneration }); got != 9 {
		t.Fatalf("config generation = %d, want the higher value kept", got)
	}
	if a.knownVaultGeneration() != 6 {
		t.Fatalf("known generation = %d, want 6", a.knownVaultGeneration())
	}
}

func TestNoteVaultGenerationLogsAConfigThatCannotBeSaved(t *testing.T) {
	a := newTestApp(t)
	buf := captureLog(a)
	if err := os.Mkdir(a.paths.ConfigFile()+".lock", 0o700); err != nil {
		t.Fatal(err)
	}
	a.noteVaultGeneration(2)
	if got := readOnDispatcher(t, a, buf.String); !strings.Contains(got, "save config failed") {
		t.Fatalf("log = %s", got)
	}
}

func TestReplacedStoreIsRefusedUnlessTheUserAcceptsIt(t *testing.T) {
	a := newTestApp(t)
	v := createTestVault(t, a.paths.VaultFile())
	generation := v.Generation()
	_ = v.Close()
	a.vaultGen = generation + 5
	view := newSecretsTestView(t, a)

	a.askConfirm = func(_, _ string, cb func(bool)) { cb(false) }
	a.runSecretsAction(view, false, true, nil)
	if a.vaultInst != nil {
		t.Fatal("a declined replaced store must not be opened")
	}
	if _, created, err := a.secretsVault(context.Background(), true); created || !errors.Is(err, vault.ErrRollback) {
		t.Fatalf("created = %v, err = %v", created, err)
	}

	a.askConfirm = func(_, _ string, cb func(bool)) { cb(true) }
	stubUnlockDialog(t, a, []unlock.Result{{Password: []byte(testVaultPassword)}})
	a.runSecretsAction(view, false, true, nil)
	if a.vaultInst == nil || a.vaultInst.Locked() {
		t.Fatal("an accepted replaced store must open and unlock")
	}
	if got := a.vaultInst.Generation(); got != generation+6 {
		t.Fatalf("generation = %d, want %d", got, generation+6)
	}
	drainPostQueue(t, a)
	if got := readOnDispatcher(t, a, func() uint64 { return a.cfg.Security.VaultGeneration }); got != generation+6 {
		t.Fatalf("config generation = %d", got)
	}
	if _, err := vault.Open(vault.Options{Path: a.paths.VaultFile(), KnownGeneration: generation + 6}); err != nil {
		t.Fatal(err)
	}
}

func TestReplacedStoreThatDisappearsIsReportedUnavailable(t *testing.T) {
	a := newTestApp(t)
	calls := 0
	prev := openVaultFile
	openVaultFile = func(vault.Options) (*vault.Vault, error) {
		calls++
		if calls == 1 {
			return nil, vault.ErrRollback
		}
		return nil, errors.New("gone")
	}
	t.Cleanup(func() { openVaultFile = prev })
	a.askConfirm = func(_, _ string, cb func(bool)) { cb(true) }

	if _, _, err := a.secretsVault(context.Background(), false); !errors.Is(err, ErrSecretsStoreUnavailable) {
		t.Fatalf("err = %v, want %v", err, ErrSecretsStoreUnavailable)
	}
}

func TestAStoreWithoutAPasswordReportsAnUnavailableKeyInsteadOfPrompting(t *testing.T) {
	a := newTestApp(t)
	keyPath := filepath.Join(t.TempDir(), "vault.key")
	v := createKeyFileVault(t, a.paths.VaultFile(), keyPath)
	v.Lock()
	a.vaultInst = v
	if err := os.Remove(keyPath); err != nil {
		t.Fatal(err)
	}
	stubSilentUnlockers(t, vault.NewDPAPIUnlocker(), vault.NewFileUnlocker(keyPath))

	err := a.ensureSecretsUnlocked(context.Background(), v, true)
	if !errors.Is(err, ErrSecretsKeyUnavailable) {
		t.Fatalf("err = %v, want %v", err, ErrSecretsKeyUnavailable)
	}
	if got := secretsStatusText(v, err); got != i18n.T("Dialog.Settings.Secrets.KeyUnavailable") {
		t.Fatalf("status = %q", got)
	}
	if err := a.ensureSecretsUnlocked(context.Background(), v, false); err != nil {
		t.Fatalf("a silent refresh must not report, got %v", err)
	}
}

func TestUnlockVaultUsesASilentUnlockerBeforeAskingForAPassword(t *testing.T) {
	a := newTestApp(t)
	keyPath := filepath.Join(t.TempDir(), "vault.key")
	v := createKeyFileVault(t, a.paths.VaultFile(), keyPath)
	if err := v.AddSlot(context.Background(), vault.NewPasswordUnlocker([]byte(testVaultPassword), vault.TestSlotParams())); err != nil {
		t.Fatal(err)
	}
	v.Lock()
	stubSilentUnlockers(t, vault.NewFileUnlocker(keyPath))
	prev := newUnlockView
	newUnlockView = func(widget.ModalShower, unlock.Request) (*unlock.View, error) {
		t.Error("the password dialog must not open when a silent unlocker works")
		return nil, errors.New("unexpected dialog")
	}
	t.Cleanup(func() { newUnlockView = prev })

	if err := a.ensureSecretsUnlocked(context.Background(), v, true); err != nil {
		t.Fatal(err)
	}
	if v.Locked() {
		t.Fatal("the store must be unlocked silently")
	}
}

func TestSecretsStatusTextExplainsKeyringAndStoreProblems(t *testing.T) {
	newTestApp(t)
	cases := map[string]error{
		"Dialog.Settings.Secrets.KeyringLocked":    errors.Join(ErrSecretsKeyUnavailable, vault.ErrKeyringLocked),
		"Dialog.Settings.Secrets.KeyNotStored":     vault.ErrKeyNotStored,
		"Dialog.Settings.Secrets.KeyUnavailable":   vault.ErrSlotUnavailable,
		"Dialog.Settings.Secrets.ChangedElsewhere": vault.ErrChangedElsewhere,
		"Dialog.Settings.Secrets.Rollback":         vault.ErrRollback,
	}
	for key, err := range cases {
		if got := secretsStatusText(nil, err); got != i18n.T(key) {
			t.Fatalf("%v: status = %q, want %q", err, got, i18n.T(key))
		}
	}
}
