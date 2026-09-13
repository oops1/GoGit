package flow

import (
	"errors"
	"slices"
	"strings"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/style"
)

const (
	startDialogName  = "flow_start"
	hintNameInvalid  = "Dialog.FlowStart.Hint.NameInvalid"
	hintNameTaken    = "Dialog.FlowStart.Hint.NameTaken"
	hintBaseRequired = "Dialog.FlowStart.Hint.BaseRequired"
	resultingBranch  = "Dialog.FlowStart.Resulting"
)

type StartKnown struct {
	Base   string
	Bases  []string
	Prefix string
	Taken  []string
}

type StartModel struct {
	Name string
	Base string
}

func ValidateStart(kind string, model StartModel, known StartKnown) Hint {
	name := strings.TrimSpace(model.Name)
	branch := known.Prefix + name
	switch {
	case name == "":
		return Hint{Key: kindKeys[kind].nameRequired}
	case !validBranchName(branch):
		return Hint{Key: hintNameInvalid, Args: []any{name}}
	case slices.Contains(known.Taken, branch):
		return Hint{Key: hintNameTaken, Args: []any{branch}}
	case model.Base == "":
		return Hint{Key: hintBaseRequired}
	}
	return Hint{OK: true}
}

type StartView struct {
	dlg         *widget.Dialog
	headerLabel *widget.Label
	textLabel   *widget.Label
	nameLabel   *widget.Label
	nameBox     *widget.TextInput
	resultLabel *widget.Label
	baseLabel   *widget.Label
	baseList    *widget.Dropdown
	hintLabel   *widget.Label
	okBtn       *widget.Button
	cancelBtn   *widget.Button

	kind    string
	texts   kindTexts
	known   StartKnown
	current Hint

	OnOK     func(StartModel)
	OnCancel func()
}

func NewStartView(kind string) (*StartView, error) {
	texts := kindKeys[kind]
	dlg, named, err := loadDialog(startDialogName, i18n.T(texts.startTitle))
	if err != nil {
		return nil, err
	}
	v := &StartView{dlg: dlg, kind: kind, texts: texts}
	if err := errors.Join(
		bindWidget(named, "header", &v.headerLabel),
		bindWidget(named, "text", &v.textLabel),
		bindWidget(named, "nameLabel", &v.nameLabel),
		bindWidget(named, "name", &v.nameBox),
		bindWidget(named, "result", &v.resultLabel),
		bindWidget(named, "baseLabel", &v.baseLabel),
		bindWidget(named, "base", &v.baseList),
		bindWidget(named, "hint", &v.hintLabel),
		bindWidget(named, "ok", &v.okBtn),
		bindWidget(named, "cancel", &v.cancelBtn),
	); err != nil {
		return nil, err
	}
	v.headerLabel.SetText(i18n.T(texts.startHeader))
	v.nameLabel.SetText(i18n.T(texts.nameLabel))
	v.hintLabel.Muted = true
	v.resultLabel.Muted = true
	v.nameBox.OnChange = func(string) { v.refresh() }
	v.baseList.OnChange = func(int, string) { v.refresh() }
	v.okBtn.OnClick = v.confirm
	v.cancelBtn.OnClick = v.cancel
	v.dlg.CancelAction = v.cancel
	v.refresh()
	return v, nil
}

func (v *StartView) Dialog() *widget.Dialog { return v.dlg }

func (v *StartView) Restyle(t *widget.Theme) {
	p := style.Of(t)
	p.Fields(v.nameBox)
	p.Lists(v.baseList)
	p.Quiet(v.cancelBtn)
	p.Primary(v.okBtn)
	p.Body(v.headerLabel, v.textLabel, v.nameLabel, v.baseLabel)
	p.Hints(v.resultLabel, v.hintLabel)
}

func (v *StartView) SetKnown(known StartKnown) {
	v.known = known
	v.baseList.SetItems(known.Bases)
	if at := slices.Index(known.Bases, known.Base); at >= 0 {
		v.baseList.SetSelected(at)
	}
	v.refresh()
}

func (v *StartView) Model() StartModel {
	return StartModel{Name: strings.TrimSpace(v.nameBox.GetText()), Base: v.baseList.SelectedText()}
}

func (v *StartView) refresh() {
	model := v.Model()
	v.current = ValidateStart(v.kind, model, v.known)
	v.textLabel.SetText(i18n.Tf(v.texts.startText, model.Base))
	v.resultLabel.SetText(i18n.Tf(resultingBranch, v.known.Prefix+model.Name))
	v.hintLabel.SetText(hintText(v.current))
	v.okBtn.SetEnabled(v.current.OK)
}

func (v *StartView) confirm() {
	if v.current.OK && v.OnOK != nil {
		v.OnOK(v.Model())
	}
}

func (v *StartView) cancel() {
	if v.OnCancel != nil {
		v.OnCancel()
	}
}

func hintText(hint Hint) string {
	if hint.Key == "" {
		return ""
	}
	return i18n.Tf(hint.Key, hint.Args...)
}
