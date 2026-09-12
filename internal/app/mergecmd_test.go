package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/ops"
	"github.com/oops1/gogit/internal/gitcore/refs"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/branches"
	"github.com/oops1/gogit/internal/ui/changes"
	"github.com/oops1/gogit/internal/ui/commit"
	"github.com/oops1/gogit/internal/ui/merge"
)

func mergeLines(seed string) string {
	var out []string
	for i := range 10 {
		out = append(out, seed+" line "+strconv.Itoa(i))
	}
	return strings.Join(out, "\n") + "\n"
}

func withLine(text string, line int, replacement string) string {
	parts := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	parts[line] = replacement
	return strings.Join(parts, "\n") + "\n"
}

func buildForkedRepo(t *testing.T, target string, conflict bool) {
	t.Helper()
	initTestRepoWithBranch(t, target, "main")
	setTestUserIdentity(t, target)
	r, err := gitrepo.Open(target, gitrepo.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	ctx := t.Context()
	commitFiles := func(message string, files map[string]string) hash.ObjectID {
		var paths []string
		for name, content := range files {
			if err := writeFile(target, name, content); err != nil {
				t.Fatal(err)
			}
			paths = append(paths, name)
		}
		if err := ops.Stage(ctx, r, paths, ops.StageOptions{}); err != nil {
			t.Fatal(err)
		}
		id, err := ops.Commit(ctx, r, ops.CommitOptions{Message: message})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	f := mergeLines("f")
	base := commitFiles("base", map[string]string{"f.txt": f, "keep.txt": "keep\n"})
	if err := ops.CreateBranch(ctx, r, "feature", base, ops.CreateBranchOptions{}); err != nil {
		t.Fatal(err)
	}
	theirs := map[string]string{"g.txt": "theirs\n"}
	ours := map[string]string{"f.txt": withLine(f, 0, "OURS")}
	if conflict {
		ours["f.txt"] = withLine(f, 4, "OURS")
		theirs = map[string]string{"f.txt": withLine(f, 4, "THEIRS")}
	}
	commitFiles("ours", ours)
	if err := ops.Switch(ctx, r, "feature", ops.SwitchOptions{}); err != nil {
		t.Fatal(err)
	}
	commitFiles("theirs", theirs)
	if err := ops.Switch(ctx, r, "main", ops.SwitchOptions{}); err != nil {
		t.Fatal(err)
	}
}

func forkedApp(t *testing.T, conflict bool) (*App, string) {
	t.Helper()
	target := filepath.Join(t.TempDir(), "main")
	buildForkedRepo(t, target, conflict)
	a := activatedWorkingApp(t, target)
	waitForWorkingIdle(t, a)
	return a, target
}

func captureMergeViews(t *testing.T) *[]*merge.View {
	t.Helper()
	views := &[]*merge.View{}
	prev := newMergeView
	newMergeView = func() (*merge.View, error) {
		view, err := prev()
		if err == nil {
			*views = append(*views, view)
		}
		return view, err
	}
	t.Cleanup(func() { newMergeView = prev })
	return views
}

func mergeThrough(t *testing.T, a *App, req merge.Request) []string {
	t.Helper()
	views := captureOperationViews(t)
	readOnDispatcher(t, a, func() bool { a.startMerge(req); return true })
	view := lastOperationView(t, views)
	waitForFinishedOperation(t, a, view)
	waitForPostQueueDrain(t, a)
	waitForWorkingIdle(t, a)
	return readOnDispatcher(t, a, view.Lines)
}

func waitForBanner(t *testing.T, a *App, visible bool) string {
	t.Helper()
	deadline := time.Now().Add(testTimeout)
	for readOnDispatcher(t, a, a.banner.panel.IsVisible) != visible {
		if time.Now().After(deadline) {
			t.Fatalf("banner visible = %v, want %v", !visible, visible)
		}
		time.Sleep(10 * time.Millisecond)
	}
	return readOnDispatcher(t, a, a.banner.text.Text)
}

func waitForBannerText(t *testing.T, a *App, want string) {
	t.Helper()
	deadline := time.Now().Add(testTimeout)
	for readOnDispatcher(t, a, a.banner.text.Text) != want {
		if time.Now().After(deadline) {
			t.Fatalf("banner = %q, want %q", readOnDispatcher(t, a, a.banner.text.Text), want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func headParents(t *testing.T, target string) []hash.ObjectID {
	t.Helper()
	r, err := gitrepo.Open(target, gitrepo.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	db, err := odb.Open(r.ObjectsDir(), odb.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	store, err := refs.Open(refs.Options{GitDir: r.GitDir(), CommonDir: r.CommonDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	head, err := store.Resolve(refs.HEAD)
	if err != nil {
		t.Fatal(err)
	}
	c, err := db.Commit(head.Target)
	if err != nil {
		t.Fatal(err)
	}
	return c.Parents
}

func TestTheBranchMenuFollowsTheMergeState(t *testing.T) {
	a := newTestApp(t)
	items := a.menu.Items()[branchMenuIndex]
	if items.Text != widget.Tr("Menu.Branch") || len(items.Items) != len(branchMenuTree) {
		t.Fatalf("branch menu = %q with %d items", items.Text, len(items.Items))
	}
	for _, c := range []struct {
		state        State
		merge, abort bool
	}{
		{State{}, false, false},
		{State{ActiveRepository: "r"}, true, false},
		{State{ActiveRepository: "r", Merging: true}, false, true},
	} {
		if c.state.Enabled(CmdMerge) != c.merge || c.state.Enabled(CmdAbortMerge) != c.abort {
			t.Errorf("state %+v: merge %v abort %v", c.state, c.state.Enabled(CmdMerge), c.state.Enabled(CmdAbortMerge))
		}
	}
	if !(State{ActiveRepository: "r", Merging: true}).Enabled(CmdCommit) {
		t.Error("a merge in progress must allow a commit without staged changes")
	}
}

func TestMergingFromTheDialogCommitsBothParents(t *testing.T) {
	a, target := forkedApp(t, false)
	views := captureMergeViews(t)
	opViews := captureOperationViews(t)

	readOnDispatcher(t, a, func() bool { return a.Dispatch(CmdMerge) })
	view := (*views)[0]
	if got := readOnDispatcher(t, a, func() string { return view.Request().Source }); got != "feature" {
		t.Fatalf("source = %q", got)
	}
	readOnDispatcher(t, a, func() bool { view.Dialog().DefaultAction(); return true })
	op := lastOperationView(t, opViews)
	waitForFinishedOperation(t, a, op)

	lines := readOnDispatcher(t, a, op.Lines)
	if len(lines) == 0 || !strings.HasPrefix(lines[len(lines)-1], strings.SplitN(i18n.T("Operation.Log.MergeCommitted"), "%", 2)[0]) {
		t.Fatalf("log = %v", lines)
	}
	if parents := headParents(t, target); len(parents) != 2 {
		t.Fatalf("parents = %v", parents)
	}
}

func TestCancellingTheMergeDialogDoesNothing(t *testing.T) {
	a, target := forkedApp(t, false)
	views := captureMergeViews(t)

	readOnDispatcher(t, a, func() bool { a.openMerge("feature"); return true })
	readOnDispatcher(t, a, func() bool { (*views)[0].Dialog().CancelAction(); return true })

	if parents := headParents(t, target); len(parents) != 1 {
		t.Fatalf("parents = %v", parents)
	}
}

func TestAConflictRaisesTheBannerAndPreparesTheMergeCommit(t *testing.T) {
	a, _ := forkedApp(t, true)

	lines := mergeThrough(t, a, merge.Request{Source: "feature"})

	if !slices.Contains(lines, i18n.Tf("Operation.Log.MergeConflictPath", "f.txt")) {
		t.Fatalf("log = %v", lines)
	}
	if text := waitForBanner(t, a, true); text != i18n.Tf("Banner.Merge.Conflicts", "feature", 1) {
		t.Fatalf("banner = %q", text)
	}
	if state := a.State(); !state.Merging || !state.Enabled(CmdAbortMerge) || state.Enabled(CmdMerge) {
		t.Fatalf("state = %+v", state)
	}
	var shown commit.Model
	a.showCommit = func(initial commit.Model, _ func(commit.Model, bool)) { shown = initial }
	readOnDispatcher(t, a, func() bool { a.banner.commit.OnClick(); return true })
	if !shown.Merging || !strings.HasPrefix(shown.Message, "Merge branch 'feature'") {
		t.Fatalf("commit model = %+v", shown)
	}
}

func TestTakingASideSettlesTheConflict(t *testing.T) {
	a, target := forkedApp(t, true)
	mergeThrough(t, a, merge.Request{Source: "feature"})
	waitForBanner(t, a, true)

	items := readOnDispatcher(t, a, func() []widget.MenuItem {
		return a.conflictItems(changes.Row{Status: changes.RowConflict, RelPath: "f.txt"})
	})
	if len(items) != 4 || items[1].Text != i18n.T("Menu.Context.TakeTheirs") {
		t.Fatalf("items = %+v", items)
	}
	readOnDispatcher(t, a, func() bool { items[1].OnClick(); return true })

	waitForBannerText(t, a, i18n.Tf("Banner.Merge.Ready", "feature"))
	data, err := os.ReadFile(filepath.Join(target, "f.txt"))
	if err != nil || !strings.Contains(string(data), "THEIRS") {
		t.Fatalf("f.txt = %q, %v", data, err)
	}
}

func TestOnlyConflictedRowsOfferASide(t *testing.T) {
	a := newTestApp(t)

	if items := a.conflictItems(changes.Row{Status: changes.RowModified, RelPath: "f.txt"}); items != nil {
		t.Fatalf("items = %+v", items)
	}
	items := a.conflictItems(changes.Row{Status: changes.RowConflict, RelPath: "f.txt"})
	items[0].OnClick()
}

func TestAFailedResolutionIsReported(t *testing.T) {
	a, _ := forkedApp(t, true)
	prev := runResolveConflicts
	runResolveConflicts = func(context.Context, *gitrepo.Repository, []string, ops.ConflictSide) error {
		return errors.New("boom")
	}
	t.Cleanup(func() { runResolveConflicts = prev })

	readOnDispatcher(t, a, func() bool { a.takeSide("f.txt", ops.TakeOurs); return true })

	waitForStatusText(t, a, i18n.Tf("Status.ResolveFailed", errors.New("boom")))
}

func waitForStatusText(t *testing.T, a *App, want string) {
	t.Helper()
	deadline := time.Now().Add(testTimeout)
	for readOnDispatcher(t, a, a.statusLabel.Text) != want {
		if time.Now().After(deadline) {
			t.Fatalf("status = %q, want %q", readOnDispatcher(t, a, a.statusLabel.Text), want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestAbortingTheMergeAsksFirstAndRestoresHead(t *testing.T) {
	a, target := forkedApp(t, true)
	mergeThrough(t, a, merge.Request{Source: "feature"})
	waitForBanner(t, a, true)
	answers := []bool{false, true}
	a.askConfirm = func(_, _ string, cb func(bool)) {
		answer := answers[0]
		answers = answers[1:]
		cb(answer)
	}

	readOnDispatcher(t, a, func() bool { a.banner.abort.OnClick(); return true })
	if !a.State().Merging {
		t.Fatal("a declined abort still ended the merge")
	}
	readOnDispatcher(t, a, func() bool { return a.Dispatch(CmdAbortMerge) })

	waitForStatusText(t, a, i18n.T("Status.MergeAborted"))
	waitForBanner(t, a, false)
	data, err := os.ReadFile(filepath.Join(target, "f.txt"))
	if err != nil || strings.Contains(string(data), "<<<<<<<") {
		t.Fatalf("f.txt = %q, %v", data, err)
	}
}

func TestAFailedAbortIsReported(t *testing.T) {
	a, _ := forkedApp(t, true)
	prev := runAbortMerge
	runAbortMerge = func(context.Context, *gitrepo.Repository) error { return errors.New("stuck") }
	t.Cleanup(func() { runAbortMerge = prev })
	a.askConfirm = func(_, _ string, cb func(bool)) { cb(true) }

	readOnDispatcher(t, a, func() bool { a.confirmAbortMerge(); return true })

	waitForStatusText(t, a, i18n.Tf("Status.AbortMergeFailed", errors.New("stuck")))
}

func TestTheBranchMenuMergesAnotherBranchIntoTheCurrentOne(t *testing.T) {
	a, _ := forkedApp(t, false)
	views := captureMergeViews(t)

	if items := readOnDispatcher(t, a, func() []widget.MenuItem { return a.branchMenu("refs/stash") }); items != nil {
		t.Fatalf("items = %+v", items)
	}
	if items := readOnDispatcher(t, a, func() []widget.MenuItem { return a.branchMenu(refs.BranchName("main")) }); len(items) != 1 {
		t.Fatalf("the current branch offers %+v", items)
	}
	items := readOnDispatcher(t, a, func() []widget.MenuItem { return a.branchMenu(refs.BranchName("feature")) })
	if items[0].Text != i18n.T("Menu.Context.MergeIntoCurrent") || items[0].Disabled {
		t.Fatalf("items = %+v", items)
	}
	readOnDispatcher(t, a, func() bool { items[0].OnClick(); return true })

	if len(*views) != 1 || readOnDispatcher(t, a, func() string { return (*views)[0].Request().Source }) != "feature" {
		t.Fatalf("views = %d", len(*views))
	}
}

func TestWithoutARepositoryTheMergeCommandsDoNothing(t *testing.T) {
	a := newTestApp(t)
	views := captureMergeViews(t)

	a.openMerge("feature")
	a.startMerge(merge.Request{Source: "feature"})

	if len(*views) != 0 || a.currentBranchName() != "" || a.workingMergeState().InProgress() {
		t.Fatal("merge commands acted without a repository")
	}
}

func TestMergeDialogFailuresAreLogged(t *testing.T) {
	a, _ := forkedApp(t, false)
	prevView := newMergeView
	newMergeView = func() (*merge.View, error) { return nil, errors.New("no dialog") }
	t.Cleanup(func() { newMergeView = prevView })
	readOnDispatcher(t, a, func() bool { a.openMerge(""); return true })

	prevLoad := loadBranchSnapshot
	loadBranchSnapshot = func(*refs.Store) (branches.Snapshot, error) { return branches.Snapshot{}, errors.New("no refs") }
	t.Cleanup(func() { loadBranchSnapshot = prevLoad })
	readOnDispatcher(t, a, func() bool { a.openMerge(""); return true })
	if name := readOnDispatcher(t, a, a.currentBranchName); name != "" {
		t.Fatalf("current = %q", name)
	}
}

func TestAnUnreadableMergeStateReadsAsNoMerge(t *testing.T) {
	a, _ := forkedApp(t, false)
	prev := readMergeState
	readMergeState = func(*gitrepo.Repository) (ops.MergeState, error) { return ops.MergeState{}, errors.New("garbled") }
	t.Cleanup(func() { readMergeState = prev })

	if state := a.workingMergeState(); state.InProgress() {
		t.Fatalf("state = %+v", state)
	}
}

func TestAMergeThatCannotOpenTheRepositoryFails(t *testing.T) {
	a, _ := forkedApp(t, false)
	prev := openGitRepository
	openGitRepository = func(string, gitrepo.OpenOptions) (*gitrepo.Repository, error) { return nil, errors.New("gone") }
	t.Cleanup(func() { openGitRepository = prev })
	views := captureOperationViews(t)

	readOnDispatcher(t, a, func() bool { a.startMerge(merge.Request{Source: "feature"}); return true })

	waitForFinishedOperation(t, a, lastOperationView(t, views))
}

func TestTheMergeLogExplainsEveryOutcome(t *testing.T) {
	a := newTestApp(t)
	id := hash.SumSHA1("commit", []byte("x"))
	for _, c := range []struct {
		name   string
		req    merge.Request
		result ops.MergeResult
		err    error
		want   string
	}{
		{"blocked", merge.Request{}, ops.MergeResult{}, &ops.OverwriteError{Paths: []string{"a", "b"}}, i18n.Tf("Operation.Log.MergeBlocked", "a, b")},
		{"not fast-forward", merge.Request{}, ops.MergeResult{}, ops.ErrCannotFastForward, i18n.T("Operation.Log.MergeNotFastForward")},
		{"unrelated", merge.Request{}, ops.MergeResult{}, ops.ErrUnrelatedHistories, i18n.T("Operation.Log.MergeUnrelated")},
		{"up to date", merge.Request{}, ops.MergeResult{UpToDate: true}, nil, i18n.T("Operation.Log.MergeUpToDate")},
		{"fast-forward", merge.Request{}, ops.MergeResult{FastForward: true, New: id}, nil, i18n.Tf("Operation.Log.MergeFastForward", shortHash(id))},
		{"squash", merge.Request{Mode: merge.ModeSquash}, ops.MergeResult{}, nil, i18n.T("Operation.Log.MergeSquashed")},
		{"no commit", merge.Request{NoCommit: true}, ops.MergeResult{}, nil, i18n.T("Operation.Log.MergeStopped")},
	} {
		views := captureOperationViews(t)
		a.RunOperation(c.name, func(_ context.Context, reporter OperationReporter) error {
			reportMerge(reporter, c.req, c.result, c.err)
			return nil
		})
		view := lastOperationView(t, views)
		waitForFinishedOperation(t, a, view)
		if lines := readOnDispatcher(t, a, view.Lines); !slices.Contains(lines, c.want) {
			t.Errorf("%s: log = %v", c.name, lines)
		}
	}
	silent := captureOperationViews(t)
	a.RunOperation("other", func(_ context.Context, reporter OperationReporter) error {
		reportMerge(reporter, merge.Request{}, ops.MergeResult{}, errors.New("other"))
		return nil
	})
	waitForFinishedOperation(t, a, lastOperationView(t, silent))
}

func TestMergeCandidatesListEverythingButTheCurrentBranch(t *testing.T) {
	snap := branches.Snapshot{
		Current: "main",
		Local:   []branches.Branch{{Name: refs.BranchName("main")}, {Name: refs.BranchName("feature")}},
		Remotes: []branches.Remote{{Name: "origin", Branches: []branches.Branch{
			{Name: refs.RemoteBranchName("origin", "HEAD"), SymbolicTarget: refs.RemoteBranchName("origin", "main")},
			{Name: refs.RemoteBranchName("origin", "main")},
		}}},
		Tags: []branches.Tag{{Name: refs.TagName("v1")}},
	}

	known := mergeCandidates(snap)
	if known.Current != "main" || !slices.Equal(known.Candidates, []string{"feature", "origin/main", "v1"}) {
		t.Fatalf("known = %+v", known)
	}
	if detached := mergeCandidates(branches.Snapshot{Detached: true}); detached.Current != "HEAD" {
		t.Fatalf("detached = %+v", detached)
	}
}

func TestMergeOptionsFollowTheChosenMode(t *testing.T) {
	for mode, want := range map[merge.Mode]ops.MergeMode{
		merge.ModeFastForward:     ops.MergeFastForward,
		merge.ModeMergeCommit:     ops.MergeNoFastForward,
		merge.ModeFastForwardOnly: ops.MergeFastForwardOnly,
		merge.ModeSquash:          ops.MergeSquash,
	} {
		if got := mergeOptions(merge.Request{Mode: mode, NoCommit: true}, nil); got.Mode != want || !got.NoCommit {
			t.Errorf("mode %d: %+v", mode, got)
		}
	}
}

func TestTheMergeSourceIsReadFromTheMessage(t *testing.T) {
	id := hash.SumSHA1("commit", []byte("x"))
	for message, want := range map[string]string{
		"Merge branch 'feature' into dev\n":         "feature",
		"Merge remote-tracking branch 'origin/x'\n": "origin/x",
		"Bring it in\n":         shortHash(id),
		"Merge commit ''\n":     shortHash(id),
		"Merge 'unterminated\n": shortHash(id),
	} {
		if got := operationSubject(ops.MergeState{Heads: []hash.ObjectID{id}, Message: message}); got != want {
			t.Errorf("%q: %q, want %q", message, got, want)
		}
	}
}

func TestTheBannerNamesTheOperationInProgress(t *testing.T) {
	newTestApp(t)
	id := hash.SumSHA1("commit", []byte("x"))
	for _, c := range []struct {
		state     ops.MergeState
		conflicts int
		want      string
	}{
		{ops.MergeState{Picked: id, Message: "picked change\n\nbody\n"}, 2, i18n.Tf("Banner.CherryPick.Conflicts", shortHash(id)+" picked change", 2)},
		{ops.MergeState{Picked: id, Message: "picked change\n"}, 0, i18n.Tf("Banner.CherryPick.Ready", shortHash(id)+" picked change")},
		{ops.MergeState{Reverted: id, Message: "Revert \"x\"\n"}, 1, i18n.Tf("Banner.Revert.Conflicts", shortHash(id), 1)},
		{ops.MergeState{Reverted: id}, 0, i18n.Tf("Banner.Revert.Ready", shortHash(id))},
	} {
		if got := bannerText(c.state, c.conflicts); got != c.want {
			t.Errorf("banner = %q, want %q", got, c.want)
		}
	}
}

func TestNewFailsWithoutTheMergeBanner(t *testing.T) {
	for _, missing := range []string{"mergeBanner", "mergeBannerText", "mergeBannerCommit", "mergeBannerAbort"} {
		named := map[string]widget.Widget{
			"mergeBanner":       widget.NewDockPanel(),
			"mergeBannerText":   widget.NewLabel("", widget.CurrentTheme().LabelText),
			"mergeBannerCommit": widget.NewButton(""),
			"mergeBannerAbort":  widget.NewButton(""),
		}
		delete(named, missing)
		if _, err := bindMergeBanner(named); !errors.Is(err, ErrWidgetMissing) {
			t.Fatalf("without %s: err = %v", missing, err)
		}
	}
}
