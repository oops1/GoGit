package flow

import (
	"errors"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/style"
)

const integrateDialogName = "flow_integrate"

type IntegrateView struct {
	dlg         *widget.Dialog
	headerLabel *widget.Label
	textLabel   *widget.Label
	mergeRadio  *widget.RadioButton
	rebaseRadio *widget.RadioButton
	okBtn       *widget.Button
	cancelBtn   *widget.Button

	OnOK     func(rebase bool)
	OnCancel func()
}

func NewIntegrateView() (*IntegrateView, error) {
	dlg, named, err := loadDialog(integrateDialogName, i18n.T("Dialog.FlowIntegrate.Title"))
	if err != nil {
		return nil, err
	}
	v := &IntegrateView{dlg: dlg}
	if err := errors.Join(
		bindWidget(named, "header", &v.headerLabel),
		bindWidget(named, "text", &v.textLabel),
		bindWidget(named, "modeMerge", &v.mergeRadio),
		bindWidget(named, "modeRebase", &v.rebaseRadio),
		bindWidget(named, "ok", &v.okBtn),
		bindWidget(named, "cancel", &v.cancelBtn),
	); err != nil {
		return nil, err
	}
	v.okBtn.OnClick = func() {
		if v.OnOK != nil {
			v.OnOK(v.rebaseRadio.IsSelected())
		}
	}
	v.cancelBtn.OnClick = func() { call(v.OnCancel) }
	v.dlg.CancelAction = v.cancelBtn.OnClick
	return v, nil
}

func (v *IntegrateView) Dialog() *widget.Dialog { return v.dlg }

func (v *IntegrateView) SetKnown(feature, develop string) {
	v.textLabel.SetText(i18n.Tf("Dialog.FlowIntegrate.Text", develop, feature))
	v.mergeRadio.SetText(i18n.Tf("Dialog.FlowIntegrate.Merge", develop))
	v.rebaseRadio.SetText(i18n.Tf("Dialog.FlowIntegrate.Rebase", develop))
}

func (v *IntegrateView) Restyle(t *widget.Theme) {
	p := style.Of(t)
	p.Primary(v.okBtn)
	p.Quiet(v.cancelBtn)
	p.Body(v.headerLabel, v.textLabel)
}
