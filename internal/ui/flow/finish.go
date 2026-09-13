package flow

import (
	"errors"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/style"
)

const (
	finishDialogName = "flow_finish"
	hintWillFinish   = "Dialog.FlowFinish.Hint.WillFinish"
	hintWillMerge    = "Dialog.FlowFinish.Hint.WillMerge"
	hintWillResume   = "Dialog.FlowFinish.Hint.WillResume"
)

type FinishKnown struct {
	Name     string
	Branch   string
	Master   string
	Develop  string
	Tag      string
	CanPush  bool
	Resuming bool
}

type FinishModel struct {
	Message      string
	Push         bool
	DeleteBranch bool
}

type FinishView struct {
	dlg          *widget.Dialog
	branchLabel  *widget.Label
	messageLabel *widget.Label
	messageBox   *widget.TextBox
	pushBox      *widget.CheckBox
	deleteBox    *widget.CheckBox
	hintLabel    *widget.Label
	okBtn        *widget.Button
	cancelBtn    *widget.Button

	texts kindTexts
	known FinishKnown

	OnOK     func(FinishModel)
	OnCancel func()
}

func NewFinishView(kind string) (*FinishView, error) {
	texts := kindKeys[kind]
	dlg, named, err := loadDialog(finishDialogName, i18n.T(texts.finishTitle))
	if err != nil {
		return nil, err
	}
	v := &FinishView{dlg: dlg, texts: texts}
	if err := errors.Join(
		bindWidget(named, "branchLabel", &v.branchLabel),
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
	p.Body(v.branchLabel, v.messageLabel)
	p.Hints(v.hintLabel)
}

func (v *FinishView) SetKnown(known FinishKnown) {
	v.known = known
	v.branchLabel.SetText(i18n.Tf(v.texts.branchLabel, known.Branch))
	v.messageBox.SetText(i18n.Tf(v.texts.message, known.Name))
	v.deleteBox.SetText(i18n.Tf("Dialog.FlowFinish.DeleteBranch", known.Branch))
	v.pushBox.SetChecked(known.CanPush)
	v.pushBox.SetEnabled(known.CanPush && !known.Resuming)
	v.messageBox.SetEnabled(!known.Resuming)
	v.deleteBox.SetEnabled(!known.Resuming)
	messageKey, push, hint := "Dialog.FlowFinish.MessageLabel.Merge", i18n.Tf("Dialog.FlowFinish.Push.Branch", known.Develop), i18n.Tf(hintWillMerge, known.Branch, known.Develop)
	if v.texts.tagged {
		messageKey, push, hint = "Dialog.FlowFinish.MessageLabel.Tag", i18n.T("Dialog.FlowFinish.Push.Tagged"), i18n.Tf(hintWillFinish, known.Branch, known.Master, known.Tag, known.Develop)
	}
	if known.Resuming {
		hint = i18n.Tf(hintWillResume, known.Branch)
	}
	v.messageLabel.SetText(i18n.T(messageKey))
	v.pushBox.SetText(push)
	v.hintLabel.SetText(hint)
}

func (v *FinishView) Model() FinishModel {
	return FinishModel{
		Message:      v.messageBox.GetText(),
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
