package app

import (
	"context"
	"errors"
	"testing"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/ops"
	"github.com/oops1/gogit/internal/gitcore/refs"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/branches"
	"github.com/oops1/gogit/internal/ui/journal"
	"github.com/oops1/gogit/internal/ui/tag"
)

func captureTagViews(t *testing.T) *[]*tag.View {
	t.Helper()
	views := &[]*tag.View{}
	prev := newTagView
	newTagView = func() (*tag.View, error) {
		view, err := prev()
		if err == nil {
			*views = append(*views, view)
		}
		return view, err
	}
	t.Cleanup(func() { newTagView = prev })
	return views
}

func tagsOf(t *testing.T, target string) []string {
	t.Helper()
	r, err := gitrepo.Open(target, gitrepo.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	known, err := ops.Tags(t.Context(), r)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(known))
	for _, one := range known {
		names = append(names, one.Name)
	}
	return names
}

func TestTheJournalMenuOffersToTagTheCommit(t *testing.T) {
	a, target := forkedApp(t, false)
	id := branchTip(t, target, "feature")

	items := readOnDispatcher(t, a, func() []widget.MenuItem { return a.journalMenu(journal.Row{ID: id}, 0) })

	last := items[len(items)-1]
	if last.Text != i18n.T("Menu.Context.CreateTag") || last.Disabled {
		t.Fatalf("items = %+v", items)
	}
	views := captureTagViews(t)
	readOnDispatcher(t, a, func() bool { last.OnClick(); return true })
	if len(*views) != 1 {
		t.Fatal("the menu item did not open the dialog")
	}
	readOnDispatcher(t, a, func() bool { (*views)[0].Dialog().CancelAction(); return true })
}

func TestTaggingFromTheJournalCreatesAnAnnotatedTag(t *testing.T) {
	a, target := forkedApp(t, false)
	views := captureTagViews(t)
	id := branchTip(t, target, "feature")

	readOnDispatcher(t, a, func() bool { a.openTag(id); return true })
	view := (*views)[0]
	readOnDispatcher(t, a, func() bool {
		view.OnOK(tag.Model{Name: "v1", Message: "first release"})
		return true
	})

	waitForStatusText(t, a, i18n.Tf("Status.TagCreated", "v1"))
	if names := tagsOf(t, target); len(names) != 1 || names[0] != "v1" {
		t.Fatalf("tags = %v", names)
	}
}

func TestTheTagDialogKnowsTheNamesAlreadyTaken(t *testing.T) {
	a, target := forkedApp(t, false)
	views := captureTagViews(t)
	id := branchTip(t, target, "feature")
	readOnDispatcher(t, a, func() bool { a.createTag(id, tag.Model{Name: "v1"}); return true })
	waitForStatusText(t, a, i18n.Tf("Status.TagCreated", "v1"))

	readOnDispatcher(t, a, func() bool { a.openTag(id); return true })

	if taken := readOnDispatcher(t, a, func() []string { return a.tagNames() }); len(taken) != 1 || taken[0] != "v1" {
		t.Fatalf("taken = %v", taken)
	}
	if hint := tag.Validate(tag.Model{Name: "v1"}, tag.Known{Taken: readOnDispatcher(t, a, func() []string { return a.tagNames() })}); hint.OK {
		t.Fatal("the dialog would allow a duplicate")
	}
	readOnDispatcher(t, a, func() bool { (*views)[0].Dialog().CancelAction(); return true })
}

func TestTheBranchMenuDeletesATag(t *testing.T) {
	a, target := forkedApp(t, false)
	id := branchTip(t, target, "feature")
	readOnDispatcher(t, a, func() bool { a.createTag(id, tag.Model{Name: "v1", Message: "tagged"}); return true })
	waitForStatusText(t, a, i18n.Tf("Status.TagCreated", "v1"))

	items := readOnDispatcher(t, a, func() []widget.MenuItem { return a.branchMenu(refs.TagName("v1")) })
	readOnDispatcher(t, a, func() bool { items[len(items)-1].OnClick(); return true })

	waitForStatusText(t, a, i18n.Tf("Status.TagDeleted", "v1"))
	if names := tagsOf(t, target); len(names) != 0 {
		t.Fatalf("tags = %v", names)
	}
}

func hasMenuItem(items []widget.MenuItem, text string) bool {
	for _, item := range items {
		if item.Text == text {
			return true
		}
	}
	return false
}

func TestTheBranchMenuKeepsItsMergeEntryForBranches(t *testing.T) {
	a, _ := forkedApp(t, false)

	items := readOnDispatcher(t, a, func() []widget.MenuItem { return a.branchMenu(refs.BranchName("feature")) })
	mine := readOnDispatcher(t, a, func() []widget.MenuItem { return a.branchMenu(refs.BranchName("main")) })

	if items[0].Text != i18n.T("Menu.Context.MergeIntoCurrent") || !hasMenuItem(items, i18n.T("Menu.Context.Reflog")) {
		t.Fatalf("items = %+v", items)
	}
	if hasMenuItem(mine, i18n.T("Menu.Context.MergeIntoCurrent")) || !hasMenuItem(mine, i18n.T("Menu.Context.Reflog")) {
		t.Fatalf("mine = %+v", mine)
	}
}

func TestAFailedTagIsReported(t *testing.T) {
	a, target := forkedApp(t, false)
	prevCreate, prevDelete := runCreateTag, runDeleteTag
	runCreateTag = func(context.Context, *gitrepo.Repository, string, string, ops.CreateTagOptions) (ops.TagResult, error) {
		return ops.TagResult{}, errors.New("no")
	}
	runDeleteTag = func(context.Context, *gitrepo.Repository, string) error { return errors.New("no") }
	t.Cleanup(func() { runCreateTag, runDeleteTag = prevCreate, prevDelete })

	readOnDispatcher(t, a, func() bool { a.createTag(branchTip(t, target, "feature"), tag.Model{Name: "v1"}); return true })
	waitForStatusText(t, a, i18n.Tf("Status.TagFailed", errors.New("no")))

	readOnDispatcher(t, a, func() bool { a.deleteTag("v1"); return true })
	waitForStatusText(t, a, i18n.Tf("Status.TagDeleteFailed", errors.New("no")))
}

func TestTagCommandsNeedARepository(t *testing.T) {
	a := newTestApp(t)
	views := captureTagViews(t)
	id := hash.SumSHA1("commit", []byte("x"))

	items := a.tagItems(id)
	a.openTag(id)

	if len(*views) != 0 || !items[0].Disabled || a.tagNames() != nil {
		t.Fatal("the tag dialog opened without a repository")
	}
}

func TestTagDialogAndSnapshotFailuresAreLogged(t *testing.T) {
	a, target := forkedApp(t, false)
	prev := newTagView
	newTagView = func() (*tag.View, error) { return nil, errors.New("no dialog") }
	t.Cleanup(func() { newTagView = prev })
	readOnDispatcher(t, a, func() bool { a.openTag(branchTip(t, target, "feature")); return true })

	prevLoad := loadBranchSnapshot
	loadBranchSnapshot = func(*refs.Store) (branches.Snapshot, error) { return branches.Snapshot{}, errors.New("no refs") }
	t.Cleanup(func() { loadBranchSnapshot = prevLoad })

	if names := readOnDispatcher(t, a, func() []string { return a.tagNames() }); names != nil {
		t.Fatalf("names = %v", names)
	}
}
