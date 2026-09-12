package filehistory

import (
	"errors"
	"testing"
	"time"

	"github.com/oops1/headless-gui/v3/widget"
	"github.com/oops1/headless-gui/v3/widget/datagrid"

	"github.com/oops1/gogit/internal/gitcore/hash"
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

func id(text string) hash.ObjectID { return hash.SumSHA1("blob", []byte(text)) }

func twoEntries() []Entry {
	when := time.Unix(1700000000, 0).UTC()
	return []Entry{
		{Commit: id("second"), Author: "ann", When: when, Subject: "the later change", Path: "f", Old: "f"},
		{Commit: id("first"), Author: "bob", When: when.Add(-time.Hour), Subject: "the move", Path: "f", Old: "older"},
	}
}

func (v *View) selectRow(t *testing.T, index int) {
	t.Helper()
	entry := v.entries[index]
	v.table.Grid.SetSelectedIndex(index)
	v.onSelected(datagrid.SelectionChangedEvent{SelectedItem: Row{Commit: entry.Commit.String()[:shortLength]}})
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
			"hint":      widget.NewLabel("", widget.CurrentTheme().LabelText),
			"blame":     widget.NewButton(""),
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

func TestAnEmptyHistorySaysSo(t *testing.T) {
	v := newTestView(t)

	v.SetEntries("f", nil)

	if v.blameBtn.IsEnabled() || v.hintLabel.Text() != i18n.T("Dialog.FileHistory.Hint.Empty") {
		t.Fatalf("hint = %q", v.hintLabel.Text())
	}
	if v.pathLabel.Text() != i18n.Tf("Dialog.FileHistory.Path", "f", 0) {
		t.Fatalf("label = %q", v.pathLabel.Text())
	}
}

func TestTheHistoryAsksForACommitBeforeBlaming(t *testing.T) {
	v := newTestView(t)
	blamed := false
	v.OnBlame = func(Entry) { blamed = true }

	v.SetEntries("f", twoEntries())
	v.blameBtn.OnClick()

	if blamed || v.blameBtn.IsEnabled() || v.hintLabel.Text() != i18n.T("Dialog.FileHistory.Hint.PickOne") {
		t.Fatalf("hint = %q", v.hintLabel.Text())
	}
	if got := v.Entries(); len(got) != 2 || got[0].Subject != "the later change" {
		t.Fatalf("entries = %+v", got)
	}
}

func TestPickingACommitEnablesBlame(t *testing.T) {
	v := newTestView(t)
	var blamed Entry
	v.OnBlame = func(entry Entry) { blamed = entry }
	v.SetEntries("f", twoEntries())

	v.selectRow(t, 0)
	v.blameBtn.OnClick()

	if blamed.Commit != twoEntries()[0].Commit || v.hintLabel.Text() != i18n.T("Dialog.FileHistory.Hint.Ready") {
		t.Fatalf("blamed = %+v, hint = %q", blamed, v.hintLabel.Text())
	}
}

func TestPickingARenamingCommitTellsTheOldName(t *testing.T) {
	v := newTestView(t)
	v.SetEntries("f", twoEntries())

	v.selectRow(t, 1)

	if v.hintLabel.Text() != i18n.Tf("Dialog.FileHistory.Hint.Renamed", "older") {
		t.Fatalf("hint = %q", v.hintLabel.Text())
	}
}

func TestLosingTheSelectionDisablesBlame(t *testing.T) {
	v := newTestView(t)
	v.SetEntries("f", twoEntries())
	v.selectRow(t, 0)

	v.onSelected(datagrid.SelectionChangedEvent{SelectedItem: "not a row"})
	v.blameBtn.OnClick()

	if v.blameBtn.IsEnabled() || v.hintLabel.Text() != i18n.T("Dialog.FileHistory.Hint.PickOne") {
		t.Fatalf("hint = %q", v.hintLabel.Text())
	}
}

func TestCloseReachesTheCallback(t *testing.T) {
	v := newTestView(t)
	closed := false
	v.OnClose = func() { closed = true }

	v.Dialog().CancelAction()

	if !closed {
		t.Fatal("the close callback did not run")
	}
}

func TestCallbacksAreOptional(t *testing.T) {
	v := newTestView(t)
	v.SetEntries("f", twoEntries())
	v.selectRow(t, 0)

	v.blameBtn.OnClick()
	v.closeBtn.OnClick()
}

func TestRestyleAcceptsBothThemes(t *testing.T) {
	v := newTestView(t)
	for _, theme := range []*widget.Theme{widget.Win11DarkTheme(), widget.Win11LightTheme()} {
		v.Restyle(theme)
	}
}
