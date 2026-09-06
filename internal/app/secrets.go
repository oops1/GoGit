package app

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"sync"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/settings"
	"github.com/oops1/gogit/internal/vault"
)

const secretServiceLabel = "gogit-vault"

var (
	ErrSecretsStoreUnavailable = errors.New("app: secrets store unavailable")
	ErrSecretsSetupCancelled   = errors.New("app: secrets setup cancelled")
	ErrSecretsUnlockCancelled  = errors.New("app: secrets unlock cancelled")
)

var (
	createVaultFile    = vault.Create
	showOpenFileDialog = func(eng widget.ModalShower, opts widget.FileDialogOptions, cb func(string, bool)) *widget.FileDialog {
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
		cred := vault.Credential{Resource: resource, Username: username, Secret: append([]byte(nil), secret...)}
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
		secretsWG.Go(func() { a.setMasterPassword(view) })
	}
	view.OnUnlockSecrets = func() {
		secretsWG.Go(func() { a.runSecretsAction(view, false, true, nil) })
	}
	view.OnBrowseKeyFile = func() {
		a.browseKeyFile(view)
	}
	secretsWG.Go(func() { a.runSecretsAction(view, false, false, nil) })
}

func (a *App) runSecretsAction(view *settings.View, createIfMissing, allowPrompt bool, fn func(ctx context.Context, v *vault.Vault) error) {
	ctx := context.Background()
	v, err := a.secretsVault(ctx, createIfMissing)
	if err == nil {
		err = a.ensureSecretsUnlocked(ctx, v, allowPrompt)
	}
	if err == nil && v != nil && !v.Locked() && fn != nil {
		err = fn(ctx, v)
	}
	a.pushSecretsState(view, v, err)
}

func (a *App) setMasterPassword(view *settings.View) {
	ctx := context.Background()
	v, err := a.secretsVault(ctx, true)
	if err == nil {
		err = a.ensureSecretsUnlocked(ctx, v, true)
	}
	if err == nil && v != nil && !v.Locked() {
		password, ok := a.askUnlockPassword(ctx, false)
		if !ok {
			err = ErrSecretsSetupCancelled
		} else {
			err = v.AddSlot(ctx, vault.NewPasswordUnlocker(password, vault.DefaultSlotParams()))
			clear(password)
		}
	}
	a.pushSecretsState(view, v, err)
}

func (a *App) secretsVault(ctx context.Context, createIfMissing bool) (*vault.Vault, error) {
	if v := a.vaultIfOpen(); v != nil {
		return v, nil
	}
	if _, err := os.Stat(a.paths.VaultFile()); err == nil {
		return nil, ErrSecretsStoreUnavailable
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	if !createIfMissing {
		return nil, nil
	}
	return a.createSecretsVault(ctx)
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
		pwd, ok := a.askUnlockPassword(ctx, false)
		if !ok {
			return nil, ErrSecretsSetupCancelled
		}
		password = pwd
		unlocker = vault.NewPasswordUnlocker(password, vault.DefaultSlotParams())
	}
	v, err := createVaultFile(ctx, vault.Options{Path: a.paths.VaultFile(), IdleTime: vaultIdleTime}, unlocker)
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
	if a.trySilentUnlock(ctx, v) {
		return nil
	}
	if !allowPrompt {
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

func (a *App) trySilentUnlock(ctx context.Context, v *vault.Vault) bool {
	for _, u := range secretsSilentUnlockers() {
		if err := v.Unlock(ctx, u); err == nil {
			return true
		}
	}
	return false
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
	a.Post(func() {
		view.SetCredentials(creds)
		view.SetKeys(keys)
		view.SetSecretsLocked(locked)
		view.SetSecretsStatus(status)
	})
}

func secretsStatusText(v *vault.Vault, err error) string {
	switch {
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
