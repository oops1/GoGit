package app

import (
	"strings"

	"github.com/oops1/gogit/internal/config"

	gitconfig "github.com/oops1/gogit/internal/gitcore/config"
	"github.com/oops1/gogit/internal/gitcore/credential"
	"github.com/oops1/gogit/internal/gitcore/transport"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/settings"
	"github.com/oops1/gogit/internal/vault"
)

const gitHelperProbeRawURL = "https://git.invalid"

func (a *App) refreshCredentialSourceInfo(view *settings.View) {
	a.refreshCredentialSourceInfoFor(view, a.cfg.Git.CredentialSource)
}

func (a *App) refreshCredentialSourceInfoFor(view *settings.View, source string) {
	var (
		storePath  string
		protection string
		helpers    []settings.CredentialHelperEntry
	)
	if source != config.CredentialSourceHelper {
		storePath = a.paths.VaultFile()
		protection = a.vaultKeyProtectionLabel()
	}
	if source != config.CredentialSourceVault {
		helpers = a.openRepositoryHelperEntries()
		if helpers == nil {
			helpers = []settings.CredentialHelperEntry{}
		}
	}
	view.SetCredentialSourceInfo(storePath, protection, helpers)
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
	rawURL := defaultRemoteRawURL(cfg, a.effectiveDefaultRemote(r))
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
	if raw, ok := configuredRemoteRawURL(cfg, defaultRemote); ok {
		return raw
	}
	return gitHelperProbeRawURL
}

func configuredRemoteRawURL(cfg *gitconfig.Config, defaultRemote string) (string, bool) {
	if rem, ok := cfg.Remote(defaultRemote); ok && len(rem.URLs) > 0 {
		return rem.URLs[0], true
	}
	for _, rem := range cfg.Remotes() {
		if len(rem.URLs) > 0 {
			return rem.URLs[0], true
		}
	}
	return "", false
}

func (a *App) remoteRawURLForCredentials(o *openedRepository) string {
	r, err := a.freshRepo(o)
	if err != nil {
		return ""
	}
	defer func() { _ = r.Close() }()
	raw, _ := configuredRemoteRawURL(r.Config(), a.effectiveDefaultRemote(r))
	return raw
}

func (a *App) reportIgnoredCredentialHelpers(reporter OperationReporter, rawURL string) {
	if a.cfg.Git.CredentialSource == config.CredentialSourceVault {
		return
	}
	endpoint, password, err := transport.ParseURL(rawURL)
	password.Wipe()
	if err != nil || (endpoint.Scheme != transport.SchemeHTTP && endpoint.Scheme != transport.SchemeHTTPS) {
		return
	}
	_, infos, err := credential.FromConfig(a.repositoryConfigForCredentials(), rawURL)
	if err != nil {
		return
	}
	var ignored []string
	for _, info := range infos {
		if !info.Supported {
			ignored = append(ignored, ignoredHelperLabel(info.Name))
		}
	}
	if len(ignored) > 0 {
		reporter.Log(i18n.Tf("Operation.Log.CredentialHelperIgnored", strings.Join(ignored, ", ")))
	}
}

func ignoredHelperLabel(name string) string {
	if command, ok := strings.CutPrefix(name, "!"); ok {
		program, _, _ := strings.Cut(strings.TrimSpace(command), " ")
		return "!" + program
	}
	return name
}
