package app

import (
	"path/filepath"
	"testing"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/ui/changes"
)

func filesRowNamesOnDispatcher(t *testing.T, a *App) []string {
	t.Helper()
	n := filesRowCountOnDispatcher(t, a)
	names := make([]string, n)
	for i := range n {
		names[i] = filesRowOnDispatcher(t, a, i).Name
	}
	return names
}

func TestOnFilesFilterChangedNarrowsTheGridToMatchingRows(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	buildWorkingRepoFixture(t, target)
	a := activatedWorkingApp(t, target)
	waitForWorkingRows(t, a, 4)

	a.onFilesFilterChanged("mod")

	if n := filesRowCountOnDispatcher(t, a); n != 1 {
		t.Fatalf("filtered rows = %d, want 1", n)
	}
	row := filesRowOnDispatcher(t, a, 0)
	if row.Name != "modified.txt" {
		t.Fatalf("filtered row = %+v, want modified.txt", row)
	}

	label := a.Widget("filesFilterCount").(*widget.Label)
	if got := label.Text(); got != "1 of 4" {
		t.Fatalf("counter text = %q, want %q", got, "1 of 4")
	}
}

func TestOnFilesFilterChangedBackToEmptyRestoresAllRowsAndTheTotalCounter(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	buildWorkingRepoFixture(t, target)
	a := activatedWorkingApp(t, target)
	waitForWorkingRows(t, a, 4)

	a.onFilesFilterChanged("mod")
	a.onFilesFilterChanged("")

	if n := filesRowCountOnDispatcher(t, a); n != 4 {
		t.Fatalf("rows after clearing the filter = %d, want 4", n)
	}
	label := a.Widget("filesFilterCount").(*widget.Label)
	if got := label.Text(); got != "4 files" {
		t.Fatalf("counter text = %q, want %q", got, "4 files")
	}
}

func TestApplyFilesFilterShowsTheTotalCounterOnStartup(t *testing.T) {
	a := newTestApp(t)
	label := a.Widget("filesFilterCount").(*widget.Label)
	if got := label.Text(); got != "0 files" {
		t.Fatalf("counter text = %q, want %q", got, "0 files")
	}
}

func TestSetFilesCounterTextIsANoOpWithoutALabel(t *testing.T) {
	a := newTestApp(t)
	a.filesFilterLabel = nil
	a.setFilesCounterText(1, 2, true)
}

func TestDisablingAFilesStatusButtonHidesMatchingRowsAndUpdatesTheCounter(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	buildWorkingRepoFixture(t, target)
	a := activatedWorkingApp(t, target)
	waitForWorkingRows(t, a, 4)

	a.toggleFilesStatusFilter(changes.FilterAdded)

	if got := filesRowNamesOnDispatcher(t, a); len(got) != 3 {
		t.Fatalf("rows after hiding added = %v, want 3 rows", got)
	}
	for _, name := range filesRowNamesOnDispatcher(t, a) {
		if name == "staged.txt" {
			t.Fatalf("staged.txt (added) must be hidden, rows = %v", filesRowNamesOnDispatcher(t, a))
		}
	}
	label := a.Widget("filesFilterCount").(*widget.Label)
	if got := label.Text(); got != "3 of 4" {
		t.Fatalf("counter text = %q, want %q", got, "3 of 4")
	}
}

func TestDisablingTheStagedButtonAlsoHidesAFullyStagedRow(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	buildWorkingRepoFixture(t, target)
	a := activatedWorkingApp(t, target)
	waitForWorkingRows(t, a, 4)

	a.toggleFilesStatusFilter(changes.FilterStaged)

	for _, name := range filesRowNamesOnDispatcher(t, a) {
		if name == "staged.txt" {
			t.Fatalf("staged.txt must be hidden when staged is disabled, rows = %v", filesRowNamesOnDispatcher(t, a))
		}
	}
}

func TestDisablingTheStagedButtonKeepsAConflictRowVisible(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	buildWorkingRepoFixture(t, target)
	a := activatedWorkingApp(t, target)
	waitForWorkingRows(t, a, 4)

	a.toggleFilesStatusFilter(changes.FilterStaged)

	found := false
	for _, name := range filesRowNamesOnDispatcher(t, a) {
		if name == "conflict.txt" {
			found = true
		}
	}
	if !found {
		t.Fatalf("conflict.txt must stay visible when staged is disabled, rows = %v", filesRowNamesOnDispatcher(t, a))
	}
}

func TestCombiningTextAndStatusButtonFiltersNarrowsTheGridFurther(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	buildWorkingRepoFixture(t, target)
	a := activatedWorkingApp(t, target)
	waitForWorkingRows(t, a, 4)

	// "e" alone matches staged/modified/untracked (not conflict); disabling
	// "added" additionally removes staged.txt, leaving the intersection.
	a.toggleFilesStatusFilter(changes.FilterAdded)
	a.onFilesFilterChanged("e")

	got := filesRowNamesOnDispatcher(t, a)
	want := []string{"modified.txt", "untracked.txt"}
	if len(got) != len(want) {
		t.Fatalf("rows = %v, want %v", got, want)
	}
	for _, name := range want {
		if !containsName(got, name) {
			t.Fatalf("rows = %v, want to contain %q", got, name)
		}
	}
}

func containsName(names []string, want string) bool {
	for _, n := range names {
		if n == want {
			return true
		}
	}
	return false
}

func TestReenablingAFilesStatusButtonRestoresItsRows(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	buildWorkingRepoFixture(t, target)
	a := activatedWorkingApp(t, target)
	waitForWorkingRows(t, a, 4)

	a.toggleFilesStatusFilter(changes.FilterUntracked)
	if n := filesRowCountOnDispatcher(t, a); n != 3 {
		t.Fatalf("rows after hiding untracked = %d, want 3", n)
	}
	a.toggleFilesStatusFilter(changes.FilterUntracked)
	if n := filesRowCountOnDispatcher(t, a); n != 4 {
		t.Fatalf("rows after re-enabling untracked = %d, want 4", n)
	}
	label := a.Widget("filesFilterCount").(*widget.Label)
	if got := label.Text(); got != "4 files" {
		t.Fatalf("counter text = %q, want %q", got, "4 files")
	}
}
