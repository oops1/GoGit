package bisect

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

func TestValidateExplainsWhichRevisionsTheSearchNeeds(t *testing.T) {
	for _, c := range []struct {
		choice Choice
		key    string
		args   []any
		ok     bool
	}{
		{Choice{}, hintBadRequired, nil, false},
		{Choice{Bad: "main"}, hintGoodRequired, nil, false},
		{Choice{Bad: "main", Good: "main"}, hintSameRevision, []any{"main"}, false},
		{Choice{Bad: "main", Good: "v1.0"}, hintWillSearch, []any{"v1.0", "main"}, true},
	} {
		got := Validate(c.choice)
		if got.Key != c.key || got.OK != c.ok || !slices.Equal(got.Args, c.args) {
			t.Errorf("%+v: %+v", c.choice, got)
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
			"header":    widget.NewLabel("", widget.CurrentTheme().LabelText),
			"badLabel":  widget.NewLabel("", widget.CurrentTheme().LabelText),
			"goodLabel": widget.NewLabel("", widget.CurrentTheme().LabelText),
			"hint":      widget.NewLabel("", widget.CurrentTheme().LabelText),
			"bad":       widget.NewDropdown(),
			"good":      widget.NewDropdown(),
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

func TestTheDialogPreselectsTheCurrentBranchAsBad(t *testing.T) {
	v := newTestView(t)

	v.SetKnown(Known{Bad: "main", Good: "v1.0", Revs: []string{"main", "v1.0"}})

	if got := v.Choice(); got.Bad != "main" || got.Good != "v1.0" {
		t.Fatalf("choice = %+v", got)
	}
	if !v.okBtn.IsEnabled() || v.hintLabel.Text() != i18n.Tf(hintWillSearch, "v1.0", "main") {
		t.Fatalf("hint = %q", v.hintLabel.Text())
	}
}

func TestTheSameRevisionOnBothSidesIsRefused(t *testing.T) {
	v := newTestView(t)
	v.SetKnown(Known{Bad: "main", Good: "v1.0", Revs: []string{"main", "v1.0"}})
	confirmed := false
	v.OnOK = func(Choice) { confirmed = true }

	v.goodList.SetSelected(0)
	v.goodList.OnChange(0, "main")
	v.okBtn.OnClick()

	if v.okBtn.IsEnabled() || confirmed || v.hintLabel.Text() != i18n.Tf(hintSameRevision, "main") {
		t.Fatalf("hint = %q, confirmed = %v", v.hintLabel.Text(), confirmed)
	}
}

func TestAnEmptyRepositoryLeavesTheDialogDisabled(t *testing.T) {
	v := newTestView(t)

	v.SetKnown(Known{})

	if v.okBtn.IsEnabled() || v.hintLabel.Text() != i18n.T(hintBadRequired) {
		t.Fatalf("hint = %q", v.hintLabel.Text())
	}
}

func TestChangingTheBadRevisionRefreshesTheHint(t *testing.T) {
	v := newTestView(t)
	v.SetKnown(Known{Bad: "main", Good: "v1.0", Revs: []string{"main", "v1.0"}})

	v.badList.SetSelected(1)
	v.badList.OnChange(1, "v1.0")

	if v.okBtn.IsEnabled() || v.hintLabel.Text() != i18n.Tf(hintSameRevision, "v1.0") {
		t.Fatalf("hint = %q", v.hintLabel.Text())
	}
}

func TestConfirmAndCancelReachTheCallbacks(t *testing.T) {
	v := newTestView(t)
	v.SetKnown(Known{Bad: "main", Good: "v1.0", Revs: []string{"main", "v1.0"}})
	var chosen Choice
	cancelled := false
	v.OnOK = func(c Choice) { chosen = c }
	v.OnCancel = func() { cancelled = true }

	v.Dialog().DefaultAction()
	v.Dialog().CancelAction()

	if chosen.Bad != "main" || chosen.Good != "v1.0" || !cancelled {
		t.Fatalf("chosen = %+v, cancelled = %v", chosen, cancelled)
	}
}

func TestCallbacksAreOptional(t *testing.T) {
	v := newTestView(t)
	v.SetKnown(Known{Bad: "main", Good: "v1.0", Revs: []string{"main", "v1.0"}})

	v.okBtn.OnClick()
	v.cancelBtn.OnClick()
}

func TestRestyleAcceptsBothThemes(t *testing.T) {
	v := newTestView(t)
	for _, theme := range []*widget.Theme{widget.Win11DarkTheme(), widget.Win11LightTheme()} {
		v.Restyle(theme)
	}
}
