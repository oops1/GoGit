package app

import (
	"context"
	"errors"
	"path/filepath"
	"sort"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/ops"
	"github.com/oops1/gogit/internal/gitcore/progress"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/refspec"
	"github.com/oops1/gogit/internal/gitcore/remote"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/gitcore/transport"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/clone"
	"github.com/oops1/gogit/internal/ui/remotes"
)

const remoteUserAgent = "Go.Git"

const headsRefPrefix = "refs/heads/"

var (
	newCloneView    = clone.NewView
	newRemotesView  = remotes.NewView
	lsRemoteFunc    = remote.LsRemote
	cloneRepository = ops.Clone
)

func (a *App) transportOptions(prog progress.Func) transport.Options {
	return transport.Options{
		Credentials: a.credentialSource(),
		UserAgent:   remoteUserAgent,
		Progress:    prog,
		HostKeys:    a.hostKeyPolicy(),
		Keys:        transport.NewAgentKeys(),
	}
}

func (a *App) freshRepo(o *openedRepository) (*gitrepo.Repository, error) {
	return openGitRepository(o.path, gitrepo.OpenOptions{})
}

func (a *App) hasRemotes() bool {
	o := a.opened()
	if o == nil {
		return false
	}
	r, err := a.freshRepo(o)
	if err != nil {
		return false
	}
	defer func() { _ = r.Close() }()
	return len(remote.List(r.Config())) > 0
}

func (a *App) refreshRemoteState() {
	a.setHasRemotes(a.hasRemotes())
}

type remoteJob func(ctx context.Context, o *openedRepository, prog progress.Func, reporter OperationReporter) error

func (a *App) runRemoteJob(title string, reloadTree bool, job remoteJob) {
	o := a.opened()
	if o == nil {
		return
	}
	a.RunOperation(title, func(ctx context.Context, reporter OperationReporter) error {
		prog := newOperationProgress(reporter)
		err := job(ctx, o, prog, reporter)
		a.finishRemoteOperation(reloadTree)
		return err
	})
}

func (a *App) finishRemoteOperation(reloadTree bool) {
	a.Post(func() {
		if reloadTree {
			a.reloadWorktree()
		}
		a.refreshRemoteState()
		a.RefreshRepository()
	})
}

func (a *App) startFetch() {
	a.runRemoteJob(i18n.T("Operation.Title.Fetch"), false, func(ctx context.Context, o *openedRepository, prog progress.Func, reporter OperationReporter) error {
		return a.runFetchBody(ctx, o, prog, reporter, a.cfg.Git.PruneOnFetch)
	})
}

func (a *App) startPrune() {
	a.runRemoteJob(i18n.T("Operation.Title.Prune"), false, func(ctx context.Context, o *openedRepository, prog progress.Func, reporter OperationReporter) error {
		return a.runFetchBody(ctx, o, prog, reporter, true)
	})
}

func (a *App) runFetchBody(ctx context.Context, o *openedRepository, prog progress.Func, reporter OperationReporter, prune bool) error {
	r, err := a.freshRepo(o)
	if err != nil {
		return err
	}
	defer func() { _ = r.Close() }()
	result, err := ops.Fetch(ctx, r, a.effectiveDefaultRemote(r), remote.FetchOptions{
		Prune:     prune,
		Progress:  prog,
		Transport: a.transportOptions(prog),
	})
	if err != nil {
		return err
	}
	if result.Objects > 0 {
		reporter.Log(i18n.Tf("Operation.Log.Fetched", result.Objects))
	} else {
		reporter.Log(i18n.T("Operation.Log.UpToDate"))
	}
	if prune {
		reporter.Log(i18n.Tf("Operation.Log.Pruned", prunedCount(result.Changes)))
	}
	return nil
}

func prunedCount(changes []remote.Change) int {
	n := 0
	for _, c := range changes {
		if c.Deleted {
			n++
		}
	}
	return n
}

func (a *App) startPull() {
	a.runRemoteJob(i18n.T("Operation.Title.Pull"), true, a.runPullBody)
}

func (a *App) startPush() {
	a.runRemoteJob(i18n.T("Operation.Title.Push"), false, a.runPushBody)
}

func (a *App) startSync() {
	a.runRemoteJob(i18n.T("Operation.Title.Sync"), true, func(ctx context.Context, o *openedRepository, prog progress.Func, reporter OperationReporter) error {
		if err := a.runPullBody(ctx, o, prog, reporter); err != nil {
			return err
		}
		return a.runPushBody(ctx, o, prog, reporter)
	})
}

func (a *App) runPullBody(ctx context.Context, o *openedRepository, prog progress.Func, reporter OperationReporter) error {
	r, err := a.freshRepo(o)
	if err != nil {
		return err
	}
	defer func() { _ = r.Close() }()
	result, err := ops.Pull(ctx, r, ops.PullOptions{
		Progress: prog,
		Fetch:    remote.FetchOptions{Progress: prog, Transport: a.transportOptions(prog)},
	})
	if errors.Is(err, ops.ErrNotFastForward) {
		reporter.Log(i18n.T("Operation.Log.NonFastForward"))
		return err
	}
	if err != nil {
		return err
	}
	if result.UpToDate {
		reporter.Log(i18n.T("Operation.Log.UpToDate"))
	}
	return nil
}

func defaultPushRefspec(o *openedRepository) (refspec.RefSpec, error) {
	snap, err := loadBranchSnapshot(o.store)
	if err != nil {
		return refspec.RefSpec{}, err
	}
	if snap.Detached || snap.Current == "" {
		return refspec.RefSpec{}, ops.ErrDetachedHead
	}
	branchRef := refs.BranchName(snap.Current).String()
	return refspec.RefSpec{Src: branchRef, Dst: branchRef}, nil
}

func (a *App) runPushBody(ctx context.Context, o *openedRepository, prog progress.Func, reporter OperationReporter) error {
	spec, err := defaultPushRefspec(o)
	if err != nil {
		return err
	}
	r, err := a.freshRepo(o)
	if err != nil {
		return err
	}
	defer func() { _ = r.Close() }()
	result, err := ops.Push(ctx, r, a.effectiveDefaultRemote(r), remote.PushOptions{
		Refspecs:  []refspec.RefSpec{spec},
		Progress:  prog,
		Transport: a.transportOptions(prog),
	})
	if err != nil {
		if errors.Is(err, remote.ErrNonFastForward) || errors.Is(err, remote.ErrRejected) {
			reporter.Log(i18n.T("Operation.Log.Rejected"))
		}
		return err
	}
	if len(result.Changes) == 0 {
		reporter.Log(i18n.T("Operation.Log.UpToDate"))
		return nil
	}
	reporter.Log(i18n.Tf("Operation.Log.Pushed", pushSummary(result.Changes)))
	return nil
}

func pushSummary(changes []remote.Change) string {
	names := make([]string, 0, len(changes))
	for _, c := range changes {
		names = append(names, c.Name.Short())
	}
	return strings.Join(names, ", ")
}

func (a *App) openClone() {
	view, err := newCloneView(a.eng, clone.Request{})
	if err != nil {
		a.log.Warn("open clone dialog failed", "error", err)
		return
	}
	view.OnCheck = func(url string) {
		view.SetStatus(i18n.T("Dialog.Clone.Status.Checking"))
		go a.checkCloneURL(view, url)
	}
	view.OnOK = func(result clone.Result) {
		a.eng.CloseModal(view.Dialog())
		a.startClone(result)
	}
	view.OnCancel = func() {
		a.eng.CloseModal(view.Dialog())
	}
	a.eng.ShowModal(view.Dialog())
}

func (a *App) checkCloneURL(view *clone.View, url string) {
	refList, err := lsRemoteFunc(context.Background(), url, a.transportOptions(nil))
	a.Post(func() {
		view.SetBusy(false)
		if err != nil {
			view.SetStatus(i18n.Tf("Dialog.Clone.Status.Failed", err))
			return
		}
		branchNames, head := cloneBranchesFromRefs(refList)
		view.SetBranches(branchNames, head)
		view.SetStatus(i18n.Tf("Dialog.Clone.Status.Found", len(branchNames)))
	})
}

func cloneBranchesFromRefs(refList []transport.Ref) ([]string, string) {
	var branchNames []string
	head := ""
	for _, r := range refList {
		if r.Name == "HEAD" {
			head = strings.TrimPrefix(r.Symref, headsRefPrefix)
			continue
		}
		if name, ok := strings.CutPrefix(r.Name, headsRefPrefix); ok {
			branchNames = append(branchNames, name)
		}
	}
	sort.Strings(branchNames)
	return branchNames, head
}

func (a *App) startClone(result clone.Result) {
	a.RunOperation(i18n.T("Operation.Title.Clone"), func(ctx context.Context, reporter OperationReporter) error {
		prog := newOperationProgress(reporter)
		r, err := cloneRepository(ctx, result.URL, result.Directory, ops.CloneOptions{
			Branch:       result.Branch,
			SingleBranch: result.Branch != "",
			Depth:        result.Depth,
			Progress:     prog,
			Transport:    a.transportOptions(prog),
		})
		if err != nil {
			return err
		}
		if closeErr := r.Close(); closeErr != nil {
			return closeErr
		}
		reporter.Log(i18n.Tf("Operation.Log.Cloned", result.Directory))
		a.Post(func() { a.addClonedRepository(result.Directory) })
		return nil
	})
}

func (a *App) addClonedRepository(dir string) {
	name := filepath.Base(dir)
	node, err := a.registry.AddRepository(name, dir, a.groupParentForNewGroup())
	if err != nil {
		a.log.Warn("add cloned repository failed", "error", err)
		return
	}
	if err := a.cfg.Save(a.paths.ConfigFile()); err != nil {
		a.log.Warn("save config failed", "error", err)
	}
	a.refreshBranchCache()
	a.reposView.Render(a.registry, a.repoTreeState())
	a.ActivateRepository(node.ID)
}

func (a *App) openManageRemotes() {
	o := a.opened()
	if o == nil {
		return
	}
	view, err := newRemotesView(a.eng, a.remoteEntries(o))
	if err != nil {
		a.log.Warn("open remotes dialog failed", "error", err)
		return
	}
	view.OnAdd = func(name, url string) { a.addRemoteEntry(view, o, name, url) }
	view.OnEdit = func(name, url string) { a.editRemoteEntry(view, o, name, url) }
	view.OnRemove = func(name string) { a.removeRemoteEntry(view, o, name) }
	view.OnClose = func() {
		a.eng.CloseModal(view.Dialog())
	}
	a.eng.ShowModal(view.Dialog())
}

func (a *App) remoteEntries(o *openedRepository) []remotes.Entry {
	r, err := a.freshRepo(o)
	if err != nil {
		return nil
	}
	defer func() { _ = r.Close() }()
	list := remote.List(r.Config())
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	entries := make([]remotes.Entry, len(list))
	for i, entry := range list {
		entries[i] = remotes.Entry{Name: entry.Name, FetchURL: entry.FetchURL(), PushURL: entry.PushURL()}
	}
	return entries
}

func remoteErrorMessage(err error) string {
	switch {
	case errors.Is(err, ops.ErrInvalidRemoteName):
		return i18n.T("Dialog.Remotes.Error.InvalidName")
	case errors.Is(err, ops.ErrRemoteExists):
		return i18n.T("Dialog.Remotes.Error.Duplicate")
	case errors.Is(err, remote.ErrNoRemote):
		return i18n.T("Dialog.Remotes.Error.NotFound")
	default:
		return i18n.Tf("Dialog.Remotes.Error.Failed", err)
	}
}

func (a *App) mutateRemote(view *remotes.View, o *openedRepository, mutate func(*gitrepo.Repository) error) {
	r, err := a.freshRepo(o)
	if err != nil {
		view.SetError(remoteErrorMessage(err))
		return
	}
	defer func() { _ = r.Close() }()
	if err := mutate(r); err != nil {
		view.SetError(remoteErrorMessage(err))
		return
	}
	view.SetError("")
	view.SetEntries(a.remoteEntries(o))
	a.RefreshRepository()
}

func (a *App) addRemoteEntry(view *remotes.View, o *openedRepository, name, url string) {
	a.mutateRemote(view, o, func(r *gitrepo.Repository) error { return ops.AddRemote(r, name, url) })
}

func (a *App) editRemoteEntry(view *remotes.View, o *openedRepository, name, url string) {
	a.mutateRemote(view, o, func(r *gitrepo.Repository) error { return ops.SetRemoteURL(r, name, url, false) })
}

func (a *App) removeRemoteEntry(view *remotes.View, o *openedRepository, name string) {
	a.mutateRemote(view, o, func(r *gitrepo.Repository) error { return ops.RemoveRemote(r, name) })
}
