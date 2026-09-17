package investigate

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oops1/headless-gui/v3/widget"
	"github.com/oops1/headless-gui/v3/widget/datagrid"

	"github.com/oops1/gogit/internal/gitcore/diff"
	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/linelog"
	"github.com/oops1/gogit/internal/i18n"
)

func newTestView(t *testing.T) *View {
	t.Helper()
	widget.ClearStrings()
	t.Cleanup(widget.ClearStrings)
	if _, err := i18n.Install(""); err != nil {
		t.Fatal(err)
	}
	i18n.Apply("en")
	v, err := NewView()
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func editEntry() Entry {
	return Entry{
		Commit:  hash.SumSHA1("blob", []byte("edit")),
		Author:  "ann",
		When:    time.Unix(1700000000, 0).UTC(),
		Subject: "edit the lines",
		Files: []linelog.File{{
			OldPath: "old.go",
			NewPath: "new.go",
			Hunks: []linelog.Hunk{{OldStart: 2, OldLines: 2, NewStart: 2, NewLines: 2, Lines: []diff.Line{
				{Kind: diff.KindContext, Text: "same"},
				{Kind: diff.KindDel, Text: "before"},
				{Kind: diff.KindAdd, Text: "after"},
			}}},
		}},
	}
}

func createEntry() Entry {
	return Entry{
		Commit:  hash.SumSHA1("blob", []byte("create")),
		Author:  "bob",
		When:    time.Unix(1690000000, 0).UTC(),
		Subject: "create the file",
		Files: []linelog.File{{OldPath: "old.go", NewPath: "old.go", Created: true, Hunks: []linelog.Hunk{{
			NewStart: 1, NewLines: 1, Lines: []diff.Line{{Kind: diff.KindAdd, Text: "born"}},
		}}}},
	}
}

func TestNewViewPropagatesLoadDialogError(t *testing.T) {
	widget.ClearStrings()
	t.Cleanup(widget.ClearStrings)
	wantErr := errors.New("boom")
	prev := loadDialog
	loadDialog = func(string, string) (*widget.Dialog, map[string]widget.Widget, error) { return nil, nil, wantErr }
	t.Cleanup(func() { loadDialog = prev })

	if _, err := NewView(); !errors.Is(err, wantErr) {
		t.Fatalf("err = %v", err)
	}
}

func TestNewViewReportsEveryMissingWidget(t *testing.T) {
	widget.ClearStrings()
	t.Cleanup(widget.ClearStrings)
	prev := loadDialog
	t.Cleanup(func() { loadDialog = prev })
	full := func() map[string]widget.Widget {
		return map[string]widget.Widget{
			"pathLabel": widget.NewLabel("", widget.CurrentTheme().LabelText),
			"commits":   widget.NewDataGridWidget(),
			"diff":      widget.NewDiffView("", ""),
			"hint":      widget.NewLabel("", widget.CurrentTheme().LabelText),
			"cancel":    widget.NewButton(""),
			"close":     widget.NewButton(""),
		}
	}
	for name := range full() {
		named := full()
		delete(named, name)
		loadDialog = func(string, string) (*widget.Dialog, map[string]widget.Widget, error) {
			return widget.NewDialog("", 100, 100), named, nil
		}
		if _, err := NewView(); !errors.Is(err, ErrWidgetMissing) {
			t.Fatalf("without %s: err = %v", name, err)
		}
	}
}

func TestAStartedSearchReportsProgressAndCanBeCancelled(t *testing.T) {
	v := newTestView(t)
	cancelled := 0
	v.OnCancel = func() { cancelled++ }
	v.SetTarget("f.go", "lines 3–5", "HEAD")

	v.Start()
	v.Progress(10, 40)

	if !v.Running() || !v.cancelBtn.IsEnabled() {
		t.Fatal("the search is not running")
	}
	if v.Hint() != i18n.Tf("Dialog.Investigate.Hint.Running", 0, 10, 40) {
		t.Fatalf("hint = %q", v.Hint())
	}
	if v.pathLabel.Text() != i18n.Tf("Dialog.Investigate.Path", "f.go", "lines 3–5", "HEAD") {
		t.Fatalf("label = %q", v.pathLabel.Text())
	}
	v.cancelBtn.OnClick()
	v.Finish(context.Canceled)

	if cancelled != 1 || v.Running() || v.cancelBtn.IsEnabled() {
		t.Fatalf("cancelled = %d, running = %v", cancelled, v.Running())
	}
	if v.Hint() != i18n.Tf("Dialog.Investigate.Hint.Cancelled", 0) {
		t.Fatalf("hint = %q", v.Hint())
	}
}

func TestTheFirstCommitIsShownAsSoonAsItArrives(t *testing.T) {
	v := newTestView(t)
	v.Start()

	v.Append(editEntry())
	v.Append(createEntry())
	v.Finish(nil)

	selected, ok := v.Selected()
	if !ok || selected.Subject != "edit the lines" || len(v.Entries()) != 2 {
		t.Fatalf("selected = %+v, %v", selected, ok)
	}
	if got := v.Diff().Text(widget.DiffLeft); got != "@@ -2,2 +2,2 @@\nsame\nbefore\n" {
		t.Fatalf("left = %q", got)
	}
	if got := v.Diff().Text(widget.DiffRight); got != "@@ -2,2 +2,2 @@\nsame\nafter\n" {
		t.Fatalf("right = %q", got)
	}
	if v.Hint() != i18n.Tf("Dialog.Investigate.Hint.Found", 2) {
		t.Fatalf("hint = %q", v.Hint())
	}
	if v.rows.Count() != 2 || v.rows.Get(1).(Row).Commit != createEntry().Commit.String()[:shortLength] {
		t.Fatalf("rows = %v", v.rows.Items())
	}
}

func TestPickingAnotherCommitShowsItsLines(t *testing.T) {
	v := newTestView(t)
	v.Start()
	v.Append(editEntry())
	v.Append(createEntry())

	v.onSelected(datagrid.SelectionChangedEvent{SelectedIndex: 1})

	if got := v.Diff().Text(widget.DiffRight); got != "@@ -0,0 +1,1 @@\nborn\n" {
		t.Fatalf("right = %q", got)
	}
	v.onSelected(datagrid.SelectionChangedEvent{SelectedIndex: -1})
	if _, ok := v.Selected(); ok || v.Diff().Text(widget.DiffRight) != "" {
		t.Fatal("the selection survived")
	}
	v.onSelected(datagrid.SelectionChangedEvent{SelectedIndex: 9})
	if _, ok := v.Selected(); ok {
		t.Fatal("a row past the end was selected")
	}
}

func TestTheHintExplainsEmptyFailedAndMergeResults(t *testing.T) {
	v := newTestView(t)
	v.Start()
	v.Finish(nil)
	if v.Hint() != i18n.T("Dialog.Investigate.Hint.Empty") {
		t.Fatalf("empty hint = %q", v.Hint())
	}

	failure := errors.New("broken")
	v.Start()
	v.Finish(failure)
	if v.Hint() != i18n.Tf("Dialog.Investigate.Hint.Failed", failure) {
		t.Fatalf("failed hint = %q", v.Hint())
	}

	v.Start()
	v.Append(Entry{Commit: hash.SumSHA1("blob", []byte("merge")), Subject: "merge", Merge: true})
	v.Finish(nil)
	if v.Hint() != i18n.T("Dialog.Investigate.Hint.Merge") {
		t.Fatalf("merge hint = %q", v.Hint())
	}
}

func TestSidesJoinFilesAndLeaveCreatedFilesUntitled(t *testing.T) {
	entry := editEntry()
	entry.Files = append(entry.Files, createEntry().Files...)

	left, right := Sides(entry)

	if left.Title != "old.go" || right.Title != "new.go, old.go" {
		t.Fatalf("titles = %q, %q", left.Title, right.Title)
	}
	if right.Text != "@@ -2,2 +2,2 @@\nsame\nafter\n@@ -0,0 +1,1 @@\nborn\n" {
		t.Fatalf("right = %q", right.Text)
	}
}

func TestCloseAndCancelCallbacksAreOptional(t *testing.T) {
	v := newTestView(t)
	v.cancelBtn.OnClick()
	v.closeBtn.OnClick()

	closed := 0
	v.OnClose = func() { closed++ }
	v.Dialog().CancelAction()
	v.closeBtn.OnClick()
	if closed != 2 {
		t.Fatalf("closed = %d", closed)
	}
}

func TestTheDialogFitsTheWindowAndRestyles(t *testing.T) {
	v := newTestView(t)

	v.FitWithin(500, 300)
	if b := v.Dialog().Bounds(); b.Dx() != dialogMinWidth || b.Dy() != dialogMinHeight {
		t.Fatalf("small window bounds = %v", b)
	}
	v.FitWithin(4000, 3000)
	if b := v.Dialog().Bounds(); b.Dx() < dialogMinWidth {
		t.Fatalf("large window bounds = %v", b)
	}
	for _, theme := range []*widget.Theme{widget.Win11DarkTheme(), widget.Win11LightTheme()} {
		v.Restyle(theme)
	}
}
