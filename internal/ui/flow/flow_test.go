package flow

import (
	"errors"
	"slices"
	"testing"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/i18n"
)

func installStrings(t *testing.T) {
	t.Helper()
	widget.ClearStrings()
	t.Cleanup(widget.ClearStrings)
	if _, err := i18n.Install(""); err != nil {
		t.Fatal(err)
	}
	i18n.Apply("en")
}

func label() *widget.Label { return widget.NewLabel("", widget.CurrentTheme().LabelText) }

func stubDialog(t *testing.T, named map[string]widget.Widget, err error) {
	t.Helper()
	prev := loadDialog
	loadDialog = func(string, string) (*widget.Dialog, map[string]widget.Widget, error) {
		if err != nil {
			return nil, nil, err
		}
		return widget.NewDialog("", 100, 100), named, nil
	}
	t.Cleanup(func() { loadDialog = prev })
}

func type_(box *widget.TextInput, text string) {
	box.SetText(text)
	box.OnChange(text)
}

func TestValidBranchNameFollowsGitRules(t *testing.T) {
	for name, want := range map[string]bool{
		"release/1.0": true,
		"":            false,
		"a..b":        false,
		"a//b":        false,
		"-a":          false,
		"a.":          false,
		"a/":          false,
		"a.lock":      false,
		"a b":         false,
		"a:b":         false,
	} {
		if got := validBranchName(name); got != want {
			t.Errorf("validBranchName(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestBindWidgetNamesTheMissingWidget(t *testing.T) {
	var target *widget.Button
	err := bindWidget(map[string]widget.Widget{"ok": label()}, "ok", &target)

	if !errors.Is(err, ErrWidgetMissing) || target != nil {
		t.Fatalf("err = %v, target = %v", err, target)
	}
}

func TestValidateStartJudgesTheVersion(t *testing.T) {
	known := StartKnown{Develop: "develop", Prefix: "release/", Taken: []string{"release/1.0"}}
	for _, c := range []struct {
		version string
		key     string
		args    []any
		ok      bool
	}{
		{"  ", hintVersionRequired, nil, false},
		{"1 0", hintVersionInvalid, []any{"1 0"}, false},
		{"1.0", hintVersionTaken, []any{"release/1.0"}, false},
		{" 2.0 ", hintWillStart, []any{"release/2.0", "develop"}, true},
	} {
		got := ValidateStart(StartModel{Version: c.version}, known)
		if got.Key != c.key || got.OK != c.ok || !slices.Equal(got.Args, c.args) {
			t.Errorf("%q: %+v", c.version, got)
		}
	}
}

func TestValidateConfigJudgesTheBranches(t *testing.T) {
	for _, c := range []struct {
		model ConfigModel
		key   string
		ok    bool
	}{
		{ConfigModel{Master: "master"}, hintBranchesRequired, false},
		{ConfigModel{Master: "ma ster", Develop: "develop"}, hintBranchInvalid, false},
		{ConfigModel{Master: "master", Develop: "dev..elop"}, hintBranchInvalid, false},
		{ConfigModel{Master: "main", Develop: "main"}, hintBranchesSame, false},
		{ConfigModel{Master: "main", Develop: "develop"}, hintConfigReady, true},
	} {
		if got := ValidateConfig(c.model); got.Key != c.key || got.OK != c.ok {
			t.Errorf("%+v: %+v", c.model, got)
		}
	}
}

func TestViewsPropagateTheDialogLoadError(t *testing.T) {
	installStrings(t)
	boom := errors.New("boom")
	stubDialog(t, nil, boom)

	if _, err := NewStartView(); !errors.Is(err, boom) {
		t.Fatalf("start: %v", err)
	}
	if _, err := NewFinishView(); !errors.Is(err, boom) {
		t.Fatalf("finish: %v", err)
	}
	if _, err := NewConfigView(); !errors.Is(err, boom) {
		t.Fatalf("config: %v", err)
	}
}

func startWidgets() map[string]widget.Widget {
	return map[string]widget.Widget{
		"versionLabel": label(), "version": widget.NewTextInput(""), "hint": label(),
		"ok": widget.NewButton(""), "cancel": widget.NewButton(""),
	}
}

func finishWidgets() map[string]widget.Widget {
	return map[string]widget.Widget{
		"releaseLabel": label(), "messageLabel": label(), "message": widget.NewTextBox(""),
		"push": widget.NewCheckBox(""), "deleteBranch": widget.NewCheckBox(""), "hint": label(),
		"ok": widget.NewButton(""), "cancel": widget.NewButton(""),
	}
}

func configWidgets() map[string]widget.Widget {
	named := map[string]widget.Widget{"hint": label(), "ok": widget.NewButton(""), "cancel": widget.NewButton("")}
	for _, name := range configFieldNames {
		named[name] = widget.NewTextInput("")
	}
	return named
}

func TestViewsReportEveryMissingWidget(t *testing.T) {
	installStrings(t)
	for view, c := range map[string]struct {
		widgets func() map[string]widget.Widget
		open    func() error
	}{
		"start":  {startWidgets, func() error { _, err := NewStartView(); return err }},
		"finish": {finishWidgets, func() error { _, err := NewFinishView(); return err }},
		"config": {configWidgets, func() error { _, err := NewConfigView(); return err }},
	} {
		for name := range c.widgets() {
			named := c.widgets()
			delete(named, name)
			stubDialog(t, named, nil)
			if err := c.open(); !errors.Is(err, ErrWidgetMissing) {
				t.Fatalf("%s without %s: err = %v", view, name, err)
			}
		}
	}
}

func newStart(t *testing.T) *StartView {
	t.Helper()
	installStrings(t)
	v, err := NewStartView()
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestTheStartDialogStaysClosedUntilTheVersionIsUsable(t *testing.T) {
	v := newStart(t)
	v.SetKnown(StartKnown{Develop: "develop", Prefix: "release/"})
	var got []StartModel
	v.OnOK = func(m StartModel) { got = append(got, m) }

	v.okBtn.OnClick()
	type_(v.versionBox, " 1.0 ")
	v.okBtn.OnClick()

	if len(got) != 1 || got[0].Version != "1.0" {
		t.Fatalf("confirmed = %+v", got)
	}
	if v.hintLabel.Text() != i18n.Tf(hintWillStart, "release/1.0", "develop") || !v.okBtn.IsEnabled() {
		t.Fatalf("hint = %q, ok = %v", v.hintLabel.Text(), v.okBtn.IsEnabled())
	}
}

func TestTheStartDialogCancelsAndRestyles(t *testing.T) {
	v := newStart(t)
	v.okBtn.OnClick()
	v.cancelBtn.OnClick()
	cancelled := false
	v.OnCancel = func() { cancelled = true }

	v.Dialog().CancelAction()
	for _, theme := range []*widget.Theme{widget.Win11DarkTheme(), widget.Win11LightTheme()} {
		v.Restyle(theme)
	}

	if !cancelled {
		t.Fatal("cancel did not reach the callback")
	}
}

func newFinish(t *testing.T) *FinishView {
	t.Helper()
	installStrings(t)
	v, err := NewFinishView()
	if err != nil {
		t.Fatal(err)
	}
	return v
}

var finishKnown = FinishKnown{Version: "1.0", Branch: "release/1.0", Master: "master", Develop: "develop", Tag: "v1.0", CanPush: true}

func TestTheFinishDialogDescribesTheFinishAndReturnsTheChoices(t *testing.T) {
	v := newFinish(t)
	var got FinishModel
	v.OnOK = func(m FinishModel) { got = m }

	v.SetKnown(finishKnown)
	v.okBtn.OnClick()

	if got != (FinishModel{TagMessage: i18n.Tf("Dialog.FlowFinish.DefaultMessage", "1.0"), Push: true, DeleteBranch: true}) {
		t.Fatalf("model = %+v", got)
	}
	if v.releaseLabel.Text() != i18n.Tf("Dialog.FlowFinish.Release", "release/1.0") ||
		v.hintLabel.Text() != i18n.Tf(hintWillFinish, "release/1.0", "master", "v1.0", "develop") {
		t.Fatalf("release = %q, hint = %q", v.releaseLabel.Text(), v.hintLabel.Text())
	}
	if !v.pushBox.IsEnabled() || !v.messageBox.IsEnabled() || !v.deleteBox.IsEnabled() {
		t.Fatal("a fresh finish must let every choice be changed")
	}
}

func TestTheFinishDialogCannotPushWithoutARemote(t *testing.T) {
	v := newFinish(t)
	known := finishKnown
	known.CanPush = false
	v.SetKnown(known)
	v.pushBox.SetChecked(true)

	if v.Model().Push || v.pushBox.IsEnabled() {
		t.Fatalf("model = %+v, push enabled = %v", v.Model(), v.pushBox.IsEnabled())
	}
}

func TestTheFinishDialogKeepsTheSavedChoicesWhenResuming(t *testing.T) {
	v := newFinish(t)
	known := finishKnown
	known.Resuming = true

	v.SetKnown(known)

	if v.pushBox.IsEnabled() || v.messageBox.IsEnabled() || v.deleteBox.IsEnabled() {
		t.Fatal("a resumed finish must not offer choices it will not use")
	}
	if v.hintLabel.Text() != i18n.Tf(hintWillResume, "1.0") {
		t.Fatalf("hint = %q", v.hintLabel.Text())
	}
}

func TestTheFinishDialogCancelsAndRestyles(t *testing.T) {
	v := newFinish(t)
	v.okBtn.OnClick()
	v.cancelBtn.OnClick()
	cancelled := false
	v.OnCancel = func() { cancelled = true }

	v.Dialog().CancelAction()
	for _, theme := range []*widget.Theme{widget.Win11DarkTheme(), widget.Win11LightTheme()} {
		v.Restyle(theme)
	}

	if !cancelled {
		t.Fatal("cancel did not reach the callback")
	}
}

func newConfig(t *testing.T) *ConfigView {
	t.Helper()
	installStrings(t)
	v, err := NewConfigView()
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestTheConfigDialogShowsAndReturnsTheSettings(t *testing.T) {
	v := newConfig(t)
	want := ConfigModel{Master: "main", Develop: "develop", Feature: "feature/", Release: "release/", Hotfix: "hotfix/", Support: "support/", VersionTag: "v"}
	var got []ConfigModel
	v.OnOK = func(m ConfigModel) { got = append(got, m) }

	v.okBtn.OnClick()
	v.SetModel(want)
	type_(v.fields[6], " v ")
	v.okBtn.OnClick()

	if len(got) != 1 || got[0] != want {
		t.Fatalf("confirmed = %+v, want %+v", got, want)
	}
	if v.hintLabel.Text() != i18n.T(hintConfigReady) {
		t.Fatalf("hint = %q", v.hintLabel.Text())
	}
}

func TestTheConfigDialogCancelsAndRestyles(t *testing.T) {
	v := newConfig(t)
	v.SetModel(ConfigModel{Master: "main", Develop: "develop"})
	v.okBtn.OnClick()
	v.cancelBtn.OnClick()
	cancelled := false
	v.OnCancel = func() { cancelled = true }

	v.Dialog().CancelAction()
	for _, theme := range []*widget.Theme{widget.Win11DarkTheme(), widget.Win11LightTheme()} {
		v.Restyle(theme)
	}

	if !cancelled {
		t.Fatal("cancel did not reach the callback")
	}
}
