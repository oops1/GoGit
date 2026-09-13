package flow

import (
	"errors"
	"slices"
	"strings"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/gitcore/ops"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/style"
)

const (
	configDialogName     = "flow_configure"
	hintBranchesRequired = "Dialog.FlowConfig.Hint.BranchesRequired"
	hintDevelopRequired  = "Dialog.FlowConfig.Hint.DevelopRequired"
	hintBranchInvalid    = "Dialog.FlowConfig.Hint.BranchInvalid"
	hintBranchesSame     = "Dialog.FlowConfig.Hint.BranchesSame"
	hintConfigReady      = "Dialog.FlowConfig.Hint.Ready"
)

type ConfigModel struct {
	Light      bool
	Develop    string
	Master     string
	Remote     string
	Feature    string
	Release    string
	Hotfix     string
	Support    string
	VersionTag string
}

func ConfigModelOf(c ops.FlowConfig) ConfigModel {
	return ConfigModel{
		Light:      c.Light(),
		Develop:    c.Develop,
		Master:     c.Master,
		Remote:     c.Remote,
		Feature:    c.FeaturePrefix,
		Release:    c.ReleasePrefix,
		Hotfix:     c.HotfixPrefix,
		Support:    c.SupportPrefix,
		VersionTag: c.VersionTagPrefix,
	}
}

func (m ConfigModel) FlowConfig() ops.FlowConfig {
	c := ops.FlowConfig{
		Master:           m.Master,
		Develop:          m.Develop,
		FeaturePrefix:    m.Feature,
		ReleasePrefix:    m.Release,
		HotfixPrefix:     m.Hotfix,
		SupportPrefix:    m.Support,
		VersionTagPrefix: m.VersionTag,
		Remote:           m.Remote,
	}
	if m.Light {
		c.Master = ""
	}
	return c
}

func ValidateConfig(model ConfigModel) Hint {
	switch {
	case model.Light && model.Develop == "":
		return Hint{Key: hintDevelopRequired}
	case !model.Light && (model.Master == "" || model.Develop == ""):
		return Hint{Key: hintBranchesRequired}
	case !validBranchName(model.Develop):
		return Hint{Key: hintBranchInvalid, Args: []any{model.Develop}}
	case model.Light:
	case !validBranchName(model.Master):
		return Hint{Key: hintBranchInvalid, Args: []any{model.Master}}
	case model.Master == model.Develop:
		return Hint{Key: hintBranchesSame}
	}
	return Hint{Key: hintConfigReady, OK: true}
}

type ConfigView struct {
	dlg         *widget.Dialog
	headerLabel *widget.Label
	lightRadio  *widget.RadioButton
	fullRadio   *widget.RadioButton
	fields      [7]*widget.TextInput
	remoteList  *widget.Dropdown
	resetBtn    *widget.Button
	hintLabel   *widget.Label
	okBtn       *widget.Button
	cancelBtn   *widget.Button

	current Hint

	OnOK     func(ConfigModel)
	OnCancel func()
}

var configFieldNames = [7]string{"develop", "master", "feature", "release", "hotfix", "support", "versionTag"}

func NewConfigView() (*ConfigView, error) {
	dlg, named, err := loadDialog(configDialogName, i18n.T("Dialog.FlowConfig.Title"))
	if err != nil {
		return nil, err
	}
	v := &ConfigView{dlg: dlg}
	errs := []error{
		bindWidget(named, "header", &v.headerLabel),
		bindWidget(named, "typeLight", &v.lightRadio),
		bindWidget(named, "typeFull", &v.fullRadio),
		bindWidget(named, "remote", &v.remoteList),
		bindWidget(named, "reset", &v.resetBtn),
		bindWidget(named, "hint", &v.hintLabel),
		bindWidget(named, "ok", &v.okBtn),
		bindWidget(named, "cancel", &v.cancelBtn),
	}
	for i, name := range configFieldNames {
		errs = append(errs, bindWidget(named, name, &v.fields[i]))
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	v.hintLabel.Muted = true
	for _, field := range v.fields {
		field.OnChange = func(string) { v.refresh() }
	}
	v.lightRadio.OnChange = func(bool) { v.refresh() }
	v.fullRadio.OnChange = func(bool) { v.refresh() }
	v.resetBtn.OnClick = v.reset
	v.okBtn.OnClick = v.confirm
	v.cancelBtn.OnClick = v.cancel
	v.dlg.CancelAction = v.cancel
	v.refresh()
	return v, nil
}

func (v *ConfigView) Dialog() *widget.Dialog { return v.dlg }

func (v *ConfigView) Restyle(t *widget.Theme) {
	p := style.Of(t)
	p.Fields(v.fields[:]...)
	p.Lists(v.remoteList)
	p.Quiet(v.cancelBtn, v.resetBtn)
	p.Primary(v.okBtn)
	p.Body(v.headerLabel)
	p.Hints(v.hintLabel)
}

func (v *ConfigView) SetRemotes(names []string) {
	v.remoteList.SetItems(names)
}

func (v *ConfigView) SetModel(model ConfigModel) {
	v.lightRadio.SetSelected(model.Light)
	v.fullRadio.SetSelected(!model.Light)
	for i, value := range model.values() {
		v.fields[i].SetText(value)
	}
	if at := slices.Index(v.remoteList.Items(), model.Remote); at >= 0 {
		v.remoteList.SetSelected(at)
	}
	v.refresh()
}

func (v *ConfigView) Model() ConfigModel {
	var values [7]string
	for i, field := range v.fields {
		values[i] = strings.TrimSpace(field.GetText())
	}
	return ConfigModel{
		Light:      v.lightRadio.IsSelected(),
		Develop:    values[0],
		Master:     values[1],
		Remote:     v.remoteList.SelectedText(),
		Feature:    values[2],
		Release:    values[3],
		Hotfix:     values[4],
		Support:    values[5],
		VersionTag: values[6],
	}
}

func (m ConfigModel) values() [7]string {
	return [7]string{m.Develop, m.Master, m.Feature, m.Release, m.Hotfix, m.Support, m.VersionTag}
}

func (v *ConfigView) reset() {
	defaults := ops.DefaultFlowConfig()
	if v.lightRadio.IsSelected() {
		defaults = ops.DefaultLightFlowConfig()
	}
	model := ConfigModelOf(defaults)
	model.Light = v.lightRadio.IsSelected()
	v.SetModel(model)
}

func (v *ConfigView) refresh() {
	full := !v.lightRadio.IsSelected()
	for _, field := range []*widget.TextInput{v.fields[1], v.fields[3], v.fields[4], v.fields[5], v.fields[6]} {
		field.SetEnabled(full)
	}
	v.current = ValidateConfig(v.Model())
	v.hintLabel.SetText(hintText(v.current))
	v.okBtn.SetEnabled(v.current.OK)
}

func (v *ConfigView) confirm() {
	if v.current.OK && v.OnOK != nil {
		v.OnOK(v.Model())
	}
}

func (v *ConfigView) cancel() {
	if v.OnCancel != nil {
		v.OnCancel()
	}
}
