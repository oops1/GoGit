package stash

import (
	"fmt"
	"strings"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/style"
)

const saveDialogName = "stash_save"

type SaveRequest struct {
	Message          string
	IncludeUntracked bool
	KeepIndex        bool
}

type SaveView struct {
	dlg           *widget.Dialog
	selectedFiles int
	scopeLabel    *widget.Label
	messageLabel  *widget.Label
	messageBox    *widget.TextInput
	untrackedBox  *widget.CheckBox
	keepIndexBox  *widget.CheckBox
	okBtn         *widget.Button
	cancelBtn     *widget.Button

	OnOK     func(SaveRequest)
	OnCancel func()
}

func NewSaveView(selectedFiles int) (*SaveView, error) {
	title := i18n.T("Dialog.SaveStash.Title")
	if selectedFiles > 0 {
		title = i18n.T("Dialog.StashSelection.Title")
	}
	dlg, named, err := loadDialog(saveDialogName, title)
	if err != nil {
		return nil, err
	}
	v := &SaveView{dlg: dlg, selectedFiles: selectedFiles}
	if err := v.bind(named); err != nil {
		return nil, err
	}
	if selectedFiles > 0 {
		v.scopeLabel.SetText(i18n.Tf("Dialog.SaveStash.Scope.Selection", selectedFiles))
	}
	v.untrackedBox.SetVisible(selectedFiles == 0)
	v.okBtn.OnClick = v.confirm
	v.cancelBtn.OnClick = v.cancel
	v.dlg.DefaultAction = v.confirm
	v.dlg.CancelAction = v.cancel
	return v, nil
}

func (v *SaveView) Dialog() *widget.Dialog { return v.dlg }

func (v *SaveView) Restyle(t *widget.Theme) {
	p := style.Of(t)
	p.Fields(v.messageBox)
	p.Quiet(v.cancelBtn)
	p.Primary(v.okBtn)
	p.Body(v.scopeLabel, v.messageLabel)
}

func (v *SaveView) bind(named map[string]widget.Widget) error {
	var ok bool
	if v.scopeLabel, ok = named["scopeLabel"].(*widget.Label); !ok {
		return fmt.Errorf("%w: scopeLabel", ErrWidgetMissing)
	}
	if v.messageLabel, ok = named["messageLabel"].(*widget.Label); !ok {
		return fmt.Errorf("%w: messageLabel", ErrWidgetMissing)
	}
	if v.messageBox, ok = named["message"].(*widget.TextInput); !ok {
		return fmt.Errorf("%w: message", ErrWidgetMissing)
	}
	if v.untrackedBox, ok = named["includeUntracked"].(*widget.CheckBox); !ok {
		return fmt.Errorf("%w: includeUntracked", ErrWidgetMissing)
	}
	if v.keepIndexBox, ok = named["keepIndex"].(*widget.CheckBox); !ok {
		return fmt.Errorf("%w: keepIndex", ErrWidgetMissing)
	}
	if v.okBtn, ok = named["ok"].(*widget.Button); !ok {
		return fmt.Errorf("%w: ok", ErrWidgetMissing)
	}
	if v.cancelBtn, ok = named["cancel"].(*widget.Button); !ok {
		return fmt.Errorf("%w: cancel", ErrWidgetMissing)
	}
	return nil
}

func (v *SaveView) Request() SaveRequest {
	return SaveRequest{
		Message:          strings.TrimSpace(v.messageBox.GetText()),
		IncludeUntracked: v.selectedFiles == 0 && v.untrackedBox.IsChecked(),
		KeepIndex:        v.keepIndexBox.IsChecked(),
	}
}

func (v *SaveView) confirm() {
	if v.OnOK != nil {
		v.OnOK(v.Request())
	}
}

func (v *SaveView) cancel() {
	if v.OnCancel != nil {
		v.OnCancel()
	}
}
