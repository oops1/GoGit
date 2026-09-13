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
	startDialogName     = "flow_start_release"
	hintVersionRequired = "Dialog.FlowStart.Hint.VersionRequired"
	hintVersionInvalid  = "Dialog.FlowStart.Hint.VersionInvalid"
	hintVersionTaken    = "Dialog.FlowStart.Hint.VersionTaken"
	hintWillStart       = "Dialog.FlowStart.Hint.WillStart"
)

type StartKnown struct {
	Develop string
	Prefix  string
	Taken   []string
}

type StartModel struct {
	Version string
}

func ValidateStart(model StartModel, known StartKnown) Hint {
	version := strings.TrimSpace(model.Version)
	branch := known.Prefix + version
	switch {
	case version == "":
		return Hint{Key: hintVersionRequired}
	case !validBranchName(branch):
		return Hint{Key: hintVersionInvalid, Args: []any{version}}
	case slices.Contains(known.Taken, branch):
		return Hint{Key: hintVersionTaken, Args: []any{branch}}
	}
	return Hint{Key: hintWillStart, Args: []any{branch, known.Develop}, OK: true}
}

type StartView struct {
	dlg          *widget.Dialog
	versionLabel *widget.Label
	versionBox   *widget.TextInput
	hintLabel    *widget.Label
	okBtn        *widget.Button
	cancelBtn    *widget.Button

	known   StartKnown
	current Hint

	OnOK     func(StartModel)
	OnCancel func()
}

func NewStartView() (*StartView, error) {
	dlg, named, err := loadDialog(startDialogName, i18n.T("Dialog.FlowStart.Title"))
	if err != nil {
		return nil, err
	}
	v := &StartView{dlg: dlg}
	if err := errors.Join(
		bindWidget(named, "versionLabel", &v.versionLabel),
		bindWidget(named, "version", &v.versionBox),
		bindWidget(named, "hint", &v.hintLabel),
		bindWidget(named, "ok", &v.okBtn),
		bindWidget(named, "cancel", &v.cancelBtn),
	); err != nil {
		return nil, err
	}
	v.hintLabel.Muted = true
	v.versionBox.OnChange = func(string) { v.refresh() }
	v.okBtn.OnClick = v.confirm
	v.cancelBtn.OnClick = v.cancel
	v.dlg.CancelAction = v.cancel
	v.refresh()
	return v, nil
}

func (v *StartView) Dialog() *widget.Dialog { return v.dlg }

func (v *StartView) Restyle(t *widget.Theme) {
	p := style.Of(t)
	p.Fields(v.versionBox)
	p.Quiet(v.cancelBtn)
	p.Primary(v.okBtn)
	p.Body(v.versionLabel)
	p.Hints(v.hintLabel)
}

func (v *StartView) SetKnown(known StartKnown) {
	v.known = known
	v.refresh()
}

func (v *StartView) Model() StartModel {
	return StartModel{Version: strings.TrimSpace(v.versionBox.GetText())}
}

func (v *StartView) refresh() {
	v.current = ValidateStart(v.Model(), v.known)
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
