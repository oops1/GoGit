package flow

import (
	"errors"
	"strings"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/style"
)

const (
	configDialogName     = "flow_configure"
	hintBranchesRequired = "Dialog.FlowConfig.Hint.BranchesRequired"
	hintBranchInvalid    = "Dialog.FlowConfig.Hint.BranchInvalid"
	hintBranchesSame     = "Dialog.FlowConfig.Hint.BranchesSame"
	hintConfigReady      = "Dialog.FlowConfig.Hint.Ready"
)

type ConfigModel struct {
	Master     string
	Develop    string
	Feature    string
	Release    string
	Hotfix     string
	Support    string
	VersionTag string
}

func ValidateConfig(model ConfigModel) Hint {
	switch {
	case model.Master == "" || model.Develop == "":
		return Hint{Key: hintBranchesRequired}
	case !validBranchName(model.Master):
		return Hint{Key: hintBranchInvalid, Args: []any{model.Master}}
	case !validBranchName(model.Develop):
		return Hint{Key: hintBranchInvalid, Args: []any{model.Develop}}
	case model.Master == model.Develop:
		return Hint{Key: hintBranchesSame}
	}
	return Hint{Key: hintConfigReady, OK: true}
}

type ConfigView struct {
	dlg       *widget.Dialog
	fields    [7]*widget.TextInput
	hintLabel *widget.Label
	okBtn     *widget.Button
	cancelBtn *widget.Button

	current Hint

	OnOK     func(ConfigModel)
	OnCancel func()
}

var configFieldNames = [7]string{"master", "develop", "feature", "release", "hotfix", "support", "versionTag"}

func NewConfigView() (*ConfigView, error) {
	dlg, named, err := loadDialog(configDialogName, i18n.T("Dialog.FlowConfig.Title"))
	if err != nil {
		return nil, err
	}
	v := &ConfigView{dlg: dlg}
	errs := []error{
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
	p.Quiet(v.cancelBtn)
	p.Primary(v.okBtn)
	p.Hints(v.hintLabel)
}

func (v *ConfigView) SetModel(model ConfigModel) {
	for i, value := range model.values() {
		v.fields[i].SetText(value)
	}
	v.refresh()
}

func (v *ConfigView) Model() ConfigModel {
	var values [7]string
	for i, field := range v.fields {
		values[i] = strings.TrimSpace(field.GetText())
	}
	return ConfigModel{Master: values[0], Develop: values[1], Feature: values[2], Release: values[3], Hotfix: values[4], Support: values[5], VersionTag: values[6]}
}

func (m ConfigModel) values() [7]string {
	return [7]string{m.Master, m.Develop, m.Feature, m.Release, m.Hotfix, m.Support, m.VersionTag}
}

func (v *ConfigView) refresh() {
	v.current = ValidateConfig(v.Model())
	v.hintLabel.SetText(i18n.Tf(v.current.Key, v.current.Args...))
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
