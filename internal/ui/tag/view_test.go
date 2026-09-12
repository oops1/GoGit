package tag

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

func TestValidateJudgesTheNameAndTheMessage(t *testing.T) {
	known := Known{Commit: "1234567", Taken: []string{"v1"}}
	for _, c := range []struct {
		model Model
		key   string
		args  []any
		ok    bool
	}{
		{Model{Name: "  "}, hintNameRequired, nil, false},
		{Model{Name: "bad..name"}, hintNameInvalid, []any{"bad..name"}, false},
		{Model{Name: "-v2"}, hintNameInvalid, []any{"-v2"}, false},
		{Model{Name: "v2."}, hintNameInvalid, []any{"v2."}, false},
		{Model{Name: "v2.lock"}, hintNameInvalid, []any{"v2.lock"}, false},
		{Model{Name: "v 2"}, hintNameInvalid, []any{"v 2"}, false},
		{Model{Name: "v1"}, hintNameTaken, []any{"v1"}, true},
		{Model{Name: "v2"}, hintLightweight, []any{"v2", "1234567"}, true},
		{Model{Name: "v2", Message: "  "}, hintLightweight, []any{"v2", "1234567"}, true},
		{Model{Name: "v2", Message: "release"}, hintAnnotated, []any{"v2", "1234567"}, true},
	} {
		got := Validate(c.model, known)
		if got.Key != c.key || !slices.Equal(got.Args, c.args) {
			t.Errorf("%+v: %+v", c.model, got)
		}
	}
}

func TestATakenNameIsRefused(t *testing.T) {
	got := Validate(Model{Name: "v1"}, Known{Taken: []string{"v1"}})

	if got.OK {
		t.Fatalf("%+v", got)
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
			"nameLabel":    widget.NewLabel("", widget.CurrentTheme().LabelText),
			"name":         widget.NewTextInput(""),
			"messageLabel": widget.NewLabel("", widget.CurrentTheme().LabelText),
			"message":      widget.NewTextBox(""),
			"hint":         widget.NewLabel("", widget.CurrentTheme().LabelText),
			"ok":           widget.NewButton(""),
			"cancel":       widget.NewButton(""),
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

func TestTheDialogNamesTheCommitAndStaysClosedWithoutAName(t *testing.T) {
	v := newTestView(t)
	confirmed := false
	v.OnOK = func(Model) { confirmed = true }

	v.SetKnown(Known{Commit: "1234567"})
	v.okBtn.OnClick()

	if v.okBtn.IsEnabled() || confirmed {
		t.Fatal("a nameless tag was accepted")
	}
	if v.nameLabel.Text() != i18n.Tf("Dialog.Tag.Name", "1234567") {
		t.Fatalf("label = %q", v.nameLabel.Text())
	}
	if v.hintLabel.Text() != i18n.T(hintNameRequired) {
		t.Fatalf("hint = %q", v.hintLabel.Text())
	}
}

func TestTypingANameAndAMessageMakesAnAnnotatedTag(t *testing.T) {
	v := newTestView(t)
	v.SetKnown(Known{Commit: "1234567"})
	var got Model
	v.OnOK = func(model Model) { got = model }

	v.nameBox.SetText(" v2 ")
	v.nameBox.OnChange(" v2 ")
	v.messageBox.SetText("second release")
	v.messageBox.OnChange("second release")
	v.okBtn.OnClick()

	if got.Name != "v2" || got.Message != "second release" {
		t.Fatalf("model = %+v", got)
	}
	if v.hintLabel.Text() != i18n.Tf(hintAnnotated, "v2", "1234567") {
		t.Fatalf("hint = %q", v.hintLabel.Text())
	}
}

func TestCancelReachesTheCallback(t *testing.T) {
	v := newTestView(t)
	cancelled := false
	v.OnCancel = func() { cancelled = true }

	v.Dialog().CancelAction()

	if !cancelled {
		t.Fatal("the cancel callback did not run")
	}
}

func TestCallbacksAreOptional(t *testing.T) {
	v := newTestView(t)
	v.SetKnown(Known{Commit: "1234567"})

	v.nameBox.SetText("v2")
	v.nameBox.OnChange("v2")
	v.okBtn.OnClick()
	v.cancelBtn.OnClick()
}

func TestRestyleAcceptsBothThemes(t *testing.T) {
	v := newTestView(t)
	for _, theme := range []*widget.Theme{widget.Win11DarkTheme(), widget.Win11LightTheme()} {
		v.Restyle(theme)
	}
}
