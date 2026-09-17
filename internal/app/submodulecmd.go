package app

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/gitcore/ops"
	"github.com/oops1/gogit/internal/gitcore/progress"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/gitcore/transport"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/repo"
)

var (
	listSubmodules     = ops.ListSubmodules
	runSubmoduleUpdate = ops.SubmoduleUpdate
	runSubmoduleSync   = ops.SubmoduleSync

	addRegistryRepository = (*repo.Registry).AddRepository
)

const literalPathspec = ":(literal)"

var submoduleEventKeys = map[ops.SubmoduleEventKind]string{
	ops.SubmoduleRegistered:      "Operation.Log.SubmoduleRegistered",
	ops.SubmoduleCloning:         "Operation.Log.SubmoduleCloning",
	ops.SubmoduleFetching:        "Operation.Log.SubmoduleFetching",
	ops.SubmoduleFetchRetry:      "Operation.Log.SubmoduleFetchRetry",
	ops.SubmoduleCheckedOut:      "Operation.Log.SubmoduleCheckedOut",
	ops.SubmoduleRebased:         "Operation.Log.SubmoduleRebased",
	ops.SubmoduleMerged:          "Operation.Log.SubmoduleMerged",
	ops.SubmoduleSkipped:         "Operation.Log.SubmoduleSkipped",
	ops.SubmoduleSkippedUnmerged: "Operation.Log.SubmoduleSkippedUnmerged",
	ops.SubmoduleNotInitialized:  "Operation.Log.SubmoduleNotInitialized",
	ops.SubmoduleSynchronized:    "Operation.Log.SubmoduleSynchronized",
}

func (a *App) registerSubmoduleHandlers() {
	a.handlers[CmdSubmoduleUpdate] = func() { a.startSubmoduleUpdate(nil, false) }
	a.handlers[CmdSubmoduleInitialize] = func() { a.startSubmoduleUpdate(nil, true) }
	a.handlers[CmdSubmoduleSync] = func() { a.startSubmoduleSync(nil) }
	a.branchesView.OnSubmoduleMenu = a.submoduleMenu
	a.branchesView.OnSubmoduleActivate = a.openSubmodule
}

func (a *App) setHasSubmodules(v bool) {
	a.mu.Lock()
	changed := a.state.HasSubmodules != v
	a.state.HasSubmodules = v
	a.mu.Unlock()
	if changed {
		a.refreshCommands()
	}
}

func (a *App) clearSubmodules() {
	a.submoduleGen.Add(1)
	a.showSubmodules(nil)
}

func (a *App) showSubmodules(list []ops.Submodule) {
	a.branchesView.SetSubmodules(list)
	a.setHasSubmodules(len(list) > 0)
	a.showSidebarSubmoduleCount(len(list))
}

type submoduleLoader struct {
	open func(string, gitrepo.OpenOptions) (*gitrepo.Repository, error)
	list func(context.Context, *gitrepo.Repository) ([]ops.Submodule, error)
	log  *slog.Logger
}

func (a *App) refreshSubmodules(o *openedRepository) {
	gen := a.submoduleGen.Add(1)
	loader := submoduleLoader{open: openGitRepository, list: listSubmodules, log: a.log}
	a.submoduleWG.Go(func() {
		list := loader.load(o.path)
		a.Post(func() {
			if a.submoduleGen.Load() == gen && a.opened() == o {
				a.showSubmodules(list)
			}
		})
	})
}

func (l submoduleLoader) load(path string) []ops.Submodule {
	r, err := l.open(path, gitrepo.OpenOptions{})
	if err != nil {
		l.log.Warn("open repository for submodules failed", "path", path, "error", err)
		return nil
	}
	defer func() { _ = r.Close() }()
	list, err := l.list(context.Background(), r)
	if err != nil {
		l.log.Warn("list submodules failed", "path", path, "error", err)
	}
	return list
}

func literalPaths(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		out = append(out, literalPathspec+path)
	}
	return out
}

func submoduleEventText(e ops.SubmoduleEvent) string {
	key := submoduleEventKeys[e.Kind]
	switch e.Kind {
	case ops.SubmoduleRegistered:
		return i18n.Tf(key, e.Name, e.URL, e.Path)
	case ops.SubmoduleCloning:
		return i18n.Tf(key, e.URL, e.Path)
	case ops.SubmoduleFetchRetry, ops.SubmoduleCheckedOut, ops.SubmoduleRebased, ops.SubmoduleMerged:
		return i18n.Tf(key, e.Path, e.Commit.String())
	}
	return i18n.Tf(key, e.Path)
}

func submoduleEventLog(reporter OperationReporter, count *int) ops.SubmoduleEvents {
	return func(e ops.SubmoduleEvent) {
		*count++
		reporter.Log(submoduleEventText(e))
	}
}

func reportSubmoduleError(reporter OperationReporter, err error) {
	if errors.Is(err, transport.ErrProtocolNotAllowed) {
		reporter.Log(i18n.T("Operation.Log.SubmoduleProtocolHint"))
	}
}

func (a *App) startSubmoduleUpdate(paths []string, initialize bool) {
	title := i18n.T("Operation.Title.SubmoduleUpdate")
	if initialize {
		title = i18n.T("Operation.Title.SubmoduleInitialize")
	}
	a.runRemoteJob(title, true, func(ctx context.Context, o *openedRepository, prog progress.Func, reporter OperationReporter) error {
		r, err := a.freshRepo(o)
		if err != nil {
			return err
		}
		defer func() { _ = r.Close() }()
		logged := 0
		err = runSubmoduleUpdate(ctx, r, literalPaths(paths), ops.SubmoduleUpdateOptions{
			Init:      initialize,
			Recursive: true,
			Progress:  prog,
			Transport: a.transportOptions(prog),
			Events:    submoduleEventLog(reporter, &logged),
		})
		reportSubmoduleError(reporter, err)
		if err == nil && logged == 0 {
			reporter.Log(i18n.T("Operation.Log.SubmodulesUpToDate"))
		}
		return err
	})
}

func (a *App) startSubmoduleSync(paths []string) {
	o := a.opened()
	if o == nil {
		return
	}
	a.RunOperation(i18n.T("Operation.Title.SubmoduleSync"), func(ctx context.Context, reporter OperationReporter) error {
		defer a.finishRemoteOperation(false)
		r, err := a.freshRepo(o)
		if err != nil {
			return err
		}
		defer func() { _ = r.Close() }()
		return runSubmoduleSync(ctx, r, literalPaths(paths), ops.SubmoduleSyncOptions{
			Recursive: true,
			Events:    submoduleEventLog(reporter, new(int)),
		})
	})
}

func (a *App) submoduleMenu(sub ops.Submodule) []widget.MenuItem {
	state := a.State()
	paths := []string{sub.Path}
	items := []widget.MenuItem{
		enabledItem("Menu.Context.Open", func() { a.openSubmodule(sub) }, sub.Populated),
		menuSeparator(),
		enabledItem("Menu.Remote.Submodule.Update", func() { a.startSubmoduleUpdate(paths, false) }, sub.Active && state.Enabled(CmdSubmoduleUpdate)),
		enabledItem("Menu.Remote.Submodule.Initialize", func() { a.startSubmoduleUpdate(paths, true) }, !sub.Populated && state.Enabled(CmdSubmoduleInitialize)),
		enabledItem("Menu.Remote.Submodule.Synchronize", func() { a.startSubmoduleSync(paths) }, sub.Active && state.Enabled(CmdSubmoduleSync)),
		menuSeparator(),
		laterItem("Menu.Remote.Submodule.Remove"),
		laterItem("Menu.Remote.Submodule.Unregister"),
		laterItem("Menu.Remote.Submodule.Reset"),
	}
	o := a.opened()
	if o == nil {
		return items
	}
	return append(append(items, menuSeparator()), a.pathMenu(submoduleDir(o, sub))...)
}

func submoduleDir(o *openedRepository, sub ops.Submodule) string {
	return filepath.Join(o.repo.WorkTree(), filepath.FromSlash(sub.Path))
}

func (a *App) openSubmodule(sub ops.Submodule) {
	o := a.opened()
	if o == nil || !sub.Populated {
		return
	}
	dir := submoduleDir(o, sub)
	if node, ok := a.registry.FindByPath(dir); ok {
		a.ActivateRepository(node.ID)
		return
	}
	group := ""
	if parent, ok := a.registry.ParentOf(o.id); ok && parent.Kind == repo.KindGroup {
		group = parent.ID
	}
	node, err := addRegistryRepository(a.registry, filepath.Base(dir), dir, group)
	if err != nil {
		a.log.Warn("add submodule repository failed", "path", dir, "error", err)
		a.statusLabel.SetText(i18n.Tf("Status.SubmoduleOpenFailed", err))
		return
	}
	if err := a.cfg.Save(a.paths.ConfigFile()); err != nil {
		a.log.Warn("save config failed", "error", err)
	}
	a.refreshBranchCache()
	a.reposView.Render(a.registry, a.repoTreeState())
	a.ActivateRepository(node.ID)
}
