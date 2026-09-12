package rebasetodo

import (
	"errors"
	"slices"
	"testing"

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

func threeSteps() []Step {
	return []Step{
		{Action: ActionPick, Commit: id("one"), Subject: "first"},
		{Action: ActionPick, Commit: id("two"), Subject: "second"},
		{Action: ActionPick, Commit: id("three"), Subject: "third"},
	}
}

func TestValidateCountsWhatWouldBeReplayed(t *testing.T) {
	for _, c := range []struct {
		name  string
		steps []Step
		key   string
		ok    bool
	}{
		{"three picks", threeSteps(), hintReady, true},
		{"nothing at all", nil, hintNothingToDo, false},
		{"all dropped", []Step{{Action: ActionDrop, Commit: id("one")}}, hintNothingToDo, false},
		{"a squash on top", []Step{{Action: ActionSquash, Commit: id("one")}}, hintFoldsFirst, false},
		{"a fixup after a drop", []Step{{Action: ActionDrop, Commit: id("one")}, {Action: ActionFixup, Commit: id("two")}}, hintFoldsFirst, false},
		{"a squash after a pick", []Step{{Action: ActionPick, Commit: id("one")}, {Action: ActionSquash, Commit: id("two")}}, hintReady, true},
	} {
		got := Validate(c.steps)
		if got.Key != c.key || got.OK != c.ok {
			t.Errorf("%s: %+v", c.name, got)
		}
	}
}

func TestActionNamesComeFromTheStringTable(t *testing.T) {
	widget.ClearStrings()
	t.Cleanup(widget.ClearStrings)
	if _, err := i18n.Install(""); err != nil {
		t.Fatal(err)
	}
	i18n.Apply("en")

	if got := ActionName(ActionSquash); got != i18n.T("Dialog.RebaseTodo.Action.Squash") {
		t.Fatalf("squash = %q", got)
	}
	if got := ActionName("unheard of"); got != "unheard of" {
		t.Fatalf("unknown action = %q", got)
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
			"ontoLabel":   widget.NewLabel("", widget.CurrentTheme().LabelText),
			"steps":       widget.NewDataGridWidget(),
			"actionLabel": widget.NewLabel("", widget.CurrentTheme().LabelText),
			"action":      widget.NewDropdown(),
			"up":          widget.NewButton(""),
			"down":        widget.NewButton(""),
			"hint":        widget.NewLabel("", widget.CurrentTheme().LabelText),
			"ok":          widget.NewButton(""),
			"cancel":      widget.NewButton(""),
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

func TestTheDialogListsTheCommitsToReplay(t *testing.T) {
	v := newTestView(t)

	v.SetSteps("main", threeSteps())

	if v.ontoLabel.Text() != i18n.Tf("Dialog.RebaseTodo.Onto", "main", 3) {
		t.Fatalf("label = %q", v.ontoLabel.Text())
	}
	if !v.okBtn.IsEnabled() || v.upBtn.IsEnabled() || v.downBtn.IsEnabled() {
		t.Fatal("a fresh dialog has the wrong buttons enabled")
	}
	if got := v.Steps(); !slices.Equal(got, threeSteps()) {
		t.Fatalf("steps = %+v", got)
	}
}

func (v *View) selectRow(t *testing.T, index int) {
	t.Helper()
	v.table.Grid.SetSelectedIndex(index)
	row := Row{Action: ActionName(v.steps[index].Action), Commit: short(v.steps[index].Commit), Subject: v.steps[index].Subject}
	v.onSelected(datagrid.SelectionChangedEvent{SelectedItem: row})
}

func TestChoosingAnActionChangesTheSelectedStep(t *testing.T) {
	v := newTestView(t)
	v.SetSteps("main", threeSteps())

	v.selectRow(t, 1)
	v.actionList.SetSelected(slices.Index(Actions, ActionSquash))
	v.actionList.OnChange(0, "")

	if v.Steps()[1].Action != ActionSquash {
		t.Fatalf("steps = %+v", v.Steps())
	}
	if v.hintLabel.Text() != i18n.Tf(hintReady, 3) {
		t.Fatalf("hint = %q", v.hintLabel.Text())
	}
}

func TestChoosingTheSameActionTwiceChangesNothing(t *testing.T) {
	v := newTestView(t)
	v.SetSteps("main", threeSteps())
	v.selectRow(t, 0)

	v.actionList.SetSelected(slices.Index(Actions, ActionPick))
	v.actionList.OnChange(0, "")

	if got := v.Steps(); !slices.Equal(got, threeSteps()) {
		t.Fatalf("steps = %+v", got)
	}
}

func TestAnActionWithoutASelectionIsIgnored(t *testing.T) {
	v := newTestView(t)
	v.SetSteps("main", threeSteps())

	v.actionList.SetSelected(slices.Index(Actions, ActionDrop))
	v.actionList.OnChange(0, "")
	v.onSelected(datagrid.SelectionChangedEvent{SelectedItem: "not a row"})
	v.actionList.OnChange(0, "")

	if got := v.Steps(); !slices.Equal(got, threeSteps()) {
		t.Fatalf("steps = %+v", got)
	}
}

func TestTheStepsCanBeReordered(t *testing.T) {
	v := newTestView(t)
	v.SetSteps("main", threeSteps())

	v.selectRow(t, 2)
	v.upBtn.OnClick()
	v.upBtn.OnClick()

	if got := v.Steps(); got[0].Subject != "third" || got[1].Subject != "first" {
		t.Fatalf("steps = %+v", got)
	}
	if v.upBtn.IsEnabled() {
		t.Fatal("the first row can still move up")
	}
	v.downBtn.OnClick()
	if got := v.Steps(); got[0].Subject != "first" || got[1].Subject != "third" {
		t.Fatalf("steps = %+v", got)
	}
}

func TestReorderingWithoutASelectionIsIgnored(t *testing.T) {
	v := newTestView(t)
	v.SetSteps("main", threeSteps())

	v.move(-1)
	v.move(1)

	if got := v.Steps(); !slices.Equal(got, threeSteps()) {
		t.Fatalf("steps = %+v", got)
	}
}

func TestADialogWithNothingToReplayStaysClosed(t *testing.T) {
	v := newTestView(t)
	confirmed := false
	v.OnOK = func([]Step) { confirmed = true }

	v.SetSteps("main", []Step{{Action: ActionDrop, Commit: id("one"), Subject: "first"}})
	v.okBtn.OnClick()

	if confirmed || v.okBtn.IsEnabled() || v.hintLabel.Text() != i18n.T(hintNothingToDo) {
		t.Fatalf("hint = %q", v.hintLabel.Text())
	}
}

func TestConfirmAndCancelReachTheCallbacks(t *testing.T) {
	v := newTestView(t)
	v.SetSteps("main", threeSteps())
	var got []Step
	cancelled := false
	v.OnOK = func(steps []Step) { got = steps }
	v.OnCancel = func() { cancelled = true }

	v.okBtn.OnClick()
	v.Dialog().CancelAction()

	if !slices.Equal(got, threeSteps()) || !cancelled {
		t.Fatalf("steps = %+v, cancelled = %v", got, cancelled)
	}
}

func TestCallbacksAreOptional(t *testing.T) {
	v := newTestView(t)
	v.SetSteps("main", threeSteps())

	v.okBtn.OnClick()
	v.cancelBtn.OnClick()
}

func TestRestyleAcceptsBothThemes(t *testing.T) {
	v := newTestView(t)
	for _, theme := range []*widget.Theme{widget.Win11DarkTheme(), widget.Win11LightTheme()} {
		v.Restyle(theme)
	}
}
