package blameview

import (
	"errors"
	"strconv"
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

func twoLines() []Line {
	when := time.Unix(1700000000, 0).UTC()
	return []Line{
		{Commit: id("one"), Author: "ann", When: when, Summary: "the first change", Path: "f", Number: 1, Text: "one\n"},
		{Commit: id("two"), Author: "bob", When: when, Summary: "the later change", Path: "older", Number: 2, Text: "two\r\n"},
	}
}

func (v *View) selectRow(t *testing.T, index int) {
	t.Helper()
	v.table.Grid.SetSelectedIndex(index)
	number := v.lines[index].Number
	v.onSelected(datagrid.SelectionChangedEvent{SelectedItem: Row{Number: strconv.Itoa(number)}})
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
			"lines":     widget.NewDataGridWidget(),
			"hint":      widget.NewLabel("", widget.CurrentTheme().LabelText),
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

func TestAnEmptyFileSaysSo(t *testing.T) {
	v := newTestView(t)

	v.SetLines("f", nil)

	if v.hintLabel.Text() != i18n.T("Dialog.Blame.Hint.Empty") {
		t.Fatalf("hint = %q", v.hintLabel.Text())
	}
	if v.pathLabel.Text() != i18n.Tf("Dialog.Blame.Path", "f", 0) {
		t.Fatalf("label = %q", v.pathLabel.Text())
	}
}

func TestTheLinesAreListedWithoutTheirLineEndings(t *testing.T) {
	v := newTestView(t)

	v.SetLines("f", twoLines())

	if got := v.Lines(); len(got) != 2 || got[1].Text != "two\r\n" {
		t.Fatalf("lines = %+v", got)
	}
	if v.hintLabel.Text() != i18n.T("Dialog.Blame.Hint.PickOne") {
		t.Fatalf("hint = %q", v.hintLabel.Text())
	}
	if _, chosen := v.Selected(); chosen {
		t.Fatal("a fresh dialog has a line selected")
	}
}

func TestPickingALineTellsWhereItCameFrom(t *testing.T) {
	v := newTestView(t)
	v.SetLines("f", twoLines())

	v.selectRow(t, 1)

	line, chosen := v.Selected()
	if !chosen || line.Number != 2 {
		t.Fatalf("selected = %+v, %v", line, chosen)
	}
	if v.hintLabel.Text() != i18n.Tf("Dialog.Blame.Hint.Line", "the later change", "older") {
		t.Fatalf("hint = %q", v.hintLabel.Text())
	}
}

func TestLosingTheSelectionGoesBackToTheInvitation(t *testing.T) {
	v := newTestView(t)
	v.SetLines("f", twoLines())
	v.selectRow(t, 0)

	v.onSelected(datagrid.SelectionChangedEvent{SelectedItem: "not a row"})

	if _, chosen := v.Selected(); chosen {
		t.Fatal("the selection survived")
	}
	if v.hintLabel.Text() != i18n.T("Dialog.Blame.Hint.PickOne") {
		t.Fatalf("hint = %q", v.hintLabel.Text())
	}
}

func TestCloseReachesTheCallback(t *testing.T) {
	v := newTestView(t)
	closed := false
	v.OnClose = func() { closed = true }

	v.Dialog().CancelAction()
	v.closeBtn.OnClick()

	if !closed {
		t.Fatal("the close callback did not run")
	}
}

func TestTheCloseCallbackIsOptional(t *testing.T) {
	v := newTestView(t)

	v.closeBtn.OnClick()
}

func TestRestyleAcceptsBothThemes(t *testing.T) {
	v := newTestView(t)
	for _, theme := range []*widget.Theme{widget.Win11DarkTheme(), widget.Win11LightTheme()} {
		v.Restyle(theme)
	}
}
