package sparse

import (
	"errors"
	"slices"
	"testing"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/i18n"
)

func newTestView(t *testing.T, model Model) *View {
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

func TestValidateJudgesTheModel(t *testing.T) {
	for _, c := range []struct {
		name  string
		model Model
		key   string
		args  []any
		ok    bool
	}{
		{"disabled", Model{}, hintDisabled, nil, true},
		{"noPatterns", Model{Enabled: true, Cone: true}, hintEmpty, nil, true},
		{"cone", Model{Enabled: true, Cone: true, Patterns: []string{"a", "b"}}, hintCone, []any{2}, true},
		{"globInCone", Model{Enabled: true, Cone: true, Patterns: []string{"a", "*.md"}}, hintGlobInCone, []any{"*.md"}, false},
		{"patterns", Model{Enabled: true, Patterns: []string{"*.md"}}, hintPatterns, []any{1}, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := Validate(c.model)
			if got.Key != c.key || !slices.Equal(got.Args, c.args) || got.OK != c.ok {
				t.Fatalf("Validate = %+v, want %s %v ok=%v", got, c.key, c.args, c.ok)
			}
		})
	}
}

func TestParsePatternsDropsBlankLines(t *testing.T) {
	got := ParsePatterns("  a \r\n\n b/c \n")
	if want := []string{"a", "b/c"}; !slices.Equal(got, want) {
		t.Fatalf("ParsePatterns = %v, want %v", got, want)
	}
}

func TestNewViewPropagatesLoadDialogError(t *testing.T) {
	widget.ClearStrings()
	t.Cleanup(widget.ClearStrings)
	wantErr := errors.New("boom")
	prev := loadDialog
	loadDialog = func(string, string) (*widget.Dialog, map[string]widget.Widget, error) { return nil, nil, wantErr }
	t.Cleanup(func() { loadDialog = prev })

	if _, err := NewView(Model{}); !errors.Is(err, wantErr) {
		t.Fatalf("NewView returned %v, want %v", err, wantErr)
	}
}

func TestNewViewReportsEveryMissingWidget(t *testing.T) {
	widget.ClearStrings()
	t.Cleanup(widget.ClearStrings)
	prev := loadDialog
	t.Cleanup(func() { loadDialog = prev })
	full := func() map[string]widget.Widget {
		return map[string]widget.Widget{
			"enabled":       widget.NewCheckBox(""),
			"cone":          widget.NewCheckBox(""),
			"patternsLabel": widget.NewLabel("", widget.CurrentTheme().LabelText),
			"patterns":      widget.NewTextBox(""),
			"hint":          widget.NewLabel("", widget.CurrentTheme().LabelText),
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
		if _, err := NewView(Model{}); !errors.Is(err, ErrWidgetMissing) {
			t.Fatalf("without %s: NewView returned %v", name, err)
		}
	}
}

func TestTheDialogOpensOnTheCurrentPatterns(t *testing.T) {
	v := newTestView(t, Model{Enabled: true, Cone: true, Patterns: []string{"a", "c/deep"}})
	if !v.enabledCheck.IsChecked() || !v.coneCheck.IsChecked() {
		t.Fatal("the dialog did not open on the recorded settings")
	}
	if got := v.patternsBox.GetText(); got != "a\nc/deep" {
		t.Fatalf("patterns = %q", got)
	}
	if got := v.patternsLabel.Text(); got != i18n.T(labelCone) {
		t.Fatalf("label = %q, want the cone wording", got)
	}
	if got := v.hintLabel.Text(); got != i18n.Tf(hintCone, 2) {
		t.Fatalf("hint = %q", got)
	}
}

func TestSwitchingOffConeModeRenamesTheListAndAllowsPatterns(t *testing.T) {
	v := newTestView(t, Model{Enabled: true, Cone: true, Patterns: []string{"*.md"}})
	if v.okBtn.IsEnabled() {
		t.Fatal("a glob was accepted in cone mode")
	}
	v.coneCheck.SetChecked(false)
	v.coneCheck.OnChange(false)
	if !v.okBtn.IsEnabled() {
		t.Fatal("the glob is still refused outside cone mode")
	}
	if got := v.patternsLabel.Text(); got != i18n.T(labelPatterns) {
		t.Fatalf("label = %q, want the pattern wording", got)
	}
}

func TestDisablingLocksTheListAndConfirmsTheFullWorkingTree(t *testing.T) {
	v := newTestView(t, Model{Enabled: true, Cone: true, Patterns: []string{"a"}})
	var got Model
	v.OnOK = func(model Model) { got = model }

	v.enabledCheck.SetChecked(false)
	v.enabledCheck.OnChange(false)
	if v.coneCheck.IsEnabled() || v.patternsBox.IsEnabled() {
		t.Fatal("the list stayed editable although the sparse checkout is switched off")
	}
	v.okBtn.OnClick()
	if got.Enabled {
		t.Fatalf("model = %+v, want the sparse checkout switched off", got)
	}
}

func TestTypingPatternsReachesTheCallback(t *testing.T) {
	v := newTestView(t, Model{Enabled: true, Cone: true})
	var got Model
	v.OnOK = func(model Model) { got = model }

	v.patternsBox.SetText("a\n b/c \n")
	v.patternsBox.OnChange(v.patternsBox.GetText())
	v.okBtn.OnClick()

	if !got.Enabled || !got.Cone || !slices.Equal(got.Patterns, []string{"a", "b/c"}) {
		t.Fatalf("model = %+v", got)
	}
}

func TestRefusedPatternsKeepTheDialogOpen(t *testing.T) {
	v := newTestView(t, Model{Enabled: true, Cone: true})
	confirmed := false
	v.OnOK = func(Model) { confirmed = true }

	v.patternsBox.SetText("a/*")
	v.patternsBox.OnChange("a/*")
	v.confirm()

	if confirmed {
		t.Fatal("a glob was accepted in cone mode")
	}
	if got := v.hintLabel.Text(); got != i18n.Tf(hintGlobInCone, "a/*") {
		t.Fatalf("hint = %q", got)
	}
}

func TestCancelReachesTheCallback(t *testing.T) {
	v := newTestView(t, Model{})
	cancelled := false
	v.OnCancel = func() { cancelled = true }

	v.Dialog().CancelAction()

	if !cancelled {
		t.Fatal("the cancel callback did not run")
	}
}

func TestCallbacksAreOptional(t *testing.T) {
	v := newTestView(t, Model{Enabled: true, Cone: true, Patterns: []string{"a"}})
	v.okBtn.OnClick()
	v.cancelBtn.OnClick()
}

func TestRestyleKeepsTheDialogUsable(t *testing.T) {
	v := newTestView(t, Model{Enabled: true, Cone: true, Patterns: []string{"a"}})
	v.Restyle(widget.CurrentTheme())
	if !v.okBtn.IsEnabled() {
		t.Fatal("restyling disabled the confirm button")
	}
}
