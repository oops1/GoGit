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
	"github.com/oops1/gogit/internal/ui/submoduleadd"
)

var (
	listSubmodules     = ops.ListSubmodules
	runSubmoduleUpdate = ops.SubmoduleUpdate
	runSubmoduleSync   = ops.SubmoduleSync
	runSubmoduleAdd    = ops.SubmoduleAdd
	runSubmoduleRemove = ops.SubmoduleRemove
	runSubmoduleDeinit = ops.SubmoduleDeinit

	newSubmoduleAddView = submoduleadd.NewView

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
	ops.SubmoduleCommandRefused:  "Operation.Log.SubmoduleCommandRefused",
	ops.SubmoduleAbsorbed:        "Operation.Log.SubmoduleAbsorbed",
	ops.SubmoduleCleared:         "Operation.Log.SubmoduleCleared",
	ops.SubmoduleUnregistered:    "Operation.Log.SubmoduleUnregistered",
	ops.SubmoduleRemoved:         "Operation.Log.SubmoduleRemoved",
}

func (a *App) registerSubmoduleHandlers() {
	a.handlers[CmdSubmoduleUpdate] = func() { a.startSubmoduleUpdate(nil, false) }
	a.handlers[CmdSubmoduleInitialize] = func() { a.startSubmoduleUpdate(nil, true) }
	a.handlers[CmdSubmoduleSync] = func() { a.startSubmoduleSync(nil) }
	a.handlers[CmdSubmoduleAdd] = a.openSubmoduleAdd
	a.handlers[CmdSubmoduleRemove] = func() { a.withSelectedSubmodule(a.confirmSubmoduleRemove) }
	a.handlers[CmdSubmoduleUnregister] = func() { a.withSelectedSubmodule(a.confirmSubmoduleUnregister) }
	a.handlers[CmdSubmoduleReset] = func() { a.withSelectedSubmodule(a.confirmSubmoduleReset) }
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
	case ops.SubmoduleRegistered, ops.SubmoduleUnregistered:
		return i18n.Tf(key, e.Name, e.URL, e.Path)
	case ops.SubmoduleCommandRefused:
		return i18n.Tf(key, e.Path, e.Command)
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
		enabledItem("Menu.Remote.Submodule.Remove", func() { a.confirmSubmoduleRemove(sub) }, state.Enabled(CmdSubmoduleRemove)),
		enabledItem("Menu.Remote.Submodule.Unregister", func() { a.confirmSubmoduleUnregister(sub) }, (sub.Active || sub.Populated) && state.Enabled(CmdSubmoduleUnregister)),
		enabledItem("Menu.Remote.Submodule.Reset", func() { a.confirmSubmoduleReset(sub) }, sub.Populated && state.Enabled(CmdSubmoduleReset)),
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

func (a *App) openSubmoduleAdd() {
	if a.opened() == nil {
		return
	}
	view, err := newSubmoduleAddView()
	if err != nil {
		a.log.Warn("open submodule dialog failed", "error", err)
		return
	}
	var paths []string
	for _, sub := range a.branchesView.Submodules() {
		paths = append(paths, sub.Path)
	}
	view.SetKnown(submoduleadd.Known{Paths: paths})
	view.OnOK = func(model submoduleadd.Model) {
		a.eng.CloseModal(view.Modal())
		a.startSubmoduleAdd(model)
	}
	view.OnCancel = func() { a.eng.CloseModal(view.Modal()) }
	a.showModal(view.Modal(), view)
}

func (a *App) startSubmoduleAdd(model submoduleadd.Model) {
	a.runRemoteJob(i18n.T("Operation.Title.SubmoduleAdd"), true, func(ctx context.Context, o *openedRepository, prog progress.Func, reporter OperationReporter) error {
		r, err := a.freshRepo(o)
		if err != nil {
			return err
		}
		defer func() { _ = r.Close() }()
		result, err := runSubmoduleAdd(ctx, r, model.URL, ops.SubmoduleAddOptions{
			Path:      model.Path,
			Branch:    model.Branch,
			Progress:  prog,
			Transport: a.transportOptions(prog),
			Events:    submoduleEventLog(reporter, new(int)),
		})
		reportSubmoduleError(reporter, err)
		if err == nil {
			reporter.Log(i18n.Tf("Operation.Log.SubmoduleAdded", result.Name, result.Path))
		}
		return err
	})
}

func (a *App) withSelectedSubmodule(action func(ops.Submodule)) {
	sub, ok := a.branchesView.SelectedSubmodule()
	if !ok {
		a.statusLabel.SetText(i18n.T("Status.SubmoduleNotSelected"))
		return
	}
	action(sub)
}

func (a *App) confirmSubmoduleRemove(sub ops.Submodule) {
	a.askConfirm(i18n.T("Dialog.SubmoduleRemove.Title"), i18n.Tf("Dialog.SubmoduleRemove.Message", sub.Path), func(ok bool) {
		if ok {
			a.startSubmoduleRemove(sub, false)
		}
	})
}

func (a *App) confirmSubmoduleUnregister(sub ops.Submodule) {
	a.askConfirm(i18n.T("Dialog.SubmoduleUnregister.Title"), i18n.Tf("Dialog.SubmoduleUnregister.Message", sub.Path), func(ok bool) {
		if ok {
			a.startSubmoduleUnregister(sub, false)
		}
	})
}

func (a *App) confirmSubmoduleReset(sub ops.Submodule) {
	a.askConfirm(i18n.T("Dialog.SubmoduleReset.Title"), i18n.Tf("Dialog.SubmoduleReset.Message", sub.Path), func(ok bool) {
		if ok {
			a.startSubmoduleReset(sub)
		}
	})
}

type submoduleRemoval func(ctx context.Context, r *gitrepo.Repository, force bool, events ops.SubmoduleEvents) error

func (a *App) startSubmoduleRemove(sub ops.Submodule, force bool) {
	a.runSubmoduleRemoval("Operation.Title.SubmoduleRemove", sub, force, a.startSubmoduleRemove, func(ctx context.Context, r *gitrepo.Repository, force bool, events ops.SubmoduleEvents) error {
		return runSubmoduleRemove(ctx, r, []string{sub.Path}, ops.SubmoduleRemoveOptions{Force: force, Events: events})
	})
}

func (a *App) startSubmoduleUnregister(sub ops.Submodule, force bool) {
	a.runSubmoduleRemoval("Operation.Title.SubmoduleUnregister", sub, force, a.startSubmoduleUnregister, func(ctx context.Context, r *gitrepo.Repository, force bool, events ops.SubmoduleEvents) error {
		return runSubmoduleDeinit(ctx, r, literalPaths([]string{sub.Path}), ops.SubmoduleDeinitOptions{Force: force, Events: events})
	})
}

func (a *App) runSubmoduleRemoval(titleKey string, sub ops.Submodule, force bool, retry func(ops.Submodule, bool), run submoduleRemoval) {
	o := a.opened()
	if o == nil {
		return
	}
	a.RunOperation(i18n.T(titleKey), func(ctx context.Context, reporter OperationReporter) error {
		r, err := a.freshRepo(o)
		if err != nil {
			return err
		}
		defer func() { _ = r.Close() }()
		err = run(ctx, r, force, submoduleEventLog(reporter, new(int)))
		if !errors.Is(err, ops.ErrSubmoduleLocalChanges) && !errors.Is(err, ops.ErrSubmoduleStagedChanges) {
			a.finishRemoteOperation(true)
			return err
		}
		reporter.Log(i18n.Tf("Operation.Log.SubmoduleLocalChanges", sub.Path))
		reporter.Then(func() {
			a.askConfirm(i18n.T("Dialog.SubmoduleForce.Title"), i18n.Tf("Dialog.SubmoduleForce.Message", sub.Path), func(ok bool) {
				if ok {
					retry(sub, true)
				}
			})
		})
		return err
	})
}

func (a *App) startSubmoduleReset(sub ops.Submodule) {
	a.runRemoteJob(i18n.T("Operation.Title.SubmoduleReset"), true, func(ctx context.Context, o *openedRepository, prog progress.Func, reporter OperationReporter) error {
		r, err := a.freshRepo(o)
		if err != nil {
			return err
		}
		defer func() { _ = r.Close() }()
		err = runSubmoduleUpdate(ctx, r, literalPaths([]string{sub.Path}), ops.SubmoduleUpdateOptions{
			Mode:      ops.SubmoduleUpdateCheckout,
			Force:     true,
			Progress:  prog,
			Transport: a.transportOptions(prog),
			Events:    submoduleEventLog(reporter, new(int)),
		})
		reportSubmoduleError(reporter, err)
		return err
	})
}
