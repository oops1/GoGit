package flow

import (
	"errors"
	"slices"
	"testing"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/gitcore/ops"
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

func check(box *widget.CheckBox, on bool) {
	box.SetChecked(on)
	if box.OnChange != nil {
		box.OnChange(on)
	}
}

func choose(radio *widget.RadioButton, others ...*widget.RadioButton) {
	for _, other := range others {
		other.SetSelected(false)
	}
	radio.SetSelected(true)
	if radio.OnChange != nil {
		radio.OnChange(true)
	}
}

func assertTexts(t *testing.T, texts map[string]string) {
	t.Helper()
	for got, want := range texts {
		if got != want {
			t.Errorf("text = %q, want %q", got, want)
		}
	}
}

func TestEveryFlowKindHasItsTexts(t *testing.T) {
	installStrings(t)

	for _, kind := range ops.FlowKinds {
		texts, ok := kindKeys[kind]
		if !ok {
			t.Fatalf("no texts for %s", kind)
		}
		keys := []string{texts.startTitle, texts.startHeader, texts.startText, texts.nameLabel, texts.nameRequired}
		if kind != ops.FlowKindSupport {
			keys = append(keys, texts.finishTitle, texts.finishHeader, texts.finishText, texts.deleteBranch, texts.pushRemove)
		}
		for _, key := range keys {
			if key == "" || i18n.T(key) == key {
				t.Errorf("%s: key %q has no string", kind, key)
			}
		}
	}
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

func TestValidateStartJudgesTheNameAndTheBase(t *testing.T) {
	known := StartKnown{Base: "develop", Prefix: "feature/", Taken: []string{"feature/login"}}
	for _, c := range []struct {
		model StartModel
		key   string
		args  []any
		ok    bool
	}{
		{StartModel{Name: "  ", Base: "develop"}, kindKeys[ops.FlowKindFeature].nameRequired, nil, false},
		{StartModel{Name: "log in", Base: "develop"}, hintNameInvalid, []any{"log in"}, false},
		{StartModel{Name: "login", Base: "develop"}, hintNameTaken, []any{"feature/login"}, false},
		{StartModel{Name: "search"}, hintBaseRequired, nil, false},
		{StartModel{Name: " search ", Base: "develop"}, "", nil, true},
	} {
		got := ValidateStart(ops.FlowKindFeature, c.model, known)
		if got.Key != c.key || got.OK != c.ok || !slices.Equal(got.Args, c.args) {
			t.Errorf("%+v: %+v", c.model, got)
		}
	}
}

func TestValidateConfigJudgesTheBranches(t *testing.T) {
	for _, c := range []struct {
		model ConfigModel
		key   string
		ok    bool
	}{
		{ConfigModel{Light: true}, hintDevelopRequired, false},
		{ConfigModel{Develop: "develop"}, hintBranchesRequired, false},
		{ConfigModel{Master: "master", Develop: "dev..elop"}, hintBranchInvalid, false},
		{ConfigModel{Light: true, Develop: "master"}, hintConfigReady, true},
		{ConfigModel{Master: "ma ster", Develop: "develop"}, hintBranchInvalid, false},
		{ConfigModel{Master: "main", Develop: "main"}, hintBranchesSame, false},
		{ConfigModel{Master: "main", Develop: "develop"}, hintConfigReady, true},
	} {
		if got := ValidateConfig(c.model); got.Key != c.key || got.OK != c.ok {
			t.Errorf("%+v: %+v", c.model, got)
		}
	}
}

func TestConfigModelConvertsToAndFromTheFlowConfig(t *testing.T) {
	full := ops.DefaultFlowConfig()
	full.VersionTagPrefix = "v"
	if got := ConfigModelOf(full).FlowConfig(); got != full {
		t.Fatalf("full round trip = %+v, want %+v", got, full)
	}
	light := ConfigModelOf(ops.DefaultLightFlowConfig())
	light.Master = "left over"
	if got := light.FlowConfig(); got != ops.DefaultLightFlowConfig() || !light.Light {
		t.Fatalf("light round trip = %+v", got)
	}
}

func TestViewsPropagateTheDialogLoadError(t *testing.T) {
	installStrings(t)
	boom := errors.New("boom")
	stubDialog(t, nil, boom)

	for name, open := range map[string]func() error{
		"start":      func() error { _, err := NewStartView(ops.FlowKindRelease); return err },
		"finish":     func() error { _, err := NewFinishView(ops.FlowKindRelease); return err },
		"config":     func() error { _, err := NewConfigView(); return err },
		"configured": func() error { _, err := NewConfiguredView(); return err },
		"integrate":  func() error { _, err := NewIntegrateView(); return err },
	} {
		if err := open(); !errors.Is(err, boom) {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

func startWidgets() map[string]widget.Widget {
	return map[string]widget.Widget{
		"header": label(), "text": label(), "nameLabel": label(), "name": widget.NewTextInput(""),
		"result": label(), "baseLabel": label(), "base": widget.NewDropdown(), "hint": label(),
		"ok": widget.NewButton(""), "cancel": widget.NewButton(""),
	}
}

func finishWidgets(kind string) func() map[string]widget.Widget {
	return func() map[string]widget.Widget {
		named := map[string]widget.Widget{
			"header": label(), "text": label(), "messageLabel": label(), "message": widget.NewTextBox(""),
			"deleteBranch": widget.NewCheckBox(""), "push": widget.NewCheckBox(""), "hint": label(),
			"ok": widget.NewButton(""), "cancel": widget.NewButton(""),
		}
		if kind == ops.FlowKindFeature {
			for _, name := range []string{"modeMerge", "modeSquash", "modeRebase"} {
				named[name] = widget.NewRadioButton("", "test")
			}
			return named
		}
		named["fetch"] = widget.NewCheckBox("")
		named["createTag"] = widget.NewCheckBox("")
		named["tagName"] = widget.NewTextInput("")
		if kind == ops.FlowKindHotfix {
			named["mergeDevelop"] = widget.NewCheckBox("")
		}
		return named
	}
}

func configWidgets() map[string]widget.Widget {
	named := map[string]widget.Widget{
		"header": label(), "typeLight": widget.NewRadioButton("", "test"), "typeFull": widget.NewRadioButton("", "test"),
		"remote": widget.NewDropdown(), "reset": widget.NewButton(""), "hint": label(),
		"ok": widget.NewButton(""), "cancel": widget.NewButton(""),
	}
	for _, name := range configFieldNames {
		named[name] = widget.NewTextInput("")
	}
	return named
}

func configuredWidgets() map[string]widget.Widget {
	return map[string]widget.Widget{
		"header": label(), "text": label(),
		"change": widget.NewButton(""), "switchOff": widget.NewButton(""), "cancel": widget.NewButton(""),
	}
}

func integrateWidgets() map[string]widget.Widget {
	return map[string]widget.Widget{
		"header": label(), "text": label(),
		"modeMerge": widget.NewRadioButton("", "test"), "modeRebase": widget.NewRadioButton("", "test"),
		"ok": widget.NewButton(""), "cancel": widget.NewButton(""),
	}
}

func TestViewsReportEveryMissingWidget(t *testing.T) {
	installStrings(t)
	finish := func(kind string) func() error {
		return func() error { _, err := NewFinishView(kind); return err }
	}
	for view, c := range map[string]struct {
		widgets func() map[string]widget.Widget
		open    func() error
	}{
		"start":          {startWidgets, func() error { _, err := NewStartView(ops.FlowKindRelease); return err }},
		"finish feature": {finishWidgets(ops.FlowKindFeature), finish(ops.FlowKindFeature)},
		"finish release": {finishWidgets(ops.FlowKindRelease), finish(ops.FlowKindRelease)},
		"finish hotfix":  {finishWidgets(ops.FlowKindHotfix), finish(ops.FlowKindHotfix)},
		"config":         {configWidgets, func() error { _, err := NewConfigView(); return err }},
		"configured":     {configuredWidgets, func() error { _, err := NewConfiguredView(); return err }},
		"integrate":      {integrateWidgets, func() error { _, err := NewIntegrateView(); return err }},
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

func newStart(t *testing.T, kind string) *StartView {
	t.Helper()
	installStrings(t)
	v, err := NewStartView(kind)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestTheStartDialogShowsTheResultingBranchAndConfirmsAUsableName(t *testing.T) {
	v := newStart(t, ops.FlowKindHotfix)
	v.SetKnown(StartKnown{Base: "master", Bases: []string{"develop", "master"}, Prefix: "hotfix/"})
	var got []StartModel
	v.OnOK = func(m StartModel) { got = append(got, m) }

	v.okBtn.OnClick()
	type_(v.nameBox, " 1.0.1 ")
	v.okBtn.OnClick()

	if len(got) != 1 || got[0] != (StartModel{Name: "1.0.1", Base: "master"}) {
		t.Fatalf("confirmed = %+v", got)
	}
	assertTexts(t, map[string]string{
		v.Dialog().Title:     i18n.T("Dialog.FlowStart.Title.Hotfix"),
		v.headerLabel.Text(): i18n.T("Dialog.FlowStart.Header.Hotfix"),
		v.nameLabel.Text():   i18n.T("Dialog.FlowStart.NameLabel.Hotfix"),
		v.textLabel.Text():   i18n.Tf("Dialog.FlowStart.Text.Hotfix", "master"),
		v.resultLabel.Text(): i18n.Tf(resultingBranch, "hotfix/1.0.1"),
		v.hintLabel.Text():   "",
	})

	v.baseList.SetSelected(0)
	v.baseList.OnChange(0, "develop")
	if v.Model().Base != "develop" || v.textLabel.Text() != i18n.Tf("Dialog.FlowStart.Text.Hotfix", "develop") {
		t.Fatalf("base = %q, text = %q", v.Model().Base, v.textLabel.Text())
	}
}

func TestTheStartDialogNeedsABase(t *testing.T) {
	v := newStart(t, ops.FlowKindSupport)

	v.SetKnown(StartKnown{Base: "gone", Prefix: "support/"})
	type_(v.nameBox, "1.x")

	if v.okBtn.IsEnabled() || v.hintLabel.Text() != i18n.T(hintBaseRequired) {
		t.Fatalf("hint = %q, ok = %v", v.hintLabel.Text(), v.okBtn.IsEnabled())
	}
}

func TestTheStartDialogCancelsAndRestyles(t *testing.T) {
	v := newStart(t, ops.FlowKindRelease)
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

func newFinish(t *testing.T, kind string) *FinishView {
	t.Helper()
	installStrings(t)
	v, err := NewFinishView(kind)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

var (
	featureKnown = FinishKnown{Name: "login", Branch: "feature/login", Master: "master", Develop: "develop", CanPush: true}
	releaseKnown = FinishKnown{Name: "1.0", Branch: "release/1.0", Master: "master", Develop: "develop", Tag: "v1.0", CanFetch: true, CanPush: true}
	hotfixKnown  = FinishKnown{Name: "1.0.1", Branch: "hotfix/1.0.1", Master: "master", Develop: "develop", Tag: "1.0.1"}
)

func TestTheFeatureFinishDialogOffersTheWaysToIntegrate(t *testing.T) {
	v := newFinish(t, ops.FlowKindFeature)
	var got []FinishModel
	v.OnOK = func(m FinishModel) { got = append(got, m) }

	v.SetKnown(featureKnown)
	v.okBtn.OnClick()
	choose(v.squashRadio, v.mergeRadio, v.rebaseRadio)
	squash := v.Model()
	choose(v.rebaseRadio, v.mergeRadio, v.squashRadio)
	rebase := v.Model()

	want := FinishModel{Message: "Finish login", Fetch: true, Push: true, DeleteBranch: true}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("confirmed = %+v, want %+v", got, want)
	}
	if squash.Integration != ops.FlowSquash || rebase.Integration != ops.FlowRebase {
		t.Fatalf("squash = %+v, rebase = %+v", squash, rebase)
	}
	assertTexts(t, map[string]string{
		v.Dialog().Title:      i18n.T("Dialog.FlowFinish.Title.Feature"),
		v.headerLabel.Text():  i18n.T("Dialog.FlowFinish.Header.Feature"),
		v.textLabel.Text():    i18n.Tf("Dialog.FlowFinish.Text.Feature", "develop"),
		v.messageLabel.Text(): i18n.T("Dialog.FlowFinish.MessageLabel.Merge"),
		v.rebaseRadio.Text:    i18n.Tf("Dialog.FlowFinish.Rebase", "develop"),
		v.deleteBox.GetText(): i18n.T("Dialog.FlowFinish.DeleteBranch.Feature"),
		v.pushBox.GetText():   i18n.T("Dialog.FlowFinish.FetchRemove.Feature"),
	})

	local := featureKnown
	local.CanPush = false
	v.SetKnown(local)
	if m := v.Model(); m.Push || m.Fetch || v.pushBox.IsEnabled() {
		t.Fatalf("a feature without a remote branch = %+v", m)
	}
}

func TestTheReleaseFinishDialogAsksForTheTag(t *testing.T) {
	v := newFinish(t, ops.FlowKindRelease)
	var got []FinishModel
	v.OnOK = func(m FinishModel) { got = append(got, m) }

	v.SetKnown(releaseKnown)
	v.okBtn.OnClick()

	want := FinishModel{Message: "Finish 1.0", TagName: "v1.0", Fetch: true, DeleteBranch: true}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("confirmed = %+v, want %+v", got, want)
	}
	assertTexts(t, map[string]string{
		v.textLabel.Text():    i18n.Tf("Dialog.FlowFinish.Text.Release", "release/1.0", "master", "develop"),
		v.messageLabel.Text(): i18n.T("Dialog.FlowFinish.MessageLabel.Tag"),
		v.fetchBox.GetText():  i18n.Tf("Dialog.FlowFinish.Fetch.Release", "develop", "master"),
		v.pushBox.GetText():   i18n.T("Dialog.FlowFinish.PushRemove.Release"),
	})

	type_(v.tagInput, " ")
	if v.okBtn.IsEnabled() || v.hintLabel.Text() != i18n.T(hintTagRequired) {
		t.Fatalf("empty tag: hint = %q", v.hintLabel.Text())
	}
	type_(v.tagInput, "v 1")
	v.okBtn.OnClick()
	if len(got) != 1 || v.hintLabel.Text() != i18n.Tf(hintTagInvalid, "v 1") {
		t.Fatalf("invalid tag: hint = %q, confirmed %d", v.hintLabel.Text(), len(got))
	}
	check(v.tagBox, false)
	check(v.pushBox, true)
	if m := v.Model(); !m.SkipTag || !m.Push || !v.okBtn.IsEnabled() {
		t.Fatalf("without a tag = %+v", m)
	}
}

func TestTheHotfixFinishDialogCanSkipDevelop(t *testing.T) {
	v := newFinish(t, ops.FlowKindHotfix)

	v.SetKnown(hotfixKnown)
	check(v.developBox, false)

	if m := v.Model(); !m.SkipDevelop || m.Fetch || m.Push || m.TagName != "1.0.1" {
		t.Fatalf("model = %+v", m)
	}
	if v.fetchBox.IsEnabled() || v.pushBox.IsEnabled() {
		t.Fatal("a hotfix without remote branches offers the network")
	}
	assertTexts(t, map[string]string{
		v.textLabel.Text():     i18n.Tf("Dialog.FlowFinish.Text.Hotfix", "master", "develop"),
		v.fetchBox.GetText():   i18n.Tf("Dialog.FlowFinish.Fetch.Hotfix", "master"),
		v.developBox.GetText(): i18n.Tf("Dialog.FlowFinish.MergeDevelop", "develop"),
	})
}

func TestTheFinishDialogKeepsTheSavedChoicesWhenResuming(t *testing.T) {
	for _, kind := range []string{ops.FlowKindFeature, ops.FlowKindHotfix} {
		v := newFinish(t, kind)
		known := hotfixKnown
		known.CanPush, known.CanFetch, known.Resuming = true, true, true

		v.SetKnown(known)

		boxes := []interface{ IsEnabled() bool }{v.messageBox, v.deleteBox, v.pushBox}
		if kind == ops.FlowKindFeature {
			boxes = append(boxes, v.mergeRadio, v.squashRadio, v.rebaseRadio)
		} else {
			boxes = append(boxes, v.fetchBox, v.tagBox, v.tagInput, v.developBox)
		}
		for _, box := range boxes {
			if box.IsEnabled() {
				t.Fatalf("%s: a resumed finish offers a choice it will not use", kind)
			}
		}
		if !v.okBtn.IsEnabled() || v.hintLabel.Text() != i18n.Tf(hintWillResume, "hotfix/1.0.1") {
			t.Fatalf("%s: hint = %q", kind, v.hintLabel.Text())
		}
	}
}

func TestTheFinishDialogCancelsAndRestyles(t *testing.T) {
	for _, kind := range []string{ops.FlowKindFeature, ops.FlowKindRelease} {
		v := newFinish(t, kind)
		v.okBtn.OnClick()
		v.cancelBtn.OnClick()
		cancelled := false
		v.OnCancel = func() { cancelled = true }

		v.Dialog().CancelAction()
		for _, theme := range []*widget.Theme{widget.Win11DarkTheme(), widget.Win11LightTheme()} {
			v.Restyle(theme)
		}

		if !cancelled {
			t.Fatalf("%s: cancel did not reach the callback", kind)
		}
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
	v.SetRemotes([]string{"origin", "upstream"})
	want := ConfigModel{Develop: "develop", Master: "main", Remote: "upstream", Feature: "feature/", Release: "release/", Hotfix: "hotfix/", Support: "support/", VersionTag: "v"}
	var got []ConfigModel
	v.OnOK = func(m ConfigModel) { got = append(got, m) }

	v.okBtn.OnClick()
	v.SetModel(want)
	type_(v.fields[6], " v ")
	v.okBtn.OnClick()

	if len(got) != 1 || got[0] != want {
		t.Fatalf("confirmed = %+v, want %+v", got, want)
	}
	if v.hintLabel.Text() != i18n.T(hintConfigReady) || !v.fields[1].IsEnabled() {
		t.Fatalf("hint = %q", v.hintLabel.Text())
	}
}

func TestTheConfigDialogSwitchesTheTypeAndResetsToTheDefaults(t *testing.T) {
	v := newConfig(t)
	v.SetRemotes([]string{"origin"})
	v.SetModel(ConfigModel{Develop: "dev", Master: "trunk"})

	choose(v.lightRadio, v.fullRadio)
	for _, i := range []int{1, 3, 4, 5, 6} {
		if v.fields[i].IsEnabled() {
			t.Fatalf("field %s stays editable in the light mode", configFieldNames[i])
		}
	}
	v.resetBtn.OnClick()
	if got := v.Model(); got != ConfigModelOf(ops.DefaultLightFlowConfig()) {
		t.Fatalf("light defaults = %+v", got)
	}

	choose(v.fullRadio, v.lightRadio)
	v.resetBtn.OnClick()
	if got := v.Model(); got != ConfigModelOf(ops.DefaultFlowConfig()) || !v.okBtn.IsEnabled() {
		t.Fatalf("full defaults = %+v", got)
	}
}

func TestTheConfigDialogCancelsAndRestyles(t *testing.T) {
	v := newConfig(t)
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

func TestTheConfiguredQuestionOffersToChangeOrSwitchOff(t *testing.T) {
	installStrings(t)
	v, err := NewConfiguredView()
	if err != nil {
		t.Fatal(err)
	}
	v.changeBtn.OnClick()
	var calls []string
	v.OnChange = func() { calls = append(calls, "change") }
	v.OnSwitchOff = func() { calls = append(calls, "off") }
	v.OnCancel = func() { calls = append(calls, "cancel") }

	v.changeBtn.OnClick()
	v.switchOffBtn.OnClick()
	v.Dialog().CancelAction()
	for _, theme := range []*widget.Theme{widget.Win11DarkTheme(), widget.Win11LightTheme()} {
		v.Restyle(theme)
	}

	if !slices.Equal(calls, []string{"change", "off", "cancel"}) {
		t.Fatalf("calls = %v", calls)
	}
}

func TestTheIntegrateDialogReturnsTheChosenWay(t *testing.T) {
	installStrings(t)
	v, err := NewIntegrateView()
	if err != nil {
		t.Fatal(err)
	}
	v.okBtn.OnClick()
	var got []bool
	v.OnOK = func(rebase bool) { got = append(got, rebase) }
	cancelled := false
	v.OnCancel = func() { cancelled = true }

	v.SetKnown("feature/login", "develop")
	v.okBtn.OnClick()
	choose(v.rebaseRadio, v.mergeRadio)
	v.okBtn.OnClick()
	v.Dialog().CancelAction()
	for _, theme := range []*widget.Theme{widget.Win11DarkTheme(), widget.Win11LightTheme()} {
		v.Restyle(theme)
	}

	if !slices.Equal(got, []bool{false, true}) || !cancelled {
		t.Fatalf("confirmed = %v, cancelled = %v", got, cancelled)
	}
	assertTexts(t, map[string]string{
		v.textLabel.Text(): i18n.Tf("Dialog.FlowIntegrate.Text", "develop", "feature/login"),
		v.mergeRadio.Text:  i18n.Tf("Dialog.FlowIntegrate.Merge", "develop"),
		v.rebaseRadio.Text: i18n.Tf("Dialog.FlowIntegrate.Rebase", "develop"),
	})
}
