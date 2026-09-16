package app

import (
	"context"
	"errors"
	"image/color"
	"io/fs"
	"sync"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/masterpassword"
	"github.com/oops1/gogit/internal/ui/settings"
	"github.com/oops1/gogit/internal/vault"
)

var secretsErrorTextColor = color.RGBA{R: 220, G: 80, B: 80, A: 255}

const secretServiceLabel = "gogit-vault"

var (
	ErrSecretsStoreUnavailable = errors.New("app: secrets store unavailable")
	ErrSecretsSetupCancelled   = errors.New("app: secrets setup cancelled")
	ErrSecretsUnlockCancelled  = errors.New("app: secrets unlock cancelled")
	ErrSecretsKeyUnavailable   = errors.New("app: the key of the secrets store is unavailable")
)

var (
	createVaultFile       = vault.Create
	newMasterPasswordView = masterpassword.NewView
	showOpenFileDialog    = func(eng widget.ModalShower, opts widget.FileDialogOptions, cb func(string, bool)) *widget.FileDialog {
		return widget.NewMessageBox(eng).ShowOpenFile(opts, cb)
	}
	secretsDefaultSlotKind = func() vault.SlotKind {
		return vault.DefaultSlotKind(vault.SecretServiceAvailable)
	}
	secretsSilentUnlockers = func() []vault.Unlocker {
		return []vault.Unlocker{vault.NewDPAPIUnlocker(), vault.NewSecretServiceUnlocker(secretServiceLabel)}
	}
)

var secretsWG sync.WaitGroup

func (a *App) wireSecretsView(view *settings.View) {
	view.OnAddCredential = func(resource, username string, secret []byte) {
		cred := vault.Credential{Resource: credentialResourceKey(resource), Username: username, Secret: append([]byte(nil), secret...)}
		secretsWG.Go(func() {
			defer cred.Wipe()
			a.runSecretsAction(view, true, true, func(ctx context.Context, v *vault.Vault) error {
				return v.SetCredential(cred)
			})
		})
	}
	view.OnRemoveCredential = func(resource string) {
		secretsWG.Go(func() {
			a.runSecretsAction(view, false, true, func(ctx context.Context, v *vault.Vault) error {
				return v.DeleteCredential(resource)
			})
		})
	}
	view.OnAddKey = func(host, path string, passphrase []byte) {
		key := vault.SSHKey{Host: host, Path: path, Passphrase: append([]byte(nil), passphrase...)}
		secretsWG.Go(func() {
			defer key.Wipe()
			a.runSecretsAction(view, true, true, func(ctx context.Context, v *vault.Vault) error {
				return v.SetSSHKey(key)
			})
		})
	}
	view.OnRemoveKey = func(host string) {
		secretsWG.Go(func() {
			a.runSecretsAction(view, false, true, func(ctx context.Context, v *vault.Vault) error {
				return v.DeleteSSHKey(host)
			})
		})
	}
	view.OnSetMasterPassword = func() {
		secretsWG.Go(func() { a.setMasterPassword(view, false) })
	}
	view.OnUnlockSecrets = func() {
		secretsWG.Go(func() { a.runSecretsAction(view, false, true, nil) })
	}
	view.OnBrowseKeyFile = func() {
		a.browseKeyFile(view)
	}
	view.OnTestConnection = func(section string) {
		a.testSecretsConnection(view, section)
	}
	view.OnCredentialSource = func(source string) {
		a.refreshCredentialSourceInfoFor(view, source)
	}
	secretsWG.Go(func() { a.runSecretsAction(view, false, false, nil) })
}

func (a *App) runSecretsAction(view *settings.View, createIfMissing, allowPrompt bool, fn func(ctx context.Context, v *vault.Vault) error) {
	ctx := context.Background()
	v, created, err := a.secretsVault(ctx, createIfMissing)
	if err == nil {
		err = a.ensureSecretsUnlocked(ctx, v, allowPrompt)
	}
	if err == nil && v != nil && !v.Locked() {
		err = a.acceptReplacedVault(v)
		if err == nil && fn != nil {
			err = fn(ctx, v)
		}
	}
	a.pushSecretsState(view, v, err)
	if err == nil && created && !v.HasSlot(vault.SlotPassword) {
		a.offerBackupPassword(ctx, view)
	}
}

func (a *App) setMasterPassword(view *settings.View, backup bool) {
	ctx := context.Background()
	v, created, err := a.secretsVault(ctx, true)
	if err == nil {
		err = a.ensureSecretsUnlocked(ctx, v, true)
	}
	if err == nil && !v.Locked() && (!created || !v.HasSlot(vault.SlotPassword)) {
		err = a.changeMasterPassword(ctx, v, backup)
	}
	a.pushSecretsState(view, v, err)
	if err == nil {
		a.reportSecretsCheck(view, i18n.T("Dialog.Settings.Secrets.PasswordChanged"), false)
	}
}

func (a *App) changeMasterPassword(ctx context.Context, v *vault.Vault, backup bool) error {
	req := masterpassword.Request{Change: v.HasSlot(vault.SlotPassword), Backup: backup}
	for {
		res, ok := a.askNewMasterPassword(ctx, req)
		if !ok {
			return ErrSecretsSetupCancelled
		}
		proof := vault.NewPasswordUnlocker(res.Current, vault.SlotParams{})
		next := vault.NewPasswordUnlocker(res.Password, vault.DefaultSlotParams())
		res.Wipe()
		var current *vault.PasswordUnlocker
		if req.Change {
			current = proof
		}
		err := v.ChangePassword(ctx, current, next, secretsSilentUnlockers())
		proof.Wipe()
		next.Wipe()
		if !req.Change || !errors.Is(err, vault.ErrWrongKey) {
			return err
		}
		req.WrongCurrent = true
	}
}

func (a *App) askNewMasterPassword(ctx context.Context, req masterpassword.Request) (masterpassword.Result, bool) {
	type response struct {
		result masterpassword.Result
		ok     bool
	}
	ch := make(chan response, 1)
	a.Post(func() {
		view, err := newMasterPasswordView(req)
		if err != nil {
			a.log.Warn("open master password dialog failed", "error", err)
			ch <- response{}
			return
		}
		view.OnOK = func(res masterpassword.Result) {
			a.eng.CloseModal(view.Dialog())
			ch <- response{result: res, ok: true}
		}
		view.OnCancel = func() {
			a.eng.CloseModal(view.Dialog())
			ch <- response{}
		}
		a.showModal(view.Dialog(), view)
	})
	select {
	case r := <-ch:
		return r.result, r.ok
	case <-ctx.Done():
		return masterpassword.Result{}, false
	}
}

func (a *App) askSecretsQuestion(ctx context.Context, titleKey, messageKey string) bool {
	ch := make(chan bool, 1)
	a.Post(func() {
		a.askConfirm(i18n.T(titleKey), i18n.T(messageKey), func(ok bool) { ch <- ok })
	})
	select {
	case ok := <-ch:
		return ok
	case <-ctx.Done():
		return false
	}
}

func (a *App) offerBackupPassword(ctx context.Context, view *settings.View) {
	if a.askSecretsQuestion(ctx, "Dialog.BackupPassword.Title", "Dialog.BackupPassword.Message") {
		a.setMasterPassword(view, true)
	}
}

func (a *App) vaultOptions(known uint64) vault.Options {
	return vault.Options{
		Path:            a.paths.VaultFile(),
		IdleTime:        vaultIdleTime,
		KnownGeneration: known,
		OnGeneration:    a.noteVaultGeneration,
	}
}

func (a *App) knownVaultGeneration() uint64 {
	a.vaultGenMu.Lock()
	defer a.vaultGenMu.Unlock()
	return a.vaultGen
}

func (a *App) noteVaultGeneration(generation uint64) {
	a.vaultGenMu.Lock()
	raised := generation > a.vaultGen
	if raised {
		a.vaultGen = generation
	}
	a.vaultGenMu.Unlock()
	if !raised {
		return
	}
	a.Post(func() {
		if a.cfg.Security.VaultGeneration >= generation {
			return
		}
		a.cfg.Security.VaultGeneration = generation
		if err := a.cfg.Save(a.paths.ConfigFile()); err != nil {
			a.log.Warn("save config failed", "error", err)
		}
	})
}

func (a *App) openVault() (*vault.Vault, error) {
	a.vaultMu.Lock()
	defer a.vaultMu.Unlock()
	if a.vaultInst != nil {
		return a.vaultInst, nil
	}
	v, err := openVaultFile(a.vaultOptions(a.knownVaultGeneration()))
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			a.log.Warn("open vault failed", "error", err)
		}
		return nil, err
	}
	a.vaultInst = v
	return v, nil
}

func (a *App) openReplacedVault(ctx context.Context) (*vault.Vault, error) {
	if !a.askSecretsQuestion(ctx, "Dialog.VaultRollback.Title", "Dialog.VaultRollback.Message") {
		return nil, vault.ErrRollback
	}
	known := a.knownVaultGeneration()
	v, err := openVaultFile(a.vaultOptions(0))
	if err != nil {
		return nil, errors.Join(ErrSecretsStoreUnavailable, err)
	}
	a.vaultMu.Lock()
	a.vaultInst = v
	a.vaultMu.Unlock()
	a.vaultGenMu.Lock()
	a.vaultAccept, a.vaultAcceptPending = known, true
	a.vaultGenMu.Unlock()
	return v, nil
}

func (a *App) acceptReplacedVault(v *vault.Vault) error {
	a.vaultGenMu.Lock()
	known, pending := a.vaultAccept, a.vaultAcceptPending
	a.vaultAcceptPending = false
	a.vaultGenMu.Unlock()
	if !pending {
		return nil
	}
	return v.AcceptGeneration(known)
}

func (a *App) secretsVault(ctx context.Context, createIfMissing bool) (*vault.Vault, bool, error) {
	v, err := a.openVault()
	switch {
	case err == nil:
		return v, false, nil
	case errors.Is(err, vault.ErrRollback):
		v, err = a.openReplacedVault(ctx)
		return v, false, err
	case !errors.Is(err, fs.ErrNotExist):
		return nil, false, errors.Join(ErrSecretsStoreUnavailable, err)
	case !createIfMissing:
		return nil, false, nil
	}
	v, err = a.createSecretsVault(ctx)
	return v, err == nil, err
}

func (a *App) createSecretsVault(ctx context.Context) (*vault.Vault, error) {
	var (
		unlocker vault.Unlocker
		password []byte
	)
	switch secretsDefaultSlotKind() {
	case vault.SlotDPAPI:
		unlocker = vault.NewDPAPIUnlocker()
	case vault.SlotSecretService:
		unlocker = vault.NewSecretServiceUnlocker(secretServiceLabel)
	default:
		res, ok := a.askNewMasterPassword(ctx, masterpassword.Request{})
		if !ok {
			return nil, ErrSecretsSetupCancelled
		}
		password = res.Password
		unlocker = vault.NewPasswordUnlocker(password, vault.DefaultSlotParams())
	}
	v, err := createVaultFile(ctx, a.vaultOptions(a.knownVaultGeneration()), unlocker)
	clear(password)
	if err != nil {
		return nil, err
	}
	a.vaultMu.Lock()
	if a.vaultInst != nil {
		existing := a.vaultInst
		a.vaultMu.Unlock()
		_ = v.Close()
		return existing, nil
	}
	a.vaultInst = v
	a.vaultMu.Unlock()
	return v, nil
}

func (a *App) ensureSecretsUnlocked(ctx context.Context, v *vault.Vault, allowPrompt bool) error {
	if v == nil || !v.Locked() {
		return nil
	}
	if !allowPrompt {
		_ = a.trySilentUnlock(ctx, v)
		return nil
	}
	unlocked, err := a.unlockVault(ctx, v)
	if err != nil {
		return err
	}
	if !unlocked {
		return ErrSecretsUnlockCancelled
	}
	return nil
}

func (a *App) trySilentUnlock(ctx context.Context, v *vault.Vault) error {
	err := vault.ErrSlotNotFound
	for _, u := range secretsSilentUnlockers() {
		unlockErr := v.Unlock(ctx, u)
		if unlockErr == nil {
			return nil
		}
		if !errors.Is(unlockErr, vault.ErrSlotNotFound) {
			err = unlockErr
		}
	}
	return err
}

func (a *App) pushSecretsState(view *settings.View, v *vault.Vault, err error) {
	if errors.Is(err, ErrSecretsUnlockCancelled) || errors.Is(err, ErrSecretsSetupCancelled) {
		err = nil
	}
	var (
		creds  []settings.SecretEntry
		keys   []settings.KeyEntry
		locked bool
	)
	if v != nil {
		if v.Locked() {
			locked = true
		} else {
			creds = secretEntries(v)
			keys = keyEntries(v)
		}
	}
	status := secretsStatusText(v, err)
	isError := secretsStatusIsError(err)
	a.Post(func() {
		view.SetCredentials(creds)
		view.SetKeys(keys)
		view.SetSecretsLocked(locked)
		view.SetSecretsStatus(status, a.secretsStatusColor(isError))
	})
}

func secretsStatusIsError(err error) bool {
	return err != nil && !errors.Is(err, vault.ErrLocked)
}

func (a *App) secretsStatusColor(isError bool) color.RGBA {
	if isError {
		return secretsErrorTextColor
	}
	return a.theme().LabelText
}

func secretsStatusText(v *vault.Vault, err error) string {
	switch {
	case errors.Is(err, vault.ErrKeyringLocked):
		return i18n.T("Dialog.Settings.Secrets.KeyringLocked")
	case errors.Is(err, vault.ErrKeyNotStored):
		return i18n.T("Dialog.Settings.Secrets.KeyNotStored")
	case errors.Is(err, ErrSecretsKeyUnavailable), errors.Is(err, vault.ErrSlotUnavailable):
		return i18n.T("Dialog.Settings.Secrets.KeyUnavailable")
	case errors.Is(err, vault.ErrChangedElsewhere):
		return i18n.T("Dialog.Settings.Secrets.ChangedElsewhere")
	case errors.Is(err, vault.ErrRollback):
		return i18n.T("Dialog.Settings.Secrets.Rollback")
	case errors.Is(err, vault.ErrWrongKey):
		return i18n.T("Dialog.Unlock.Wrong")
	case errors.Is(err, vault.ErrLocked):
		return i18n.T("Dialog.Settings.Secrets.Locked")
	case err != nil:
		return i18n.T("Dialog.Settings.Secrets.Error")
	case v == nil:
		return i18n.T("Dialog.Settings.Secrets.Empty")
	case v.Locked():
		return i18n.T("Dialog.Settings.Secrets.Locked")
	case len(v.Resources()) == 0 && len(v.Hosts()) == 0:
		return i18n.T("Dialog.Settings.Secrets.Empty")
	default:
		return ""
	}
}

func secretEntries(v *vault.Vault) []settings.SecretEntry {
	resources := v.Resources()
	out := make([]settings.SecretEntry, 0, len(resources))
	for _, r := range resources {
		c, ok := v.Credential(r)
		if !ok {
			continue
		}
		out = append(out, settings.SecretEntry{Resource: c.Resource, Username: c.Username})
		c.Wipe()
	}
	return out
}

func keyEntries(v *vault.Vault) []settings.KeyEntry {
	hosts := v.Hosts()
	out := make([]settings.KeyEntry, 0, len(hosts))
	for _, h := range hosts {
		k, ok := v.SSHKey(h)
		if !ok {
			continue
		}
		out = append(out, settings.KeyEntry{Host: k.Host, Path: k.Path})
		k.Wipe()
	}
	return out
}

func (a *App) browseKeyFile(view *settings.View) {
	showOpenFileDialog(a.eng, widget.FileDialogOptions{}, func(path string, ok bool) {
		if !ok {
			return
		}
		view.SetKeyPath(path)
	})
}
