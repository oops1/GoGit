package flow

import (
	"errors"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/style"
)

const configuredDialogName = "flow_configured"

type ConfiguredView struct {
	dlg          *widget.Dialog
	headerLabel  *widget.Label
	textLabel    *widget.Label
	changeBtn    *widget.Button
	switchOffBtn *widget.Button
	cancelBtn    *widget.Button

	OnChange    func()
	OnSwitchOff func()
	OnCancel    func()
}

func NewConfiguredView() (*ConfiguredView, error) {
	dlg, named, err := loadDialog(configuredDialogName, i18n.T("Dialog.FlowConfigured.Title"))
	if err != nil {
		return nil, err
	}
	v := &ConfiguredView{dlg: dlg}
	if err := errors.Join(
		bindWidget(named, "header", &v.headerLabel),
		bindWidget(named, "text", &v.textLabel),
		bindWidget(named, "change", &v.changeBtn),
		bindWidget(named, "switchOff", &v.switchOffBtn),
		bindWidget(named, "cancel", &v.cancelBtn),
	); err != nil {
		return nil, err
	}
	v.changeBtn.OnClick = func() { call(v.OnChange) }
	v.switchOffBtn.OnClick = func() { call(v.OnSwitchOff) }
	v.cancelBtn.OnClick = func() { call(v.OnCancel) }
	v.dlg.CancelAction = v.cancelBtn.OnClick
	return v, nil
}

func (v *ConfiguredView) Dialog() *widget.Dialog { return v.dlg }

func (v *ConfiguredView) Restyle(t *widget.Theme) {
	p := style.Of(t)
	p.Primary(v.changeBtn)
	p.Quiet(v.switchOffBtn, v.cancelBtn)
	p.Body(v.headerLabel, v.textLabel)
}

func call(fn func()) {
	if fn != nil {
		fn()
	}
}
