package toolbar

import (
	"errors"
	"slices"
	"testing"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/i18n"
)

func newTestView(t *testing.T, model *Model) *View {
	t.Helper()
	widget.ClearStrings()
	t.Cleanup(widget.ClearStrings)
	if _, err := i18n.Install(""); err != nil {
		t.Fatal(err)
	}
	i18n.Apply("en")
	v, err := NewView(model)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func pick(list *widget.ListView, at int) {
	list.SetSelected(at)
	list.OnSelect(at, "")
}

func TestNewViewPropagatesLoadDialogError(t *testing.T) {
	widget.ClearStrings()
	t.Cleanup(widget.ClearStrings)
	wantErr := errors.New("boom")
	prev := loadDialog
	loadDialog = func(string, string) (*widget.Dialog, map[string]widget.Widget, error) {
		return nil, nil, wantErr
	}
	t.Cleanup(func() { loadDialog = prev })

	if _, err := NewView(newSampleModel()); !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
}

func TestNewViewReportsEveryMissingWidget(t *testing.T) {
	widget.ClearStrings()
	t.Cleanup(widget.ClearStrings)
	prev := loadDialog
	t.Cleanup(func() { loadDialog = prev })
	full := func() map[string]widget.Widget {
		return map[string]widget.Widget{
			"availableLabel": widget.NewLabel("", widget.CurrentTheme().LabelText),
			"selectedLabel":  widget.NewLabel("", widget.CurrentTheme().LabelText),
			"available":      widget.NewListView(),
			"selected":       widget.NewListView(),
			"captions":       widget.NewCheckBox(""),
			"add":            widget.NewButton(""),
			"remove":         widget.NewButton(""),
			"moveUp":         widget.NewButton(""),
			"moveDown":       widget.NewButton(""),
			"reset":          widget.NewButton(""),
			"ok":             widget.NewButton(""),
			"cancel":         widget.NewButton(""),
		}
	}
	for name := range full() {
		named := full()
		delete(named, name)
		loadDialog = func(string, string) (*widget.Dialog, map[string]widget.Widget, error) {
			return widget.NewDialog("", 100, 100), named, nil
		}
		if _, err := NewView(newSampleModel()); !errors.Is(err, ErrWidgetMissing) {
			t.Fatalf("without %s: err = %v", name, err)
		}
	}
}

func TestTheDialogOpensOnTheConfiguredRow(t *testing.T) {
	v := newTestView(t, newSampleModel())
	if got := v.selected.Items(); !slices.Equal(got, []string{"Pull", "——", "Commit"}) {
		t.Fatalf("selected list = %v", got)
	}
	if got := v.available.Items(); !slices.Equal(got, []string{"——", "<->", "Push", "Merge"}) {
		t.Fatalf("available list = %v", got)
	}
	if !v.captionsBox.IsChecked() {
		t.Fatal("the captions flag must follow the model")
	}
	if v.Model() == nil || v.Dialog() == nil {
		t.Fatal("the view must expose its model and its dialog")
	}
}

func TestAddPutsTheChosenEntryAfterTheSelectedOne(t *testing.T) {
	v := newTestView(t, newSampleModel())
	pick(v.selected, 0)
	pick(v.available, 2)
	v.addBtn.OnClick()
	if got := v.Result().Items; !slices.Equal(got, []string{"remote.pull", "remote.push", SeparatorID, "local.commit"}) {
		t.Fatalf("row = %v", got)
	}
	if got := v.selected.Selected(); got != 1 {
		t.Fatalf("the new item must stay selected, got %d", got)
	}
}

func TestAddAppendsWhenNothingIsSelectedOnTheRight(t *testing.T) {
	v := newTestView(t, newSampleModel())
	pick(v.selected, -1)
	pick(v.available, 1)
	v.addBtn.OnClick()
	if got := v.Result().Items; !slices.Equal(got, []string{"remote.pull", SeparatorID, "local.commit", StretchID}) {
		t.Fatalf("row = %v", got)
	}
}

func TestAddDoesNothingWithoutAChoiceOnTheLeft(t *testing.T) {
	v := newTestView(t, newSampleModel())
	pick(v.available, -1)
	v.addBtn.OnClick()
	if got := v.Result().Items; !slices.Equal(got, sampleDefaults()) {
		t.Fatalf("row = %v", got)
	}
	if v.addBtn.IsEnabled() {
		t.Fatal("add must be disabled without a choice")
	}
}

func TestRemoveTakesTheSelectedItemOut(t *testing.T) {
	v := newTestView(t, newSampleModel())
	pick(v.selected, 1)
	v.removeBtn.OnClick()
	if got := v.Result().Items; !slices.Equal(got, []string{"remote.pull", "local.commit"}) {
		t.Fatalf("row = %v", got)
	}
	pick(v.selected, -1)
	v.removeBtn.OnClick()
	if got := v.Result().Items; !slices.Equal(got, []string{"remote.pull", "local.commit"}) {
		t.Fatalf("row after a removal with no choice = %v", got)
	}
}

func TestUpAndDownReorderTheRow(t *testing.T) {
	v := newTestView(t, newSampleModel())
	pick(v.selected, 2)
	v.upBtn.OnClick()
	if got := v.Result().Items; !slices.Equal(got, []string{"remote.pull", "local.commit", SeparatorID}) {
		t.Fatalf("after up = %v", got)
	}
	if got := v.selected.Selected(); got != 1 {
		t.Fatalf("the moved item must stay selected, got %d", got)
	}
	v.downBtn.OnClick()
	if got := v.Result().Items; !slices.Equal(got, []string{"remote.pull", SeparatorID, "local.commit"}) {
		t.Fatalf("after down = %v", got)
	}
}

func TestTheMoveButtonsStopAtTheEnds(t *testing.T) {
	v := newTestView(t, newSampleModel())
	pick(v.selected, 0)
	if v.upBtn.IsEnabled() {
		t.Fatal("the first item cannot move up")
	}
	v.upBtn.OnClick()
	pick(v.selected, 2)
	if v.downBtn.IsEnabled() {
		t.Fatal("the last item cannot move down")
	}
	v.downBtn.OnClick()
	if got := v.Result().Items; !slices.Equal(got, sampleDefaults()) {
		t.Fatalf("row = %v", got)
	}
}

func TestResetBringsBackTheDefaultRowAndTurnsItselfOff(t *testing.T) {
	v := newTestView(t, newSampleModel())
	if v.resetBtn.IsEnabled() {
		t.Fatal("the default row has nothing to reset")
	}
	pick(v.selected, 0)
	v.removeBtn.OnClick()
	if !v.resetBtn.IsEnabled() {
		t.Fatal("an edited row can be reset")
	}
	v.resetBtn.OnClick()
	if got := v.Result().Items; !slices.Equal(got, sampleDefaults()) {
		t.Fatalf("row = %v", got)
	}
}

func TestARowWithoutCommandsCannotBeConfirmed(t *testing.T) {
	v := newTestView(t, NewModel(sampleCatalog(), []string{SeparatorID}, sampleDefaults(), false))
	confirmed := 0
	v.OnOK = func(Result) { confirmed++ }
	if v.okBtn.IsEnabled() {
		t.Fatal("a row of spacers alone cannot be confirmed")
	}
	v.okBtn.OnClick()
	v.dlg.DefaultAction()
	if confirmed != 0 {
		t.Fatalf("confirmed %d times", confirmed)
	}
}

func TestConfirmAndCancelReportTheChoice(t *testing.T) {
	v := newTestView(t, newSampleModel())
	var got Result
	confirmed, cancelled := 0, 0
	v.OnOK = func(r Result) { got = r; confirmed++ }
	v.OnCancel = func() { cancelled++ }

	v.captionsBox.SetChecked(false)
	v.okBtn.OnClick()
	if confirmed != 1 || got.Captions || !slices.Equal(got.Items, sampleDefaults()) {
		t.Fatalf("result = %+v after %d confirmations", got, confirmed)
	}
	v.cancelBtn.OnClick()
	v.dlg.CancelAction()
	if cancelled != 2 {
		t.Fatalf("cancelled %d times", cancelled)
	}
}

func TestTheDialogWithoutHandlersStaysQuiet(t *testing.T) {
	v := newTestView(t, newSampleModel())
	v.okBtn.OnClick()
	v.cancelBtn.OnClick()
}

func TestSelectingInEitherListRefreshesTheButtons(t *testing.T) {
	v := newTestView(t, newSampleModel())
	pick(v.available, 0)
	if !v.addBtn.IsEnabled() {
		t.Fatal("a choice on the left enables add")
	}
	pick(v.selected, -1)
	if v.removeBtn.IsEnabled() {
		t.Fatal("no choice on the right disables remove")
	}
}

func TestRestyleRepaintsEveryWidget(t *testing.T) {
	v := newTestView(t, newSampleModel())
	for _, theme := range []*widget.Theme{widget.Win11LightTheme(), widget.Win11DarkTheme()} {
		v.Restyle(theme)
		if v.okBtn.Background != theme.Accent {
			t.Fatalf("the primary button keeps %v", v.okBtn.Background)
		}
		if v.availableLabel.TextColor != theme.LabelText {
			t.Fatalf("the label keeps %v", v.availableLabel.TextColor)
		}
	}
}

func TestAnEmptyListSelectsNothing(t *testing.T) {
	v := newTestView(t, NewModel([]Entry{{ID: "local.commit", Label: "Commit"}}, []string{"local.commit"}, nil, true))
	if got := v.available.Selected(); got != -1 {
		t.Fatalf("an empty available list selected %d", got)
	}
}
