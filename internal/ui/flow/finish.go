package flow

import (
	"errors"
	"strings"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/gitcore/ops"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/style"
)

const (
	hintTagRequired = "Dialog.FlowFinish.Hint.TagRequired"
	hintTagInvalid  = "Dialog.FlowFinish.Hint.TagInvalid"
	hintWillResume  = "Dialog.FlowFinish.Hint.Resuming"
)

type FinishKnown struct {
	Name     string
	Branch   string
	Master   string
	Develop  string
	Tag      string
	CanFetch bool
	CanPush  bool
	Resuming bool
}

type FinishModel struct {
	Message      string
	Integration  ops.FlowIntegration
	TagName      string
	SkipTag      bool
	SkipDevelop  bool
	Fetch        bool
	Push         bool
	DeleteBranch bool
}

type FinishView struct {
	dlg          *widget.Dialog
	headerLabel  *widget.Label
	textLabel    *widget.Label
	messageLabel *widget.Label
	messageBox   *widget.TextBox
	deleteBox    *widget.CheckBox
	pushBox      *widget.CheckBox
	hintLabel    *widget.Label
	okBtn        *widget.Button
	cancelBtn    *widget.Button

	mergeRadio  *widget.RadioButton
	squashRadio *widget.RadioButton
	rebaseRadio *widget.RadioButton

	fetchBox   *widget.CheckBox
	tagBox     *widget.CheckBox
	tagInput   *widget.TextInput
	developBox *widget.CheckBox

	kind    string
	texts   kindTexts
	known   FinishKnown
	current Hint

	OnOK     func(FinishModel)
	OnCancel func()
}

func NewFinishView(kind string) (*FinishView, error) {
	texts := kindKeys[kind]
	dlg, named, err := loadDialog(texts.finishDialog, i18n.T(texts.finishTitle))
	if err != nil {
		return nil, err
	}
	v := &FinishView{dlg: dlg, kind: kind, texts: texts}
	binds := []error{
		bindWidget(named, "header", &v.headerLabel),
		bindWidget(named, "text", &v.textLabel),
		bindWidget(named, "messageLabel", &v.messageLabel),
		bindWidget(named, "message", &v.messageBox),
		bindWidget(named, "deleteBranch", &v.deleteBox),
		bindWidget(named, "push", &v.pushBox),
		bindWidget(named, "hint", &v.hintLabel),
		bindWidget(named, "ok", &v.okBtn),
		bindWidget(named, "cancel", &v.cancelBtn),
	}
	switch kind {
	case ops.FlowKindFeature:
		binds = append(binds,
			bindWidget(named, "modeMerge", &v.mergeRadio),
			bindWidget(named, "modeSquash", &v.squashRadio),
			bindWidget(named, "modeRebase", &v.rebaseRadio),
		)
	case ops.FlowKindHotfix:
		binds = append(binds, bindWidget(named, "mergeDevelop", &v.developBox))
		fallthrough
	default:
		binds = append(binds,
			bindWidget(named, "fetch", &v.fetchBox),
			bindWidget(named, "createTag", &v.tagBox),
			bindWidget(named, "tagName", &v.tagInput),
		)
	}
	if err := errors.Join(binds...); err != nil {
		return nil, err
	}
	v.headerLabel.SetText(i18n.T(texts.finishHeader))
	v.deleteBox.SetText(i18n.T(texts.deleteBranch))
	v.pushBox.SetText(i18n.T(texts.pushRemove))
	v.deleteBox.SetChecked(true)
	v.hintLabel.Muted = true
	if v.tagInput != nil {
		v.tagBox.SetChecked(true)
		v.tagBox.OnChange = func(bool) { v.refresh() }
		v.tagInput.OnChange = func(string) { v.refresh() }
	}
	if v.developBox != nil {
		v.developBox.SetChecked(true)
	}
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
	p.Body(v.headerLabel, v.textLabel, v.messageLabel)
	p.Hints(v.hintLabel)
	if v.tagInput != nil {
		p.Fields(v.tagInput)
	}
}

func (v *FinishView) tagged() bool { return v.tagInput != nil }

func (v *FinishView) SetKnown(known FinishKnown) {
	v.known = known
	editable := !known.Resuming
	v.messageBox.SetText(ops.DefaultFlowMessage(known.Name))
	v.messageBox.SetEnabled(editable)
	v.deleteBox.SetEnabled(editable)
	v.pushBox.SetEnabled(known.CanPush && editable)
	if v.tagged() {
		v.setTaggedKnown(known, editable)
	} else {
		v.textLabel.SetText(i18n.Tf(v.texts.finishText, known.Develop))
		v.messageLabel.SetText(i18n.T("Dialog.FlowFinish.MessageLabel.Merge"))
		v.rebaseRadio.SetText(i18n.Tf("Dialog.FlowFinish.Rebase", known.Develop))
		v.pushBox.SetChecked(known.CanPush)
		for _, radio := range []*widget.RadioButton{v.mergeRadio, v.squashRadio, v.rebaseRadio} {
			radio.SetEnabled(editable)
		}
	}
	v.refresh()
}

func (v *FinishView) setTaggedKnown(known FinishKnown, editable bool) {
	if v.developBox != nil {
		v.textLabel.SetText(i18n.Tf(v.texts.finishText, known.Master, known.Develop))
		v.fetchBox.SetText(i18n.Tf("Dialog.FlowFinish.Fetch.Hotfix", known.Master))
		v.developBox.SetText(i18n.Tf("Dialog.FlowFinish.MergeDevelop", known.Develop))
		v.developBox.SetEnabled(editable)
	} else {
		v.textLabel.SetText(i18n.Tf(v.texts.finishText, known.Branch, known.Master, known.Develop))
		v.fetchBox.SetText(i18n.Tf("Dialog.FlowFinish.Fetch.Release", known.Develop, known.Master))
	}
	v.messageLabel.SetText(i18n.T("Dialog.FlowFinish.MessageLabel.Tag"))
	v.fetchBox.SetChecked(known.CanFetch)
	v.fetchBox.SetEnabled(known.CanFetch && editable)
	v.tagInput.SetText(known.Tag)
	v.tagBox.SetEnabled(editable)
	v.tagInput.SetEnabled(editable)
}

func (v *FinishView) Model() FinishModel {
	model := FinishModel{
		Message:      v.messageBox.GetText(),
		DeleteBranch: v.deleteBox.IsChecked(),
		Push:         v.pushBox.IsEnabled() && v.pushBox.IsChecked(),
	}
	if !v.tagged() {
		model.Fetch = model.Push
		switch {
		case v.squashRadio.IsSelected():
			model.Integration = ops.FlowSquash
		case v.rebaseRadio.IsSelected():
			model.Integration = ops.FlowRebase
		}
		return model
	}
	model.Fetch = v.fetchBox.IsEnabled() && v.fetchBox.IsChecked()
	model.TagName = strings.TrimSpace(v.tagInput.GetText())
	model.SkipTag = !v.tagBox.IsChecked()
	model.SkipDevelop = v.developBox != nil && !v.developBox.IsChecked()
	return model
}

func (v *FinishView) refresh() {
	model := v.Model()
	switch {
	case v.known.Resuming:
		v.current = Hint{Key: hintWillResume, Args: []any{v.known.Branch}, OK: true}
	case v.tagged() && !model.SkipTag && model.TagName == "":
		v.current = Hint{Key: hintTagRequired}
	case v.tagged() && !model.SkipTag && !validBranchName(model.TagName):
		v.current = Hint{Key: hintTagInvalid, Args: []any{model.TagName}}
	default:
		v.current = Hint{OK: true}
	}
	v.hintLabel.SetText(hintText(v.current))
	v.okBtn.SetEnabled(v.current.OK)
}

func (v *FinishView) confirm() {
	if v.current.OK && v.OnOK != nil {
		v.OnOK(v.Model())
	}
}

func (v *FinishView) cancel() {
	if v.OnCancel != nil {
		v.OnCancel()
	}
}
