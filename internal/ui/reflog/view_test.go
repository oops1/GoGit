package reflog

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

func twoRecords() []Record {
	when := time.Unix(1700000000, 0).UTC()
	return []Record{
		{Selector: "main@{0}", Commit: id("second"), When: when, Who: "ann", Message: "commit: the later change"},
		{Selector: "main@{1}", Commit: id("first"), When: when.Add(-time.Hour), Who: "bob", Message: "commit (initial): the first change"},
	}
}

func (v *View) selectRow(t *testing.T, index int) {
	t.Helper()
	v.table.Grid.SetSelectedIndex(index)
	v.onSelected(datagrid.SelectionChangedEvent{SelectedItem: Row{Selector: v.records[index].Selector}})
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
			"refLabel": widget.NewLabel("", widget.CurrentTheme().LabelText),
			"records":  widget.NewDataGridWidget(),
			"hint":     widget.NewLabel("", widget.CurrentTheme().LabelText),
			"reset":    widget.NewButton(""),
			"close":    widget.NewButton(""),
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

func TestAnEmptyReflogSaysSo(t *testing.T) {
	v := newTestView(t)

	v.SetRecords("main", nil)

	if v.resetBtn.IsEnabled() || v.hintLabel.Text() != i18n.T("Dialog.Reflog.Hint.Empty") {
		t.Fatalf("hint = %q", v.hintLabel.Text())
	}
	if v.refLabel.Text() != i18n.Tf("Dialog.Reflog.Ref", "main", 0) {
		t.Fatalf("label = %q", v.refLabel.Text())
	}
}

func TestTheReflogAsksForARecordBeforeResetting(t *testing.T) {
	v := newTestView(t)
	reset := false
	v.OnReset = func(Record) { reset = true }

	v.SetRecords("main", twoRecords())
	v.resetBtn.OnClick()

	if reset || v.resetBtn.IsEnabled() || v.hintLabel.Text() != i18n.T("Dialog.Reflog.Hint.PickOne") {
		t.Fatalf("hint = %q", v.hintLabel.Text())
	}
	if got := v.Records(); len(got) != 2 || got[0].Selector != "main@{0}" {
		t.Fatalf("records = %+v", got)
	}
}

func TestPickingARecordOffersTheReset(t *testing.T) {
	v := newTestView(t)
	var chosen Record
	v.OnReset = func(record Record) { chosen = record }
	v.SetRecords("main", twoRecords())

	v.selectRow(t, 1)
	v.resetBtn.OnClick()

	if chosen.Selector != "main@{1}" {
		t.Fatalf("chosen = %+v", chosen)
	}
	want := i18n.Tf("Dialog.Reflog.Hint.Ready", "main@{1}", id("first").String()[:shortLength])
	if v.hintLabel.Text() != want {
		t.Fatalf("hint = %q, want %q", v.hintLabel.Text(), want)
	}
}

func TestLosingTheSelectionDisablesTheReset(t *testing.T) {
	v := newTestView(t)
	v.SetRecords("main", twoRecords())
	v.selectRow(t, 0)

	v.onSelected(datagrid.SelectionChangedEvent{SelectedItem: "not a row"})
	v.resetBtn.OnClick()

	if v.resetBtn.IsEnabled() || v.hintLabel.Text() != i18n.T("Dialog.Reflog.Hint.PickOne") {
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
	v.SetRecords("main", twoRecords())
	v.selectRow(t, 0)

	v.resetBtn.OnClick()
	v.closeBtn.OnClick()
}

func TestRestyleAcceptsBothThemes(t *testing.T) {
	v := newTestView(t)
	for _, theme := range []*widget.Theme{widget.Win11DarkTheme(), widget.Win11LightTheme()} {
		v.Restyle(theme)
	}
}
