package unlock

import (
	"errors"
	"fmt"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/dialogs"
)

const dialogName = "unlock"

var ErrWidgetMissing = errors.New("unlock: named widget missing")

var loadDialog = dialogs.Load

type Request struct{ Retry bool }

type Result struct{ Password []byte }

type View struct {
	dlg           *widget.Dialog
	hintLabel     *widget.Label
	passwordInput *widget.TextInput
	okBtn         *widget.Button
	cancelBtn     *widget.Button

	eng      widget.ModalShower
	password []byte

	OnOK     func(Result)
	OnCancel func()
}

func NewView(eng widget.ModalShower, req Request) (*View, error) {
	dlg, named, err := loadDialog(dialogName, i18n.T("Dialog.Unlock.Title"))
	if err != nil {
		return nil, err
	}
	v := &View{dlg: dlg, eng: eng}
	if err := v.bind(named); err != nil {
		return nil, err
	}
	v.applyRequest(req)
	v.wire()
	return v, nil
}

func (v *View) Dialog() *widget.Dialog { return v.dlg }

func (v *View) bind(named map[string]widget.Widget) error {
	var ok bool
	if v.hintLabel, ok = named["hint"].(*widget.Label); !ok {
		return fmt.Errorf("%w: hint", ErrWidgetMissing)
	}
	if v.passwordInput, ok = named["password"].(*widget.TextInput); !ok {
		return fmt.Errorf("%w: password", ErrWidgetMissing)
	}
	if v.okBtn, ok = named["ok"].(*widget.Button); !ok {
		return fmt.Errorf("%w: ok", ErrWidgetMissing)
	}
	if v.cancelBtn, ok = named["cancel"].(*widget.Button); !ok {
		return fmt.Errorf("%w: cancel", ErrWidgetMissing)
	}
	return nil
}

func (v *View) applyRequest(req Request) {
	v.hintLabel.SetText(i18n.T("Dialog.Unlock.Wrong"))
	v.hintLabel.SetVisible(req.Retry)
}

func (v *View) wire() {
	v.passwordInput.OnChange = func(string) { v.hintLabel.SetVisible(false) }
	v.okBtn.OnClick = v.confirm
	v.cancelBtn.OnClick = v.cancel
	v.dlg.DefaultAction = v.confirm
	v.dlg.CancelAction = v.cancel
}

func (v *View) confirm() {
	v.password = v.passwordInput.TakeSecret()
	result := Result{Password: append([]byte(nil), v.password...)}
	if v.OnOK != nil {
		v.OnOK(result)
	}
	clear(v.password)
	v.password = nil
}

func (v *View) cancel() {
	v.passwordInput.WipeSecret()
	if v.OnCancel != nil {
		v.OnCancel()
	}
}
