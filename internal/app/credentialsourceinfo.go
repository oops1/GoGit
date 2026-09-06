package app

import (
	"strings"

	gitconfig "github.com/oops1/gogit/internal/gitcore/config"
	"github.com/oops1/gogit/internal/gitcore/credential"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/settings"
	"github.com/oops1/gogit/internal/vault"
)

const gitHelperProbeRawURL = "https://git.invalid"

func (a *App) refreshCredentialSourceInfo(view *settings.View) {
	view.SetCredentialSourceInfo(a.paths.VaultFile(), a.vaultKeyProtectionLabel(), a.openRepositoryHelperEntries())
}

func (a *App) vaultKeyProtectionLabel() string {
	v := a.vaultIfOpen()
	if v == nil {
		return i18n.T("Dialog.Settings.Secrets.Slot.None")
	}
	slots := v.Slots()
	seen := make(map[vault.SlotKind]bool, len(slots))
	labels := make([]string, 0, len(slots))
	for _, s := range slots {
		if seen[s.Kind] {
			continue
		}
		seen[s.Kind] = true
		labels = append(labels, i18n.T(slotKindKey(s.Kind)))
	}
	return strings.Join(labels, ", ")
}

func slotKindKey(kind vault.SlotKind) string {
	switch kind {
	case vault.SlotDPAPI:
		return "Dialog.Settings.Secrets.Slot.DPAPI"
	case vault.SlotSecretService:
		return "Dialog.Settings.Secrets.Slot.SecretService"
	case vault.SlotFile:
		return "Dialog.Settings.Secrets.Slot.File"
	default:
		return "Dialog.Settings.Secrets.Slot.Password"
	}
}

func (a *App) openRepositoryHelperEntries() []settings.CredentialHelperEntry {
	o := a.opened()
	if o == nil {
		return nil
	}
	r, err := a.freshRepo(o)
	if err != nil {
		return nil
	}
	defer func() { _ = r.Close() }()
	cfg := r.Config()
	rawURL := defaultRemoteRawURL(cfg, a.cfg.Git.DefaultRemote)
	_, infos, err := credential.FromConfig(cfg, rawURL)
	if err != nil {
		a.log.Warn("resolve credential helpers for the settings page failed", "error", err)
		return nil
	}
	out := make([]settings.CredentialHelperEntry, len(infos))
	for i, info := range infos {
		out[i] = settings.CredentialHelperEntry{Name: info.Name, Supported: info.Supported}
	}
	return out
}

func defaultRemoteRawURL(cfg *gitconfig.Config, defaultRemote string) string {
	if rem, ok := cfg.Remote(defaultRemote); ok && len(rem.URLs) > 0 {
		return rem.URLs[0]
	}
	for _, rem := range cfg.Remotes() {
		if len(rem.URLs) > 0 {
			return rem.URLs[0]
		}
	}
	return gitHelperProbeRawURL
}
