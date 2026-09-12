package reset

import (
	"errors"
	"slices"
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

func TestValidateDescribesWhatEachModeKeeps(t *testing.T) {
	known := Known{Branch: "main", Commit: "1234567"}
	for _, c := range []struct {
		mode Mode
		key  string
	}{
		{ModeSoft, "Dialog.Reset.Hint.Soft"},
		{ModeMixed, "Dialog.Reset.Hint.Mixed"},
		{ModeHard, "Dialog.Reset.Hint.Hard"},
		{Mode(9), "Dialog.Reset.Hint.Mixed"},
		{Mode(-1), "Dialog.Reset.Hint.Mixed"},
	} {
		got := Validate(c.mode, known)
		if got.Key != c.key || !slices.Equal(got.Args, []any{"main", "1234567"}) {
			t.Errorf("mode %d: %+v", c.mode, got)
		}
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
			"modeLabel": widget.NewLabel("", widget.CurrentTheme().LabelText),
			"mode":      widget.NewDropdown(),
			"hint":      widget.NewLabel("", widget.CurrentTheme().LabelText),
			"ok":        widget.NewButton(""),
			"cancel":    widget.NewButton(""),
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

func TestTheDialogOpensOnTheChosenMode(t *testing.T) {
	v := newTestView(t)

	v.SetKnown(Known{Branch: "main", Commit: "1234567"}, ModeHard)

	if v.Mode() != ModeHard {
		t.Fatalf("mode = %d", v.Mode())
	}
	if v.modeLabel.Text() != i18n.Tf("Dialog.Reset.Target", "main", "1234567") {
		t.Fatalf("label = %q", v.modeLabel.Text())
	}
	if v.hintLabel.Text() != i18n.Tf("Dialog.Reset.Hint.Hard", "main", "1234567") {
		t.Fatalf("hint = %q", v.hintLabel.Text())
	}
}

func TestChangingTheModeChangesTheHint(t *testing.T) {
	v := newTestView(t)
	v.SetKnown(Known{Branch: "main", Commit: "1234567"}, ModeMixed)

	v.modeList.SetSelected(int(ModeSoft))
	v.modeList.OnChange(int(ModeSoft), "")

	if v.hintLabel.Text() != i18n.Tf("Dialog.Reset.Hint.Soft", "main", "1234567") {
		t.Fatalf("hint = %q", v.hintLabel.Text())
	}
}

func TestTheModeNamesComeFromTheStringTable(t *testing.T) {
	v := newTestView(t)

	if got := ModeNames(); !slices.Equal(got, []string{
		i18n.T("Dialog.Reset.Mode.Soft"), i18n.T("Dialog.Reset.Mode.Mixed"), i18n.T("Dialog.Reset.Mode.Hard"),
	}) {
		t.Fatalf("names = %v", got)
	}
	if v.Mode() != ModeMixed {
		t.Fatalf("a fresh dialog starts at mode %d", v.Mode())
	}
}

func TestConfirmAndCancelReachTheCallbacks(t *testing.T) {
	v := newTestView(t)
	v.SetKnown(Known{Branch: "main", Commit: "1234567"}, ModeHard)
	mode := ModeSoft
	cancelled := false
	v.OnOK = func(chosen Mode) { mode = chosen }
	v.OnCancel = func() { cancelled = true }

	v.Dialog().DefaultAction()
	v.Dialog().CancelAction()

	if mode != ModeHard || !cancelled {
		t.Fatalf("mode = %d, cancelled = %v", mode, cancelled)
	}
}

func TestCallbacksAreOptional(t *testing.T) {
	v := newTestView(t)
	v.SetKnown(Known{Branch: "main", Commit: "1234567"}, ModeMixed)

	v.okBtn.OnClick()
	v.cancelBtn.OnClick()
}

func TestRestyleAcceptsBothThemes(t *testing.T) {
	v := newTestView(t)
	for _, theme := range []*widget.Theme{widget.Win11DarkTheme(), widget.Win11LightTheme()} {
		v.Restyle(theme)
	}
}
