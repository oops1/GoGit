package push

import (
	"errors"
	"testing"

	"github.com/oops1/headless-gui/v3/widget"

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
			"target":      widget.NewLabel("", widget.CurrentTheme().LabelText),
			"bypassHooks": widget.NewCheckBox(""),
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

func TestTheDialogNamesTheBranchAndTheRemote(t *testing.T) {
	v := newTestView(t)

	v.SetKnown(Known{Branch: "main", Remote: "origin"})

	if got := v.targetLabel.Text(); got != i18n.Tf("Dialog.Push.Target", "main", "origin") {
		t.Fatalf("label = %q", got)
	}
	if v.Dialog().Title != i18n.T("Dialog.Push.Title") {
		t.Fatalf("title = %q", v.Dialog().Title)
	}
}

func TestConfirmPassesTheBypassChoiceAndCancelReachesItsCallback(t *testing.T) {
	v := newTestView(t)
	var choices []bool
	cancelled := false
	v.OnOK = func(noVerify bool) { choices = append(choices, noVerify) }
	v.OnCancel = func() { cancelled = true }

	v.Dialog().DefaultAction()
	v.bypassCheck.SetChecked(true)
	v.okBtn.OnClick()
	v.Dialog().CancelAction()

	if len(choices) != 2 || choices[0] || !choices[1] || !cancelled {
		t.Fatalf("choices = %v, cancelled = %v", choices, cancelled)
	}
}

func TestCallbacksAreOptional(t *testing.T) {
	v := newTestView(t)
	v.okBtn.OnClick()
	v.cancelBtn.OnClick()
}

func TestRestyleAcceptsBothThemes(t *testing.T) {
	v := newTestView(t)
	for _, theme := range []*widget.Theme{widget.Win11DarkTheme(), widget.Win11LightTheme()} {
		v.Restyle(theme)
	}
}
