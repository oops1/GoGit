package switchbranch

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

func known() Known {
	return Known{Current: "main", Local: []string{"main", "topic"}, Remote: []string{"origin/main", "origin/fresh"}}
}

func TestValidateJudgesEveryChoice(t *testing.T) {
	for _, c := range []struct {
		name   string
		choice Choice
		key    string
		ok     bool
	}{
		{"nothing chosen", Choice{}, hintSourceRequired, false},
		{"the current branch", Choice{Source: "main"}, hintAlreadyThere, false},
		{"another local branch", Choice{Source: "topic"}, hintWillSwitch, true},
		{"a remote without a name", Choice{Source: "origin/fresh"}, hintNameRequired, false},
		{"a remote with a bad name", Choice{Source: "origin/fresh", Name: "bad..name"}, hintNameInvalid, false},
		{"a remote with a name in use", Choice{Source: "origin/fresh", Name: "topic"}, hintNameTaken, false},
		{"a remote with a fresh name", Choice{Source: "origin/fresh", Name: "fresh"}, hintWillStart, true},
		{"an unknown source", Choice{Source: "elsewhere", Name: "fresh"}, hintWillStart, true},
	} {
		got := Validate(c.choice, known())
		if got.Key != c.key || got.OK != c.ok {
			t.Errorf("%s: %+v", c.name, got)
		}
	}
}

func TestABranchNameIsCheckedForTheUsualTraps(t *testing.T) {
	for _, name := range []string{"bad..name", "-lead", "name.lock", "with space", "tilde~", "caret^", "colon:", "question?", "star*", "bracket[", "back\\slash"} {
		if validName(name) {
			t.Errorf("%q passed", name)
		}
	}
	if !validName("good/name-1.2") {
		t.Fatal("a good name was refused")
	}
}

func TestTheLocalNameOfARemoteBranchDropsTheRemote(t *testing.T) {
	if got := LocalNameFor("origin/feature/one"); got != "feature/one" {
		t.Fatalf("name = %q", got)
	}
	if got := LocalNameFor("plain"); got != "plain" {
		t.Fatalf("name = %q", got)
	}
}

func TestTheSourcesAreLocalThenRemote(t *testing.T) {
	if got := known().Sources(); !slices.Equal(got, []string{"main", "topic", "origin/main", "origin/fresh"}) {
		t.Fatalf("sources = %v", got)
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
			"sourceLabel": widget.NewLabel("", widget.CurrentTheme().LabelText),
			"source":      widget.NewDropdown(),
			"nameLabel":   widget.NewLabel("", widget.CurrentTheme().LabelText),
			"name":        widget.NewTextInput(""),
			"track":       widget.NewCheckBox(""),
			"hint":        widget.NewLabel("", widget.CurrentTheme().LabelText),
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

func TestTheDialogStartsOnTheChosenBranch(t *testing.T) {
	v := newTestView(t)

	v.SetKnown(known(), "topic")

	if choice := v.Choice(); choice.Source != "topic" || choice.StartsABranch() {
		t.Fatalf("choice = %+v", choice)
	}
	if !v.okBtn.IsEnabled() || v.nameBox.IsEnabled() || v.trackCheck.IsEnabled() {
		t.Fatal("a local branch offers a name or tracking")
	}
	if v.hintLabel.Text() != i18n.Tf(hintWillSwitch, "topic") {
		t.Fatalf("hint = %q", v.hintLabel.Text())
	}
}

func TestTheCurrentBranchIsRefused(t *testing.T) {
	v := newTestView(t)
	confirmed := false
	v.OnOK = func(Choice) { confirmed = true }

	v.SetKnown(known(), "main")
	v.okBtn.OnClick()

	if confirmed || v.okBtn.IsEnabled() || v.hintLabel.Text() != i18n.Tf(hintAlreadyThere, "main") {
		t.Fatalf("hint = %q", v.hintLabel.Text())
	}
}

func TestARemoteBranchOffersALocalNameAndTracking(t *testing.T) {
	v := newTestView(t)
	var chosen Choice
	v.OnOK = func(choice Choice) { chosen = choice }

	v.SetKnown(known(), "origin/fresh")
	v.Dialog().DefaultAction()

	if chosen.Source != "origin/fresh" || chosen.Name != "fresh" || !chosen.Track {
		t.Fatalf("choice = %+v", chosen)
	}
	if !v.nameBox.IsEnabled() || !v.trackCheck.IsEnabled() {
		t.Fatal("a remote branch does not offer a name or tracking")
	}
	if v.hintLabel.Text() != i18n.Tf(hintWillStart, "fresh", "origin/fresh") {
		t.Fatalf("hint = %q", v.hintLabel.Text())
	}
}

func TestTurningTrackingOffIsRemembered(t *testing.T) {
	v := newTestView(t)
	var chosen Choice
	v.OnOK = func(choice Choice) { chosen = choice }
	v.SetKnown(known(), "origin/fresh")

	v.trackCheck.SetChecked(false)
	v.trackCheck.OnChange(false)
	v.okBtn.OnClick()

	if chosen.Track {
		t.Fatalf("choice = %+v", chosen)
	}
}

func TestTypingANameInUseStopsTheDialog(t *testing.T) {
	v := newTestView(t)
	confirmed := false
	v.OnOK = func(Choice) { confirmed = true }
	v.SetKnown(known(), "origin/fresh")

	v.nameBox.SetText("topic")
	v.nameBox.OnChange("topic")
	v.okBtn.OnClick()

	if confirmed || v.okBtn.IsEnabled() || v.hintLabel.Text() != i18n.Tf(hintNameTaken, "topic") {
		t.Fatalf("hint = %q", v.hintLabel.Text())
	}
}

func TestChangingTheSourceBackToALocalBranchForgetsTheName(t *testing.T) {
	v := newTestView(t)
	v.SetKnown(known(), "origin/fresh")

	v.sourceList.SetSelected(slices.Index(known().Sources(), "topic"))
	v.sourceList.OnChange(0, "topic")

	if choice := v.Choice(); choice.StartsABranch() {
		t.Fatalf("choice = %+v", choice)
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
	v.SetKnown(known(), "topic")

	v.okBtn.OnClick()
	v.cancelBtn.OnClick()
}

func TestRestyleAcceptsBothThemes(t *testing.T) {
	v := newTestView(t)
	for _, theme := range []*widget.Theme{widget.Win11DarkTheme(), widget.Win11LightTheme()} {
		v.Restyle(theme)
	}
}
