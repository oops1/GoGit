package switchchanges

import (
	"errors"
	"testing"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/config"
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

func click(w interface{ OnMouseButton(widget.MouseEvent) bool }) {
	w.OnMouseButton(widget.MouseEvent{Button: widget.MouseLeft, Pressed: true})
	w.OnMouseButton(widget.MouseEvent{Button: widget.MouseLeft, Pressed: false})
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

	if _, err := NewView(); !errors.Is(err, wantErr) {
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
			"message":       widget.NewLabel("", widget.CurrentTheme().LabelText),
			"paths":         widget.NewLabel("", widget.CurrentTheme().LabelText),
			"modeStash":     widget.NewRadioButton("", "g"),
			"modeMerge":     widget.NewRadioButton("", "g"),
			"modeOverwrite": widget.NewRadioButton("", "g"),
			"remember":      widget.NewCheckBox(""),
			"rememberHint":  widget.NewLabel("", widget.CurrentTheme().LabelText),
			"ok":            widget.NewButton(""),
			"cancel":        widget.NewButton(""),
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

func TestTheDialogNamesTheTargetAndTheBlockedFiles(t *testing.T) {
	v := newTestView(t)
	v.SetBlocked("develop", []string{"a.txt", "b.txt"})
	if got := v.messageLabel.Text(); got != i18n.Tf("Dialog.SwitchChanges.Message", "develop") {
		t.Fatalf("message = %q", got)
	}
	if got := v.pathsLabel.Text(); got != "a.txt, b.txt" {
		t.Fatalf("paths = %q", got)
	}
}

func TestTheChoiceFollowsTheSelectedOption(t *testing.T) {
	v := newTestView(t)
	if got := v.Choice(); got != (Choice{Mode: config.SwitchChangesStash}) {
		t.Fatalf("default choice = %+v", got)
	}
	click(v.modeMerge)
	click(v.rememberBox)
	if got := v.Choice(); got != (Choice{Mode: config.SwitchChangesMerge, Remember: true}) {
		t.Fatalf("merge choice = %+v", got)
	}
	click(v.modeOverwrite)
	if got := v.Choice(); got.Mode != config.SwitchChangesOverwrite {
		t.Fatalf("overwrite choice = %+v", got)
	}
}

func TestConfirmAndCancelReachTheCallbacks(t *testing.T) {
	v := newTestView(t)
	var chosen []Choice
	cancelled := 0
	v.OnChoose = func(c Choice) { chosen = append(chosen, c) }
	v.OnCancel = func() { cancelled++ }
	v.Dialog().DefaultAction()
	v.Dialog().CancelAction()
	if len(chosen) != 1 || chosen[0].Mode != config.SwitchChangesStash || cancelled != 1 {
		t.Fatalf("chosen = %+v, cancelled = %d", chosen, cancelled)
	}
}

func TestCallbacksAreOptional(t *testing.T) {
	v := newTestView(t)
	v.Dialog().DefaultAction()
	v.Dialog().CancelAction()
}

func TestRestyleAcceptsBothThemes(t *testing.T) {
	v := newTestView(t)
	v.Restyle(widget.Win11DarkTheme())
	v.Restyle(widget.Win11LightTheme())
}
