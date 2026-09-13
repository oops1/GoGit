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
	startDialogName = "flow_start"
	hintNameInvalid = "Dialog.FlowStart.Hint.NameInvalid"
	hintNameTaken   = "Dialog.FlowStart.Hint.NameTaken"
	hintWillStart   = "Dialog.FlowStart.Hint.WillStart"
)

type StartKnown struct {
	Base   string
	Prefix string
	Taken  []string
}

type StartModel struct {
	Name string
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
	}
	return Hint{Key: hintWillStart, Args: []any{branch, known.Base}, OK: true}
}

type StartView struct {
	dlg       *widget.Dialog
	nameLabel *widget.Label
	nameBox   *widget.TextInput
	hintLabel *widget.Label
	okBtn     *widget.Button
	cancelBtn *widget.Button

	kind    string
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
	v := &StartView{dlg: dlg, kind: kind}
	if err := errors.Join(
		bindWidget(named, "nameLabel", &v.nameLabel),
		bindWidget(named, "name", &v.nameBox),
		bindWidget(named, "hint", &v.hintLabel),
		bindWidget(named, "ok", &v.okBtn),
		bindWidget(named, "cancel", &v.cancelBtn),
	); err != nil {
		return nil, err
	}
	v.nameLabel.SetText(i18n.T(texts.nameLabel))
	v.hintLabel.Muted = true
	v.nameBox.OnChange = func(string) { v.refresh() }
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
	p.Quiet(v.cancelBtn)
	p.Primary(v.okBtn)
	p.Body(v.nameLabel)
	p.Hints(v.hintLabel)
}

func (v *StartView) SetKnown(known StartKnown) {
	v.known = known
	v.refresh()
}

func (v *StartView) Model() StartModel {
	return StartModel{Name: strings.TrimSpace(v.nameBox.GetText())}
}

func (v *StartView) refresh() {
	v.current = ValidateStart(v.kind, v.Model(), v.known)
	v.hintLabel.SetText(i18n.Tf(v.current.Key, v.current.Args...))
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
