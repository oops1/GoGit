package submoduleadd

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

func TestValidateFollowsTheRulesOfGitSubmoduleAdd(t *testing.T) {
	known := Known{Paths: []string{"libs/lib"}}
	for _, c := range []struct {
		model Model
		key   string
		args  []any
		ok    bool
	}{
		{Model{URL: "  "}, hintURLRequired, nil, false},
		{Model{URL: "lib.git"}, hintURLNotAbsolute, []any{"lib.git"}, false},
		{Model{URL: "https://.git"}, hintPathRequired, []any{"https://.git"}, false},
		{Model{URL: "../lib", Path: "../outside"}, hintPathInvalid, []any{"../outside"}, false},
		{Model{URL: "../lib", Path: "./"}, hintPathInvalid, []any{"."}, false},
		{Model{URL: "../lib", Path: "libs/lib/"}, hintPathTaken, []any{"libs/lib"}, false},
		{Model{URL: "https://host/group/tools.git"}, hintReady, []any{"https://host/group/tools.git", "tools"}, true},
		{Model{URL: "/srv/lib", Path: "deps/lib//"}, hintReady, []any{"/srv/lib", "deps/lib"}, true},
		{Model{URL: "git@host:lib.git", Path: "deps/lib", Branch: " stable "}, hintReadyBranch, []any{"git@host:lib.git", "deps/lib", "stable"}, true},
	} {
		got := Validate(c.model, known)
		if got.Key != c.key || !slices.Equal(got.Args, c.args) || got.OK != c.ok {
			t.Errorf("%+v: %+v", c.model, got)
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
		label := func() widget.Widget { return widget.NewLabel("", widget.CurrentTheme().LabelText) }
		return map[string]widget.Widget{
			"urlLabel":    label(),
			"url":         widget.NewTextInput(""),
			"pathLabel":   label(),
			"path":        widget.NewTextInput(""),
			"branchLabel": label(),
			"branch":      widget.NewTextInput(""),
			"hint":        label(),
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

func typeInto(input *widget.TextInput, text string) {
	input.SetText(text)
	input.OnChange(text)
}

func TestTheDialogStaysClosedUntilTheURLIsUsable(t *testing.T) {
	v := newTestView(t)
	v.SetKnown(Known{})
	confirmed := false
	v.OnOK = func(Model) { confirmed = true }

	v.okBtn.OnClick()
	typeInto(v.urlBox, "lib")
	handled := v.modal.HandleInputBinding(widget.KeyEnter, 0)

	if !handled || confirmed || v.okBtn.IsEnabled() {
		t.Fatalf("handled = %v, confirmed = %v", handled, confirmed)
	}
	if v.hintLabel.Text() != i18n.Tf(hintURLNotAbsolute, "lib") {
		t.Fatalf("hint = %q", v.hintLabel.Text())
	}
}

func TestFillingTheFieldsAddsTheSubmodule(t *testing.T) {
	v := newTestView(t)
	v.SetKnown(Known{Paths: []string{"other"}})
	var got Model
	v.OnOK = func(model Model) { got = model }

	typeInto(v.urlBox, " ../lib.git ")
	typeInto(v.pathBox, " deps/lib ")
	typeInto(v.branchBox, "main")
	v.okBtn.OnClick()

	if got != (Model{URL: "../lib.git", Path: "deps/lib", Branch: "main"}) {
		t.Fatalf("model = %+v", got)
	}
	if v.hintLabel.Text() != i18n.Tf(hintReadyBranch, "../lib.git", "deps/lib", "main") {
		t.Fatalf("hint = %q", v.hintLabel.Text())
	}
	got = Model{}
	if !v.modal.HandleInputBinding(widget.KeyEnter, 0) || got.URL != "../lib.git" {
		t.Fatalf("Enter confirmed %+v", got)
	}
	if v.Modal() != v.modal {
		t.Fatal("the dialog is shown through the modal that knows about Enter")
	}
}

func TestCancelAndCallbacksAreOptional(t *testing.T) {
	v := newTestView(t)
	typeInto(v.urlBox, "../lib")
	v.okBtn.OnClick()
	v.cancelBtn.OnClick()
	cancelled := false
	v.OnCancel = func() { cancelled = true }
	v.Dialog().CancelAction()
	if !cancelled {
		t.Fatal("the cancel callback did not run")
	}
	if v.modal.HandleInputBinding(widget.KeyA, 0) {
		t.Fatal("a plain key was taken for a confirmation")
	}
}

func TestRestyleAcceptsBothThemes(t *testing.T) {
	v := newTestView(t)
	for _, theme := range []*widget.Theme{widget.Win11DarkTheme(), widget.Win11LightTheme()} {
		v.Restyle(theme)
	}
}
