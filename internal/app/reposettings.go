package app

import (
	"errors"
	"os"
	"path/filepath"

	gitconfig "github.com/oops1/gogit/internal/gitcore/config"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/repo"
	"github.com/oops1/gogit/internal/ui/reposettings"
)

const (
	keyUserName      = "user.name"
	keyUserEmail     = "user.email"
	keyPushDefault   = "remote.pushDefault"
	keyPullRebase    = "pull.rebase"
	keyPullFF        = "pull.ff"
	keyAutoFetch     = "gogit.autofetch"
	pullFFOnlyValue  = "only"
	configFileName   = "config"
	autoFetchOnValue = "true"
)

var newRepoSettingsView = reposettings.NewView

func (a *App) openActiveRepoSettings() {
	node, ok := a.registry.Active()
	if !ok {
		return
	}
	a.openRepoSettings(node.ID)
}

func (a *App) openRepoSettings(id string) {
	node, ok := a.registry.Find(id)
	if !ok || node.Kind == repo.KindGroup {
		return
	}
	r, err := openGitRepository(node.Path, gitrepo.OpenOptions{})
	if err != nil {
		a.log.Warn("open repository settings failed", "path", node.Path, "error", err)
		a.showError(i18n.T("Dialog.RepoSettings.Title"), i18n.Tf("Dialog.RepoSettings.Failed", err))
		return
	}
	defer func() { _ = closeGitRepository(r) }()
	view, err := newRepoSettingsView()
	if err != nil {
		a.log.Warn("open repository settings dialog failed", "error", err)
		return
	}
	cfg := r.Config()
	view.SetRemotes(remoteNames(cfg))
	view.SetInherited(a.inheritedSettings(cfg))
	view.Apply(a.localSettings(node, cfg))
	a.wireRepoSettingsView(view, node.ID, r.GitDir())
	view.FitHeight(a.root.Bounds().Dy())
	a.eng.ShowModal(view.Dialog())
	view.Restyle(themeFor(a.EffectiveTheme()))
}

func remoteNames(cfg *gitconfig.Config) []string {
	remotes := cfg.Remotes()
	names := make([]string, 0, len(remotes))
	for _, remote := range remotes {
		names = append(names, remote.Name)
	}
	return names
}

func (a *App) inheritedSettings(cfg *gitconfig.Config) reposettings.Inherited {
	return reposettings.Inherited{
		UserName:      inheritedValue(cfg, keyUserName),
		UserEmail:     inheritedValue(cfg, keyUserEmail),
		DefaultRemote: a.cfg.Git.DefaultRemote,
		AutoFetch:     a.cfg.Git.AutoFetch,
	}
}

func inheritedValue(cfg *gitconfig.Config, key string) string {
	for _, level := range []gitconfig.Level{gitconfig.LevelGlobal, gitconfig.LevelSystem} {
		file, ok := cfg.File(level)
		if !ok {
			continue
		}
		if value, ok := file.Get(key); ok {
			return value
		}
	}
	return ""
}

func (a *App) localSettings(node *repo.Node, cfg *gitconfig.Config) reposettings.Settings {
	settings := reposettings.Settings{Name: node.Name, Path: node.Path}
	local, ok := cfg.File(gitconfig.LevelLocal)
	if !ok {
		return settings
	}
	settings.UserName, _ = local.Get(keyUserName)
	settings.UserEmail, _ = local.Get(keyUserEmail)
	settings.DefaultRemote, _ = local.Get(keyPushDefault)
	settings.PullStrategy = pullStrategyOf(local)
	settings.AutoFetch = autoFetchOf(local)
	return settings
}

func pullStrategyOf(local *gitconfig.File) string {
	if rebase, err := local.GetBool(keyPullRebase); err == nil {
		if rebase {
			return reposettings.PullRebase
		}
		return reposettings.PullMerge
	}
	if value, ok := local.Get(keyPullFF); ok && value == pullFFOnlyValue {
		return reposettings.PullFF
	}
	return reposettings.PullInherit
}

func autoFetchOf(local *gitconfig.File) string {
	on, err := local.GetBool(keyAutoFetch)
	if err != nil {
		return reposettings.AutoFetchInherit
	}
	if on {
		return reposettings.AutoFetchOn
	}
	return reposettings.AutoFetchOff
}

func (a *App) effectiveDefaultRemote(r *gitrepo.Repository) string {
	if local, ok := r.Config().File(gitconfig.LevelLocal); ok {
		if name, ok := local.Get(keyPushDefault); ok && name != "" {
			return name
		}
	}
	return a.cfg.Git.DefaultRemote
}

func (a *App) autoFetchEnabled(o *openedRepository) bool {
	r, err := a.freshRepo(o)
	if err != nil {
		return a.cfg.Git.AutoFetch
	}
	defer func() { _ = closeGitRepository(r) }()
	switch localAutoFetch(r.Config()) {
	case reposettings.AutoFetchOn:
		return true
	case reposettings.AutoFetchOff:
		return false
	}
	return a.cfg.Git.AutoFetch
}

func localAutoFetch(cfg *gitconfig.Config) string {
	local, ok := cfg.File(gitconfig.LevelLocal)
	if !ok {
		return reposettings.AutoFetchInherit
	}
	return autoFetchOf(local)
}

func (a *App) wireRepoSettingsView(view *reposettings.View, id, gitDir string) {
	view.OnOK = func(settings reposettings.Settings) {
		a.eng.CloseModal(view.Dialog())
		a.saveRepoSettings(id, gitDir, settings)
	}
	view.OnCancel = func() { a.eng.CloseModal(view.Dialog()) }
}

func (a *App) saveRepoSettings(id, gitDir string, settings reposettings.Settings) {
	if err := writeRepoSettings(filepath.Join(gitDir, configFileName), settings); err != nil {
		a.log.Warn("save repository settings failed", "error", err)
		a.showError(i18n.T("Dialog.RepoSettings.Title"), i18n.Tf("Dialog.RepoSettings.Failed", err))
		return
	}
	if err := a.registry.RenameRepository(id, settings.Name); err != nil {
		a.log.Warn("rename repository failed", "id", id, "error", err)
		return
	}
	a.saveRepositoryTree()
	a.updateStatusText()
	a.restartAutoFetch()
}

var readConfigFile = os.ReadFile

func writeRepoSettings(path string, settings reposettings.Settings) error {
	data, err := readConfigFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	file, err := gitconfig.Parse(data)
	if err != nil {
		return err
	}
	for _, pair := range repoSettingPairs(settings) {
		if err := applySetting(file, pair.key, pair.value); err != nil {
			return err
		}
	}
	return file.Save(path)
}

type configPair struct {
	key   string
	value string
}

func repoSettingPairs(settings reposettings.Settings) []configPair {
	rebase, ff := pullValues(settings.PullStrategy)
	return []configPair{
		{key: keyUserName, value: settings.UserName},
		{key: keyUserEmail, value: settings.UserEmail},
		{key: keyPushDefault, value: settings.DefaultRemote},
		{key: keyPullRebase, value: rebase},
		{key: keyPullFF, value: ff},
		{key: keyAutoFetch, value: autoFetchValue(settings.AutoFetch)},
	}
}

func pullValues(strategy string) (rebase, ff string) {
	switch strategy {
	case reposettings.PullRebase:
		return "true", ""
	case reposettings.PullMerge:
		return "false", "true"
	case reposettings.PullFF:
		return "", pullFFOnlyValue
	}
	return "", ""
}

func autoFetchValue(choice string) string {
	switch choice {
	case reposettings.AutoFetchOn:
		return autoFetchOnValue
	case reposettings.AutoFetchOff:
		return "false"
	}
	return ""
}

var applySetting = setOrUnset

func setOrUnset(file *gitconfig.File, key, value string) error {
	if value != "" {
		return file.Set(key, value)
	}
	if err := file.UnsetAll(key); err != nil && !errors.Is(err, gitconfig.ErrNotFound) {
		return err
	}
	return nil
}
