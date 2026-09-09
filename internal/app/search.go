package app

import (
	"context"
	"errors"
	"image/color"
	"os"
	"path/filepath"
	"sync"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/repo"
	"github.com/oops1/gogit/internal/repo/scan"
	"github.com/oops1/gogit/internal/ui/search"
)

var newSearchView = search.NewView

var showPickFolderDialog = func(eng widget.ModalShower, opts widget.FileDialogOptions, cb func(string, bool)) *widget.FileDialog {
	return widget.NewMessageBox(eng).ShowPickFolder(opts, cb)
}

var scanRepositories = func(ctx context.Context, root string, includeBare bool) ([]search.Found, error) {
	var out []search.Found
	for found, err := range scan.Scan(ctx, root, scan.Options{IncludeBare: includeBare}) {
		if err != nil {
			return out, err
		}
		out = append(out, search.Found{Path: found.Path, Bare: found.Bare, Worktree: found.Worktree})
	}
	return out, nil
}

var searchWG sync.WaitGroup

func (a *App) openSearch() {
	view, err := newSearchView(a.eng)
	if err != nil {
		a.log.Warn("open search dialog failed", "error", err)
		return
	}
	view.SetRoot(a.defaultSearchRoot())
	a.wireSearchView(view)
	a.showModal(view.Dialog(), view)
}

var userHomeDir = os.UserHomeDir

func (a *App) defaultSearchRoot() string {
	if node, ok := a.registry.Active(); ok {
		if dir := filepath.Dir(node.Path); dir != "." && dir != "" {
			return dir
		}
	}
	home, err := userHomeDir()
	if err != nil {
		return ""
	}
	return home
}

func (a *App) wireSearchView(view *search.View) {
	view.OnBrowse = func() {
		showPickFolderDialog(a.eng, widget.FileDialogOptions{StartDir: view.Root()}, func(path string, ok bool) {
			if !ok {
				return
			}
			view.SetRoot(path)
		})
	}
	view.OnScan = func(root string, includeBare bool) {
		a.startRepositorySearch(view, root, includeBare)
	}
	view.OnAdd = func(paths []string) {
		a.eng.CloseModal(view.Dialog())
		a.addFoundRepositories(paths)
	}
	view.OnCancel = func() {
		a.eng.CloseModal(view.Dialog())
	}
}

func (a *App) startRepositorySearch(view *search.View, root string, includeBare bool) {
	view.SetScanning(true)
	view.SetStatus(i18n.T("Dialog.Search.Scanning"), themeFor(a.EffectiveTheme()).LabelText)
	searchWG.Go(func() {
		found, err := scanRepositories(context.Background(), root, includeBare)
		a.Post(func() {
			view.SetScanning(false)
			view.SetResults(found)
			view.SetStatus(searchStatusText(found, err), a.searchStatusColor(err))
		})
	})
}

func searchStatusText(found []search.Found, err error) string {
	if err != nil {
		return err.Error()
	}
	if len(found) == 0 {
		return i18n.T("Dialog.Search.Empty")
	}
	return i18n.Tf("Dialog.Search.Found", len(found))
}

func (a *App) searchStatusColor(err error) color.RGBA {
	if err != nil {
		return secretsErrorTextColor
	}
	return themeFor(a.EffectiveTheme()).LabelText
}

var addRepositoryToRegistry = func(r *repo.Registry, name, path, group string) (*repo.Node, error) {
	return r.AddRepository(name, path, group)
}

func (a *App) addFoundRepositories(paths []string) {
	for _, path := range paths {
		name := filepath.Base(path)
		if _, err := addRepositoryToRegistry(a.registry, name, path, ""); err != nil {
			if errors.Is(err, repo.ErrDuplicatePath) {
				continue
			}
			a.log.Warn("add repository failed", "error", err, "path", path)
			continue
		}
	}
	if err := a.cfg.Save(a.paths.ConfigFile()); err != nil {
		a.log.Warn("save config failed", "error", err)
	}
	a.refreshBranchCache()
	a.reposView.Render(a.registry, a.repoTreeState())
}
