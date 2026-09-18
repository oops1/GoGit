package app

import (
	"context"
	"errors"
	"iter"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/gitcore/diff"
	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/linelog"
	"github.com/oops1/gogit/internal/gitcore/ops"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/changes"
	"github.com/oops1/gogit/internal/ui/gitdiff"
	"github.com/oops1/gogit/internal/ui/investigate"
)

func captureInvestigateViews(t *testing.T) *[]*investigate.View {
	t.Helper()
	views := &[]*investigate.View{}
	prev := newInvestigateView
	newInvestigateView = func() (*investigate.View, error) {
		view, err := prev()
		if err == nil {
			*views = append(*views, view)
		}
		return view, err
	}
	t.Cleanup(func() { newInvestigateView = prev })
	return views
}

func waitForInvestigation(t *testing.T, a *App, views *[]*investigate.View) *investigate.View {
	t.Helper()
	waitForDialog(t, a, func() int { return len(*views) }, "the investigate dialog")
	view := (*views)[len(*views)-1]
	deadline := time.Now().Add(testTimeout)
	for readOnDispatcher(t, a, view.Running) {
		if time.Now().After(deadline) {
			t.Fatal("the investigation did not finish")
		}
		time.Sleep(10 * time.Millisecond)
	}
	return view
}

func subjectsOf(entries []investigate.Entry) []string {
	var out []string
	for _, entry := range entries {
		out = append(out, entry.Subject)
	}
	return out
}

func TestTheFilesMenuInvestigatesTheWholeFile(t *testing.T) {
	a, _ := forkedApp(t, false)
	views := captureInvestigateViews(t)

	items := readOnDispatcher(t, a, func() []widget.MenuItem {
		return a.filesMenu(changes.Row{Name: "f.txt", RelPath: "f.txt", Status: changes.RowModified}, 0)
	})
	item, found := findMenuItem(items, i18n.T("Menu.Files.Investigate"))
	if !found || item.Disabled {
		t.Fatalf("items = %v", menuTexts(items))
	}
	readOnDispatcher(t, a, func() bool { item.OnClick(); return true })
	view := waitForInvestigation(t, a, views)

	entries := readOnDispatcher(t, a, view.Entries)
	if got := strings.Join(subjectsOf(entries), ","); got != "ours,base" {
		t.Fatalf("subjects = %s", got)
	}
	if readOnDispatcher(t, a, view.Hint) != i18n.Tf("Dialog.Investigate.Hint.Found", 2) {
		t.Fatalf("hint = %q", readOnDispatcher(t, a, view.Hint))
	}
	readOnDispatcher(t, a, func() bool { view.Dialog().CancelAction(); return true })
}

func TestTheDiffMenuInvestigatesTheLinesUnderTheCaret(t *testing.T) {
	a, target := forkedApp(t, false)
	views := captureInvestigateViews(t)
	head, err := os.ReadFile(filepath.Join(target, "f.txt"))
	if err != nil {
		t.Fatal(err)
	}
	shown := append([]byte("fresh line\n"), head...)
	runOnDispatcher(t, a, func() {
		a.shownMu.Lock()
		a.shownDiff = diffTarget{kind: diffKindWorktree, path: "f.txt", file: diff.File{OldPath: "f.txt", NewPath: "f.txt"}, oldData: head, newData: shown}
		a.shownMu.Unlock()
	})

	items := readOnDispatcher(t, a, func() []widget.MenuItem { return a.diffMenu(gitdiff.Spot{Side: widget.DiffRight, From: 1, To: 2}) })
	item, found := findMenuItem(items, i18n.T("Menu.Files.Investigate"))
	if !found || item.Disabled || !items[len(items)-2].Separator {
		t.Fatalf("items = %v", menuTexts(items))
	}
	readOnDispatcher(t, a, func() bool { item.OnClick(); return true })
	view := waitForInvestigation(t, a, views)

	if got := strings.Join(subjectsOf(readOnDispatcher(t, a, view.Entries)), ","); got != "ours,base" {
		t.Fatalf("subjects = %s", got)
	}
	readOnDispatcher(t, a, func() bool { view.Dialog().CancelAction(); return true })

	fresh := readOnDispatcher(t, a, func() []widget.MenuItem { return a.diffMenu(gitdiff.Spot{Side: widget.DiffRight, From: 0, To: 1}) })
	item, _ = findMenuItem(fresh, i18n.T("Menu.Files.Investigate"))
	readOnDispatcher(t, a, func() bool { item.OnClick(); return true })
	waitForStatusText(t, a, i18n.T("Status.InvestigateUncommitted"))
}

func TestTheDiffInvestigationNeedsContentAndARevision(t *testing.T) {
	a, _ := forkedApp(t, false)
	file := diff.File{OldPath: "old.txt", NewPath: "new.txt"}
	right := gitdiff.Spot{Side: widget.DiffRight, From: 0, To: 1}
	left := gitdiff.Spot{Side: widget.DiffLeft, From: 2, To: 4}
	commit := hash.SumSHA1("blob", []byte("commit"))

	cases := []struct {
		name   string
		target diffTarget
		spot   gitdiff.Spot
		commit hash.ObjectID
		ok     bool
		want   investigation
	}{
		{name: "binary", target: diffTarget{kind: diffKindIndex, file: diff.File{NewPath: "b", Binary: true}, newData: []byte("x")}, spot: right},
		{name: "absent side", target: diffTarget{kind: diffKindIndex, file: file}, spot: right},
		{name: "no caret", target: diffTarget{kind: diffKindIndex, file: file, newData: []byte("x\n")}, spot: gitdiff.Spot{Side: widget.DiffRight, From: -1}},
		{name: "commit without selection", target: diffTarget{file: file, newData: []byte("x\n")}, spot: right},
		{name: "index right", target: diffTarget{kind: diffKindIndex, file: file, newData: []byte("x\n")}, spot: right, ok: true,
			want: investigation{rev: "HEAD", selection: ops.LineSelection{Path: "new.txt", First: 1, Last: 1, Shown: []byte("x\n")}}},
		{name: "commit left", target: diffTarget{file: file, oldData: []byte("y\n")}, spot: left, commit: commit, ok: true,
			want: investigation{rev: commit.String() + "^", selection: ops.LineSelection{Path: "old.txt", First: 3, Last: 4, Shown: []byte("y\n")}}},
		{name: "commit right", target: diffTarget{file: diff.File{OldPath: "only.txt"}, newData: []byte("z\n")}, spot: right, commit: commit, ok: true,
			want: investigation{rev: commit.String(), selection: ops.LineSelection{Path: "only.txt", First: 1, Last: 1, Shown: []byte("z\n")}}},
	}
	for _, c := range cases {
		type outcome struct {
			found investigation
			ok    bool
		}
		result := readOnDispatcher(t, a, func() outcome {
			a.selectedCommit = c.commit
			found, ok := a.diffInvestigation(c.target, c.spot)
			return outcome{found: found, ok: ok}
		})
		got, ok := result.found, result.ok
		if ok != c.ok || ok && (got.rev != c.want.rev || got.selection.Path != c.want.selection.Path ||
			got.selection.First != c.want.selection.First || got.selection.Last != c.want.selection.Last ||
			string(got.selection.Shown) != string(c.want.selection.Shown)) {
			t.Errorf("%s: investigation = %+v, %v", c.name, got, ok)
		}
	}

	closed := newTestApp(t)
	if _, ok := closed.diffInvestigation(diffTarget{kind: diffKindIndex, file: file, newData: []byte("x\n")}, right); ok {
		t.Error("an investigation was offered without a repository")
	}
	if item := closed.investigateFileItem("f.txt", true); !item.Disabled {
		t.Error("the files menu offered an investigation without a repository")
	}
	closed.showInvestigation(investigation{}, []linelog.Spec{{Range: ",", Path: "f"}})
}

func TestTheFilesRevisionFollowsTheSelectedCommit(t *testing.T) {
	a, _ := forkedApp(t, false)
	commit := hash.SumSHA1("blob", []byte("picked"))

	got := readOnDispatcher(t, a, func() []string {
		before := a.filesRevision()
		a.filesMu.Lock()
		a.filesMode = filesModeCommit
		a.filesMu.Unlock()
		a.selectedCommit = commit
		after := a.filesRevision()
		a.filesMu.Lock()
		a.filesMode = filesModeWorking
		a.filesMu.Unlock()
		a.selectedCommit = hash.Zero
		return []string{before, after}
	})

	if got[0] != "HEAD" || got[1] != commit.String() {
		t.Fatalf("revisions = %v", got)
	}
}

func TestTheBlameDialogInvestigatesTheSelectedLine(t *testing.T) {
	a, _ := forkedApp(t, false)
	blames := captureBlameViews(t)
	views := captureInvestigateViews(t)

	readOnDispatcher(t, a, func() bool { a.openBlame("HEAD", "f.txt"); return true })
	blamed := waitForBlameView(t, a, blames)
	lines := readOnDispatcher(t, a, blamed.Lines)
	readOnDispatcher(t, a, func() bool { blamed.OnInvestigate(lines[1]); return true })
	view := waitForInvestigation(t, a, views)

	if got := strings.Join(subjectsOf(readOnDispatcher(t, a, view.Entries)), ","); got != "base" {
		t.Fatalf("subjects = %s", got)
	}
	readOnDispatcher(t, a, func() bool { view.Dialog().CancelAction(); return true })
}

func swapLineHistory(t *testing.T, fake func(ctx context.Context, opts ops.LineHistoryOptions) iter.Seq2[ops.LineHistoryEntry, error]) {
	t.Helper()
	prev := readLineHistory
	readLineHistory = func(ctx context.Context, _ *gitrepo.Repository, _ string, _ []linelog.Spec, opts ops.LineHistoryOptions) iter.Seq2[ops.LineHistoryEntry, error] {
		return fake(ctx, opts)
	}
	t.Cleanup(func() { readLineHistory = prev })
}

func TestAFailedInvestigationIsReported(t *testing.T) {
	a, _ := forkedApp(t, false)
	views := captureInvestigateViews(t)

	prevSpecs := readLineSpecs
	readLineSpecs = func(context.Context, *gitrepo.Repository, string, ops.LineSelection) ([]linelog.Spec, error) {
		return nil, errors.New("no specs")
	}
	readOnDispatcher(t, a, func() bool { a.investigateFile("f.txt"); return true })
	waitForStatusText(t, a, i18n.Tf("Status.InvestigateFailed", errors.New("no specs")))
	readLineSpecs = prevSpecs

	prevView := newInvestigateView
	newInvestigateView = func() (*investigate.View, error) { return nil, errors.New("no dialog") }
	readOnDispatcher(t, a, func() bool { a.investigateFile("f.txt"); return true })
	waitForStatusText(t, a, i18n.Tf("Status.InvestigateFailed", errors.New("no dialog")))
	newInvestigateView = prevView

	broken := errors.New("broken history")
	swapLineHistory(t, func(_ context.Context, opts ops.LineHistoryOptions) iter.Seq2[ops.LineHistoryEntry, error] {
		return func(yield func(ops.LineHistoryEntry, error) bool) {
			opts.Progress(0, 600)
			opts.Progress(1, 600)
			if !yield(ops.LineHistoryEntry{Commit: hash.SumSHA1("blob", []byte("m")), Subject: "merge", Parents: []hash.ObjectID{{1}, {2}}}, nil) {
				return
			}
			yield(ops.LineHistoryEntry{}, broken)
		}
	})
	readOnDispatcher(t, a, func() bool { a.investigateFile("f.txt"); return true })
	view := waitForInvestigation(t, a, views)

	entries := readOnDispatcher(t, a, view.Entries)
	if len(entries) != 1 || !entries[0].Merge {
		t.Fatalf("entries = %+v", entries)
	}
	if readOnDispatcher(t, a, view.Hint) != i18n.Tf("Dialog.Investigate.Hint.Failed", broken) {
		t.Fatalf("hint = %q", readOnDispatcher(t, a, view.Hint))
	}
}

func TestClosingTheInvestigationStopsTheSearch(t *testing.T) {
	a, _ := forkedApp(t, false)
	views := captureInvestigateViews(t)
	started := make(chan struct{})
	swapLineHistory(t, func(ctx context.Context, _ ops.LineHistoryOptions) iter.Seq2[ops.LineHistoryEntry, error] {
		return func(yield func(ops.LineHistoryEntry, error) bool) {
			close(started)
			<-ctx.Done()
			yield(ops.LineHistoryEntry{}, ctx.Err())
		}
	})

	readOnDispatcher(t, a, func() bool { a.investigateFile("f.txt"); return true })
	waitForDialog(t, a, func() int { return len(*views) }, "the investigate dialog")
	waitForChannel(t, started, "the line history")
	view := (*views)[0]
	readOnDispatcher(t, a, func() bool { view.Dialog().CancelAction(); return true })
	view = waitForInvestigation(t, a, views)

	if readOnDispatcher(t, a, view.Hint) != i18n.Tf("Dialog.Investigate.Hint.Cancelled", 0) {
		t.Fatalf("hint = %q", readOnDispatcher(t, a, view.Hint))
	}
	waitForReadJobs(t, a)
}

func TestTheLinesLabelNamesTheRange(t *testing.T) {
	newTestApp(t)
	if got := linesLabel(ops.LineSelection{}); got != i18n.T("Dialog.Investigate.WholeFile") {
		t.Fatalf("whole = %q", got)
	}
	if got := linesLabel(ops.LineSelection{First: 5}); got != i18n.Tf("Dialog.Investigate.Lines", 5, 5) {
		t.Fatalf("caret = %q", got)
	}
}
