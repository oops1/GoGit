package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/ops"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/journal"
	"github.com/oops1/gogit/internal/ui/reset"
)

func captureResetViews(t *testing.T) *[]*reset.View {
	t.Helper()
	views := &[]*reset.View{}
	prev := newResetView
	newResetView = func() (*reset.View, error) {
		view, err := prev()
		if err == nil {
			*views = append(*views, view)
		}
		return view, err
	}
	t.Cleanup(func() { newResetView = prev })
	return views
}

func TestTheJournalMenuOffersToResetTheBranch(t *testing.T) {
	a, target := forkedApp(t, false)
	id := branchTip(t, target, "feature")

	items := readOnDispatcher(t, a, func() []widget.MenuItem { return a.journalMenu(journal.Row{ID: id}, 0) })

	if items[5].Text != i18n.T("Menu.Context.Reset") || items[5].Disabled {
		t.Fatalf("items = %+v", items)
	}
}

func TestResettingFromTheJournalMovesTheBranch(t *testing.T) {
	a, target := forkedApp(t, false)
	views := captureResetViews(t)
	id := branchTip(t, target, "feature")

	readOnDispatcher(t, a, func() bool { a.journalMenu(journal.Row{ID: id}, 0)[5].OnClick(); return true })
	view := (*views)[0]
	readOnDispatcher(t, a, func() bool { view.Dialog().DefaultAction(); return true })

	waitForStatusText(t, a, i18n.Tf("Status.Reset", shortHash(id)))
	if got := branchTip(t, target, "main"); got != id {
		t.Fatalf("main = %s, want %s", got, id)
	}
}

func TestAHardResetFromTheJournalRestoresTheFiles(t *testing.T) {
	a, target := forkedApp(t, false)
	views := captureResetViews(t)
	id := branchTip(t, target, "feature")
	if err := writeFile(target, "f.txt", "dirty\n"); err != nil {
		t.Fatal(err)
	}

	readOnDispatcher(t, a, func() bool { a.openReset(id); return true })
	view := (*views)[0]
	readOnDispatcher(t, a, func() bool {
		view.SetKnown(reset.Known{Branch: "main", Commit: shortHash(id)}, reset.ModeHard)
		view.Dialog().DefaultAction()
		return true
	})

	waitForStatusText(t, a, i18n.Tf("Status.Reset", shortHash(id)))
	data, err := os.ReadFile(filepath.Join(target, "f.txt"))
	if err != nil || string(data) == "dirty\n" {
		t.Fatalf("f.txt = %q, %v", data, err)
	}
}

func TestCancellingTheResetDialogLeavesTheBranch(t *testing.T) {
	a, target := forkedApp(t, false)
	views := captureResetViews(t)
	before := branchTip(t, target, "main")

	readOnDispatcher(t, a, func() bool { a.openReset(branchTip(t, target, "feature")); return true })
	readOnDispatcher(t, a, func() bool { (*views)[0].Dialog().CancelAction(); return true })

	if got := branchTip(t, target, "main"); got != before {
		t.Fatalf("main = %s, want %s", got, before)
	}
}

func TestAFailedResetIsReported(t *testing.T) {
	a, target := forkedApp(t, false)
	prev := runReset
	runReset = func(context.Context, *gitrepo.Repository, string, ops.ResetOptions) (ops.ResetResult, error) {
		return ops.ResetResult{}, errors.New("no")
	}
	t.Cleanup(func() { runReset = prev })

	readOnDispatcher(t, a, func() bool { a.startReset(branchTip(t, target, "feature"), reset.ModeMixed); return true })

	waitForStatusText(t, a, i18n.Tf("Status.ResetFailed", errors.New("no")))
}

func TestResetNeedsARepositoryAndADialog(t *testing.T) {
	a := newTestApp(t)
	views := captureResetViews(t)
	id := hash.SumSHA1("commit", []byte("x"))

	items := a.resetItems(id)
	a.openReset(id)

	if len(*views) != 0 || !items[0].Disabled {
		t.Fatal("the dialog opened without a repository")
	}

	b, target := forkedApp(t, false)
	prevView := newResetView
	newResetView = func() (*reset.View, error) { return nil, errors.New("no dialog") }
	t.Cleanup(func() { newResetView = prevView })

	readOnDispatcher(t, b, func() bool { b.openReset(branchTip(t, target, "feature")); return true })
}
