package app

import (
	"path/filepath"
	"testing"

	"github.com/oops1/headless-gui/v3/widget"
)

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
