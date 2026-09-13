package flow

import (
	"errors"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/style"
)

const (
	finishDialogName = "flow_finish_release"
	hintWillFinish   = "Dialog.FlowFinish.Hint.WillFinish"
	hintWillResume   = "Dialog.FlowFinish.Hint.WillResume"
)

type FinishKnown struct {
	Version  string
	Branch   string
	Master   string
	Develop  string
	Tag      string
	CanPush  bool
	Resuming bool
}

type FinishModel struct {
	TagMessage   string
	Push         bool
	DeleteBranch bool
}

type FinishView struct {
	dlg          *widget.Dialog
	releaseLabel *widget.Label
	messageLabel *widget.Label
	messageBox   *widget.TextBox
	pushBox      *widget.CheckBox
	deleteBox    *widget.CheckBox
	hintLabel    *widget.Label
	okBtn        *widget.Button
	cancelBtn    *widget.Button

	known FinishKnown

	OnOK     func(FinishModel)
	OnCancel func()
}

func NewFinishView() (*FinishView, error) {
	dlg, named, err := loadDialog(finishDialogName, i18n.T("Dialog.FlowFinish.Title"))
	if err != nil {
		return nil, err
	}
	v := &FinishView{dlg: dlg}
	if err := errors.Join(
		bindWidget(named, "releaseLabel", &v.releaseLabel),
		bindWidget(named, "messageLabel", &v.messageLabel),
		bindWidget(named, "message", &v.messageBox),
		bindWidget(named, "push", &v.pushBox),
		bindWidget(named, "deleteBranch", &v.deleteBox),
		bindWidget(named, "hint", &v.hintLabel),
		bindWidget(named, "ok", &v.okBtn),
		bindWidget(named, "cancel", &v.cancelBtn),
	); err != nil {
		return nil, err
	}
	v.hintLabel.Muted = true
	v.deleteBox.SetChecked(true)
	v.okBtn.OnClick = v.confirm
	v.cancelBtn.OnClick = v.cancel
	v.dlg.CancelAction = v.cancel
	return v, nil
}

func (v *FinishView) Dialog() *widget.Dialog { return v.dlg }

func (v *FinishView) Restyle(t *widget.Theme) {
	p := style.Of(t)
	p.Areas(v.messageBox)
	p.Quiet(v.cancelBtn)
	p.Primary(v.okBtn)
	p.Body(v.releaseLabel, v.messageLabel)
	p.Hints(v.hintLabel)
}

func (v *FinishView) SetKnown(known FinishKnown) {
	v.known = known
	v.releaseLabel.SetText(i18n.Tf("Dialog.FlowFinish.Release", known.Branch))
	v.messageBox.SetText(i18n.Tf("Dialog.FlowFinish.DefaultMessage", known.Version))
	v.pushBox.SetChecked(known.CanPush)
	v.pushBox.SetEnabled(known.CanPush && !known.Resuming)
	v.messageBox.SetEnabled(!known.Resuming)
	v.deleteBox.SetEnabled(!known.Resuming)
	if known.Resuming {
		v.hintLabel.SetText(i18n.Tf(hintWillResume, known.Version))
		return
	}
	v.hintLabel.SetText(i18n.Tf(hintWillFinish, known.Branch, known.Master, known.Tag, known.Develop))
}

func (v *FinishView) Model() FinishModel {
	return FinishModel{
		TagMessage:   v.messageBox.GetText(),
		Push:         v.known.CanPush && v.pushBox.IsChecked(),
		DeleteBranch: v.deleteBox.IsChecked(),
	}
}

func (v *FinishView) confirm() {
	if v.OnOK != nil {
		v.OnOK(v.Model())
	}
}

func (v *FinishView) cancel() {
	if v.OnCancel != nil {
		v.OnCancel()
	}
}
