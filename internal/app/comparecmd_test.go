package app

import (
	"context"
	"errors"
	"testing"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/gitcore/diff"
	"github.com/oops1/gogit/internal/gitcore/ops"
	"github.com/oops1/gogit/internal/gitcore/refs"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/branches"
	"github.com/oops1/gogit/internal/ui/compareview"
)

func captureCompareViews(t *testing.T) *[]*compareview.View {
	t.Helper()
	views := &[]*compareview.View{}
	prev := newCompareRefsView
	newCompareRefsView = func() (*compareview.View, error) {
		view, err := prev()
		if err == nil {
			*views = append(*views, view)
		}
		return view, err
	}
	t.Cleanup(func() { newCompareRefsView = prev })
	return views
}

func waitForSummary(t *testing.T, a *App, view *compareview.View) compareview.Summary {
	t.Helper()
	waitForDialog(t, a, func() int {
		if view.Summary().Left == "" {
			return 0
		}
		return 1
	}, "the comparison")
	return readOnDispatcher(t, a, view.Summary)
}

func TestComparingTwoBranchesFillsTheDialog(t *testing.T) {
	a, _ := forkedApp(t, false)
	views := captureCompareViews(t)

	readOnDispatcher(t, a, func() bool { return a.Dispatch(CmdCompareRefs) })
	view := (*views)[0]

	summary := waitForSummary(t, a, view)
	if summary.Left != "main" || summary.Right != "feature" {
		t.Fatalf("summary = %+v", summary)
	}
	if summary.Ahead == 0 && summary.Behind == 0 {
		t.Fatalf("summary = %+v", summary)
	}
	readOnDispatcher(t, a, func() bool { view.Dialog().CancelAction(); return true })
}

func TestTheBranchMenuComparesWithTheCurrentBranch(t *testing.T) {
	a, _ := forkedApp(t, false)
	views := captureCompareViews(t)

	items := readOnDispatcher(t, a, func() []widget.MenuItem { return a.compareItems(refs.BranchName("feature")) })
	if len(items) != 1 || items[0].Text != i18n.T("Menu.Context.CompareWithCurrent") || items[0].Disabled {
		t.Fatalf("items = %+v", items)
	}
	readOnDispatcher(t, a, func() bool { items[0].OnClick(); return true })

	view := (*views)[0]
	summary := waitForSummary(t, a, view)
	if summary.Right != "feature" {
		t.Fatalf("summary = %+v", summary)
	}
	readOnDispatcher(t, a, func() bool { view.Dialog().CancelAction(); return true })
}

func TestTheCurrentBranchAndTagsHaveNoCompareEntry(t *testing.T) {
	a, _ := forkedApp(t, false)

	for _, ref := range []refs.Name{refs.BranchName("main"), refs.TagName("v1"), "refs/stash"} {
		if items := readOnDispatcher(t, a, func() []widget.MenuItem { return a.compareItems(ref) }); items != nil {
			t.Fatalf("%s: items = %+v", ref, items)
		}
	}
}

func TestChangingASideAsksForANewComparison(t *testing.T) {
	a, _ := forkedApp(t, false)
	views := captureCompareViews(t)
	readOnDispatcher(t, a, func() bool { a.openCompareRefs("feature"); return true })
	view := (*views)[0]
	waitForSummary(t, a, view)

	readOnDispatcher(t, a, func() bool {
		left, _ := view.Sides()
		view.OnCompare(left, "main")
		return true
	})

	waitForDialog(t, a, func() int {
		if view.Summary().Right != "main" {
			return 0
		}
		return 1
	}, "the second comparison")
	readOnDispatcher(t, a, func() bool { view.Dialog().CancelAction(); return true })
}

func TestAFailedComparisonIsReported(t *testing.T) {
	a, _ := forkedApp(t, false)
	captureCompareViews(t)
	prev := readCompare
	readCompare = func(context.Context, *gitrepo.Repository, string, string, ops.CompareOptions) (ops.CompareResult, error) {
		return ops.CompareResult{}, errors.New("no compare")
	}
	t.Cleanup(func() { readCompare = prev })

	readOnDispatcher(t, a, func() bool { a.openCompareRefs("feature"); return true })

	waitForStatusText(t, a, i18n.Tf("Status.CompareFailed", errors.New("no compare")))
}

func TestTheCompareDialogNeedsARepositoryAndADialog(t *testing.T) {
	a := newTestApp(t)
	views := captureCompareViews(t)

	a.openCompareRefs("")
	items := a.compareItems(refs.BranchName("feature"))

	if len(*views) != 0 || !items[0].Disabled {
		t.Fatal("the compare dialog opened without a repository")
	}

	b, _ := forkedApp(t, false)
	prev := newCompareRefsView
	newCompareRefsView = func() (*compareview.View, error) { return nil, errors.New("no dialog") }
	t.Cleanup(func() { newCompareRefsView = prev })
	readOnDispatcher(t, b, func() bool { b.openCompareRefs(""); return true })

	prevLoad := loadBranchSnapshot
	loadBranchSnapshot = func(*refs.Store) (branches.Snapshot, error) { return branches.Snapshot{}, errors.New("no refs") }
	t.Cleanup(func() { loadBranchSnapshot = prevLoad })
	readOnDispatcher(t, b, func() bool { b.openCompareRefs(""); return true })
}

func TestTheComparisonSummaryNamesWhatHappenedToEachFile(t *testing.T) {
	summary := compareSummary("main", "feature", ops.CompareResult{
		Ahead:  1,
		Behind: 2,
		Changes: []diff.File{
			{Status: diff.StatusAdded, NewPath: "added"},
			{Status: diff.StatusDeleted, OldPath: "gone", NewPath: "gone"},
			{Status: diff.StatusRenamed, OldPath: "f", NewPath: "moved"},
			{Status: diff.StatusModified, NewPath: "f"},
		},
	})

	if len(summary.Changes) != 4 || summary.Ahead != 1 || summary.Behind != 2 {
		t.Fatalf("summary = %+v", summary)
	}
	want := []string{i18n.T("Files.State.Added"), i18n.T("Files.State.Deleted"), i18n.T("Files.State.Renamed"), i18n.T("Files.State.Modified")}
	for i, change := range summary.Changes {
		if change.Status != want[i] {
			t.Fatalf("change %d = %+v, want %s", i, change, want[i])
		}
	}
}

func TestTheFirstOtherSideSkipsTheCurrentBranch(t *testing.T) {
	if got := firstOther([]string{"main", "feature"}, "main"); got != "feature" {
		t.Fatalf("other = %q", got)
	}
	if got := firstOther([]string{"main"}, "main"); got != "main" {
		t.Fatalf("other = %q", got)
	}
}
