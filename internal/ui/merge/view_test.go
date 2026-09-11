package merge

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

func clickRadio(rb *widget.RadioButton) {
	rb.OnMouseButton(widget.MouseEvent{Button: widget.MouseLeft, Pressed: true})
	rb.OnMouseButton(widget.MouseEvent{Button: widget.MouseLeft, Pressed: false})
}

func toggle(box *widget.CheckBox) {
	box.OnMouseButton(widget.MouseEvent{Button: widget.MouseLeft, Pressed: true})
	box.OnMouseButton(widget.MouseEvent{Button: widget.MouseLeft, Pressed: false})
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
			"intoLabel":           widget.NewLabel("", widget.CurrentTheme().LabelText),
			"source":              widget.NewDropdown(),
			"modeFastForward":     widget.NewRadioButton("", "g"),
			"modeMergeCommit":     widget.NewRadioButton("", "g"),
			"modeFastForwardOnly": widget.NewRadioButton("", "g"),
			"modeSquash":          widget.NewRadioButton("", "g"),
			"noCommit":            widget.NewCheckBox(""),
			"hint":                widget.NewLabel("", widget.CurrentTheme().LabelText),
			"ok":                  widget.NewButton(""),
			"cancel":              widget.NewButton(""),
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

func TestTheDialogPreselectsTheRequestedSource(t *testing.T) {
	v := newTestView(t)

	v.SetKnown(Known{Current: "main", Candidates: []string{"feature", "origin/feature", "v1"}}, "origin/feature")

	if got := v.Request(); got.Source != "origin/feature" || got.Mode != ModeFastForward {
		t.Fatalf("request = %+v", got)
	}
	if v.intoLabel.Text() != i18n.Tf("Dialog.Merge.Into", "main") || !v.okBtn.IsEnabled() {
		t.Fatalf("label = %q", v.intoLabel.Text())
	}
}

func TestAnUnknownSelectionFallsBackToTheFirstCandidate(t *testing.T) {
	v := newTestView(t)

	v.SetKnown(Known{Current: "main", Candidates: []string{"feature", "v1"}}, "gone")

	if got := v.Request().Source; got != "feature" {
		t.Fatalf("source = %q", got)
	}
}

func TestNoCandidatesKeepTheMergeButtonOff(t *testing.T) {
	v := newTestView(t)
	confirmed := false
	v.OnOK = func(Request) { confirmed = true }

	v.SetKnown(Known{Current: "main"}, "")
	v.okBtn.OnClick()

	if v.okBtn.IsEnabled() || confirmed || v.hintLabel.Text() != i18n.T(hintSourceRequired) {
		t.Fatalf("hint = %q", v.hintLabel.Text())
	}
}

func TestModesShapeTheRequest(t *testing.T) {
	v := newTestView(t)
	v.SetKnown(Known{Current: "main", Candidates: []string{"feature"}}, "feature")
	toggle(v.noCommitBox)
	for _, c := range []struct {
		radio    *widget.RadioButton
		mode     Mode
		noCommit bool
	}{
		{v.modeMergeCommit, ModeMergeCommit, true},
		{v.modeFastForwardOnly, ModeFastForwardOnly, false},
		{v.modeSquash, ModeSquash, false},
		{v.modeFastForward, ModeFastForward, true},
	} {
		clickRadio(c.radio)
		got := v.Request()
		if got.Mode != c.mode || got.NoCommit != c.noCommit || v.noCommitBox.IsEnabled() != c.mode.AllowsNoCommit() {
			t.Fatalf("mode %d: request = %+v", c.mode, got)
		}
	}
}

func TestPickingASourceRefreshesTheHint(t *testing.T) {
	v := newTestView(t)
	v.SetKnown(Known{Current: "main", Candidates: []string{"feature", "main"}}, "feature")

	v.sourceList.SetSelected(1)
	v.sourceList.OnChange(1, "main")

	if v.okBtn.IsEnabled() || v.hintLabel.Text() != i18n.Tf(hintSourceIsCurrent, "main") {
		t.Fatalf("hint = %q", v.hintLabel.Text())
	}
}

func TestConfirmAndCancelReachTheCallbacks(t *testing.T) {
	v := newTestView(t)
	v.SetKnown(Known{Current: "main", Candidates: []string{"feature"}}, "feature")
	var got Request
	cancelled := false
	v.OnOK = func(req Request) { got = req }
	v.OnCancel = func() { cancelled = true }

	v.Dialog().DefaultAction()
	v.Dialog().CancelAction()

	if got.Source != "feature" || !cancelled {
		t.Fatalf("request = %+v, cancelled = %v", got, cancelled)
	}
}

func TestCallbacksAreOptional(t *testing.T) {
	v := newTestView(t)
	v.SetKnown(Known{Current: "main", Candidates: []string{"feature"}}, "feature")

	v.okBtn.OnClick()
	v.cancelBtn.OnClick()
}

func TestRestyleAcceptsBothThemes(t *testing.T) {
	v := newTestView(t)
	for _, theme := range []*widget.Theme{widget.Win11DarkTheme(), widget.Win11LightTheme()} {
		v.Restyle(theme)
	}
}
