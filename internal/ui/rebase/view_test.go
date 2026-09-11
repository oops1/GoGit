package rebase

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

func TestValidateExplainsWhereTheBranchGoes(t *testing.T) {
	known := Known{Current: "topic", Candidates: []string{"main", "topic"}}
	for _, c := range []struct {
		onto string
		key  string
		args []any
		ok   bool
	}{
		{"  ", hintOntoRequired, nil, false},
		{"topic", hintOntoIsCurrent, []any{"topic"}, false},
		{"main", hintWillReplay, []any{"topic", "main"}, true},
	} {
		got := Validate(c.onto, known)
		if got.Key != c.key || got.OK != c.ok || !slices.Equal(got.Args, c.args) {
			t.Errorf("%q: %+v", c.onto, got)
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
			"ontoLabel": widget.NewLabel("", widget.CurrentTheme().LabelText),
			"onto":      widget.NewDropdown(),
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

func TestTheDialogPreselectsTheChosenBase(t *testing.T) {
	v := newTestView(t)

	v.SetKnown(Known{Current: "topic", Candidates: []string{"main", "origin/main"}}, "origin/main")

	if v.Onto() != "origin/main" || !v.okBtn.IsEnabled() || v.ontoLabel.Text() != i18n.Tf("Dialog.Rebase.Onto", "topic") {
		t.Fatalf("onto = %q, label = %q", v.Onto(), v.ontoLabel.Text())
	}
}

func TestWithoutCandidatesTheDialogStaysClosed(t *testing.T) {
	v := newTestView(t)
	confirmed := false
	v.OnOK = func(string) { confirmed = true }

	v.SetKnown(Known{Current: "topic"}, "")
	v.okBtn.OnClick()

	if v.okBtn.IsEnabled() || confirmed || v.hintLabel.Text() != i18n.T(hintOntoRequired) {
		t.Fatalf("hint = %q", v.hintLabel.Text())
	}
}

func TestPickingTheCurrentBranchIsRefused(t *testing.T) {
	v := newTestView(t)
	v.SetKnown(Known{Current: "topic", Candidates: []string{"main", "topic"}}, "main")

	v.ontoList.SetSelected(1)
	v.ontoList.OnChange(1, "topic")

	if v.okBtn.IsEnabled() || v.hintLabel.Text() != i18n.Tf(hintOntoIsCurrent, "topic") {
		t.Fatalf("hint = %q", v.hintLabel.Text())
	}
}

func TestConfirmAndCancelReachTheCallbacks(t *testing.T) {
	v := newTestView(t)
	v.SetKnown(Known{Current: "topic", Candidates: []string{"main"}}, "main")
	var onto string
	cancelled := false
	v.OnOK = func(target string) { onto = target }
	v.OnCancel = func() { cancelled = true }

	v.Dialog().DefaultAction()
	v.Dialog().CancelAction()

	if onto != "main" || !cancelled {
		t.Fatalf("onto = %q, cancelled = %v", onto, cancelled)
	}
}

func TestCallbacksAreOptional(t *testing.T) {
	v := newTestView(t)
	v.SetKnown(Known{Current: "topic", Candidates: []string{"main"}}, "main")

	v.okBtn.OnClick()
	v.cancelBtn.OnClick()
}

func TestRestyleAcceptsBothThemes(t *testing.T) {
	v := newTestView(t)
	for _, theme := range []*widget.Theme{widget.Win11DarkTheme(), widget.Win11LightTheme()} {
		v.Restyle(theme)
	}
}
