package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/gitcore/ops"
	"github.com/oops1/gogit/internal/gitcore/progress"
	"github.com/oops1/gogit/internal/gitcore/refs"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/gitcore/worktree"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/branches"
	"github.com/oops1/gogit/internal/ui/changes"
	"github.com/oops1/gogit/internal/ui/merge"
	"github.com/oops1/gogit/internal/ui/style"
)

var newMergeView = merge.NewView

var runMerge = ops.Merge

var runAbortMerge = ops.AbortOperation

var runResolveConflicts = ops.ResolveConflicts

var readMergeState = ops.ReadMergeState

type mergeBanner struct {
	panel  *widget.DockPanel
	text   *widget.Label
	commit *widget.Button
	abort  *widget.Button
}

func bindMergeBanner(named map[string]widget.Widget) (mergeBanner, error) {
	var b mergeBanner
	var ok bool
	if b.panel, ok = named["mergeBanner"].(*widget.DockPanel); !ok {
		return mergeBanner{}, fmt.Errorf("%w: mergeBanner", ErrWidgetMissing)
	}
	if b.text, ok = named["mergeBannerText"].(*widget.Label); !ok {
		return mergeBanner{}, fmt.Errorf("%w: mergeBannerText", ErrWidgetMissing)
	}
	if b.commit, ok = named["mergeBannerCommit"].(*widget.Button); !ok {
		return mergeBanner{}, fmt.Errorf("%w: mergeBannerCommit", ErrWidgetMissing)
	}
	if b.abort, ok = named["mergeBannerAbort"].(*widget.Button); !ok {
		return mergeBanner{}, fmt.Errorf("%w: mergeBannerAbort", ErrWidgetMissing)
	}
	return b, nil
}

func (a *App) applyMergeBannerTheme(t *widget.Theme) {
	p := style.Of(t)
	p.Banner(a.banner.panel, a.banner.text)
	p.Primary(a.banner.commit)
	p.Quiet(a.banner.abort)
}

func (a *App) registerMergeHandlers() {
	a.handlers[CmdMerge] = func() { a.openMerge("") }
	a.handlers[CmdAbortMerge] = a.confirmAbortMerge
	a.branchesView.OnMenu = a.branchMenu
	a.banner.commit.OnClick = func() { a.Dispatch(CmdContinue) }
	a.banner.abort.OnClick = func() { a.Dispatch(CmdAbortMerge) }
}

func (a *App) branchMenu(ref refs.Name) []widget.MenuItem {
	state := a.State()
	current := a.currentBranchName()
	var items []widget.MenuItem
	if ref != refs.BranchName(current) && mergeable(ref) {
		item := menuItem("Menu.Context.MergeIntoCurrent", func() { a.openMerge(ref.Short()) })
		item.Disabled = !state.Enabled(CmdMerge)
		items = append(items, item)
	}
	items = append(items, a.switchItems(ref)...)
	items = append(items, a.compareItems(ref)...)
	items = append(items, a.deleteTagItems(ref)...)
	return append(items, a.reflogItems(ref)...)
}

func mergeable(ref refs.Name) bool {
	return ref.IsBranch() || ref.IsRemote() || ref.IsTag()
}

func (a *App) currentBranchName() string {
	o := a.opened()
	if o == nil {
		return ""
	}
	snap, err := loadBranchSnapshot(o.store)
	if err != nil {
		return ""
	}
	return snap.Current
}

func (a *App) openMerge(selected string) {
	o := a.opened()
	if o == nil {
		return
	}
	snap, err := loadBranchSnapshot(o.store)
	if err != nil {
		a.log.Warn("read branches for merge dialog failed", "error", err)
		return
	}
	view, err := newMergeView()
	if err != nil {
		a.log.Warn("open merge dialog failed", "error", err)
		return
	}
	view.SetKnown(mergeCandidates(snap), selected)
	view.OnOK = func(req merge.Request) {
		a.eng.CloseModal(view.Dialog())
		a.startMerge(req)
	}
	view.OnCancel = func() { a.eng.CloseModal(view.Dialog()) }
	a.showModal(view.Dialog(), view)
}

func mergeCandidates(snap branches.Snapshot) merge.Known {
	known := merge.Known{Current: snap.Current}
	if snap.Detached {
		known.Current = refs.HEAD.String()
	}
	for _, b := range snap.Local {
		if name := b.Name.Short(); name != snap.Current {
			known.Candidates = append(known.Candidates, name)
		}
	}
	for _, r := range snap.Remotes {
		for _, b := range r.Branches {
			if b.SymbolicTarget == "" {
				known.Candidates = append(known.Candidates, b.Name.Short())
			}
		}
	}
	for _, t := range snap.Tags {
		known.Candidates = append(known.Candidates, t.Name.Short())
	}
	return known
}

func mergeOptions(req merge.Request, prog progress.Func) ops.MergeOptions {
	opts := ops.MergeOptions{NoCommit: req.NoCommit, Progress: prog}
	switch req.Mode {
	case merge.ModeMergeCommit:
		opts.Mode = ops.MergeNoFastForward
	case merge.ModeFastForwardOnly:
		opts.Mode = ops.MergeFastForwardOnly
	case merge.ModeSquash:
		opts.Mode = ops.MergeSquash
	default:
		opts.Mode = ops.MergeFastForward
	}
	return opts
}

func (a *App) startMerge(req merge.Request) {
	o := a.opened()
	if o == nil {
		return
	}
	a.RunOperation(i18n.T("Operation.Title.Merge"), func(ctx context.Context, reporter OperationReporter) error {
		defer a.Post(a.finishMerge)
		r, err := a.freshRepo(o)
		if err != nil {
			return err
		}
		defer func() { _ = r.Close() }()
		result, err := runMerge(ctx, r, req.Source, mergeOptions(req, newOperationProgress(reporter)))
		reportMerge(reporter, req, result, err)
		return err
	})
}

func reportMerge(reporter OperationReporter, req merge.Request, result ops.MergeResult, err error) {
	var overwrite *ops.OverwriteError
	switch {
	case errors.As(err, &overwrite):
		reporter.Log(i18n.Tf("Operation.Log.MergeBlocked", overwriteList(overwrite)))
	case errors.Is(err, ops.ErrCannotFastForward):
		reporter.Log(i18n.T("Operation.Log.MergeNotFastForward"))
	case errors.Is(err, ops.ErrUnrelatedHistories):
		reporter.Log(i18n.T("Operation.Log.MergeUnrelated"))
	case err != nil:
	case result.UpToDate:
		reporter.Log(i18n.T("Operation.Log.MergeUpToDate"))
	case result.FastForward:
		reporter.Log(i18n.Tf("Operation.Log.MergeFastForward", shortHash(result.New)))
	case result.Committed:
		reporter.Log(i18n.Tf("Operation.Log.MergeCommitted", shortHash(result.New)))
	case !result.Clean():
		reporter.Log(i18n.Tf("Operation.Log.MergeConflicts", len(result.Conflicts)))
		for _, path := range result.Conflicts {
			reporter.Log(i18n.Tf("Operation.Log.MergeConflictPath", path))
		}
	case req.Mode == merge.ModeSquash:
		reporter.Log(i18n.T("Operation.Log.MergeSquashed"))
	default:
		reporter.Log(i18n.T("Operation.Log.MergeStopped"))
	}
}

func overwriteList(overwrite *ops.OverwriteError) string {
	return strings.Join(overwrite.Paths, ", ")
}

func (a *App) finishMerge() {
	a.reloadWorktree()
	a.RefreshRepository()
}

func (a *App) confirmAbortMerge() {
	a.askConfirm(i18n.T("Dialog.AbortMerge.Title"), i18n.T("Dialog.AbortMerge.Message"), func(ok bool) {
		if !ok {
			return
		}
		a.startWrite(func(ctx context.Context, r *gitrepo.Repository) error {
			return runAbortMerge(ctx, r)
		}, func(err error) {
			if err != nil {
				a.log.Warn("abort merge failed", "error", err)
				a.statusLabel.SetText(i18n.Tf("Status.AbortMergeFailed", err))
				return
			}
			a.statusLabel.SetText(i18n.T("Status.MergeAborted"))
			a.RefreshRepository()
		})
	})
}

func (a *App) conflictItems(row changes.Row) []widget.MenuItem {
	if row.Status != changes.RowConflict {
		return nil
	}
	path := row.RelPath
	return []widget.MenuItem{
		menuItem("Menu.Context.ResolveConflict", func() { a.openConflictEditor(path) }),
		menuItem("Menu.Context.TakeOurs", func() { a.takeSide(path, ops.TakeOurs) }),
		menuItem("Menu.Context.TakeTheirs", func() { a.takeSide(path, ops.TakeTheirs) }),
		menuItem("Menu.Context.MarkResolved", a.stageSelected),
		menuSeparator(),
	}
}

func (a *App) takeSide(path string, side ops.ConflictSide) {
	a.startWrite(func(ctx context.Context, r *gitrepo.Repository) error {
		return runResolveConflicts(ctx, r, []string{path}, side)
	}, func(err error) {
		if err != nil {
			a.log.Warn("resolve conflict failed", "path", path, "error", err)
			a.statusLabel.SetText(i18n.Tf("Status.ResolveFailed", err))
			return
		}
		a.clearFilesSelection()
	})
}

func (a *App) workingMergeState() ops.MergeState {
	o := a.opened()
	if o == nil {
		return ops.MergeState{}
	}
	state, err := readMergeState(o.repo)
	if err != nil {
		a.log.Warn("read merge state failed", "error", err)
		return ops.MergeState{}
	}
	return state
}

func (a *App) showMergeState(state ops.MergeState, conflicts int) {
	a.setMerging(state.InProgress(), state.Operation() == ops.OperationRebase)
	a.banner.panel.SetVisible(state.InProgress())
	if !state.InProgress() {
		return
	}
	a.banner.text.SetText(bannerText(state, conflicts))
	a.banner.commit.SetText(i18n.T(bannerActionKey(state.Operation())))
}

func bannerActionKey(operation ops.Operation) string {
	if operation == ops.OperationRebase {
		return "Banner.Rebase.Continue"
	}
	return "Banner.Merge.Commit"
}

var bannerKeys = map[ops.Operation][2]string{
	ops.OperationMerge:      {"Banner.Merge.Ready", "Banner.Merge.Conflicts"},
	ops.OperationCherryPick: {"Banner.CherryPick.Ready", "Banner.CherryPick.Conflicts"},
	ops.OperationRevert:     {"Banner.Revert.Ready", "Banner.Revert.Conflicts"},
	ops.OperationRebase:     {"Banner.Rebase.Ready", "Banner.Rebase.Conflicts"},
}

func bannerText(state ops.MergeState, conflicts int) string {
	keys := bannerKeys[state.Operation()]
	name := operationSubject(state)
	if conflicts > 0 {
		return i18n.Tf(keys[1], name, conflicts)
	}
	return i18n.Tf(keys[0], name)
}

func operationSubject(state ops.MergeState) string {
	subject, _, _ := strings.Cut(state.Message, "\n")
	switch state.Operation() {
	case ops.OperationCherryPick:
		return shortHash(state.Picked) + " " + subject
	case ops.OperationRevert:
		return shortHash(state.Reverted)
	case ops.OperationRebase:
		return subject
	}
	return mergeSourceName(state, subject)
}

func mergeSourceName(state ops.MergeState, subject string) string {
	if _, quoted, ok := strings.Cut(subject, "'"); ok {
		if name, _, closed := strings.Cut(quoted, "'"); closed && name != "" {
			return name
		}
	}
	return shortHash(state.Heads[0])
}

func conflictEntryCount(entries []worktree.Entry) int {
	count := 0
	for _, e := range entries {
		if e.Conflict != worktree.ConflictNone {
			count++
		}
	}
	return count
}

func (a *App) setMerging(merging, rebasing bool) {
	a.mu.Lock()
	changed := a.state.Merging != merging || a.state.Rebasing != rebasing
	a.state.Merging, a.state.Rebasing = merging, rebasing
	a.mu.Unlock()
	if changed {
		a.refreshCommands()
	}
}
