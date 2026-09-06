package credentials

import (
	"errors"
	"fmt"
	"image/color"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/dialogs"
)

const dialogName = "credentials"

var ErrWidgetMissing = errors.New("credentials: named widget missing")

var loadDialog = dialogs.Load

type Request struct {
	Resource       string
	Username       string
	Retry          bool
	RememberTarget string
}

type Result struct {
	Username string
	Secret   []byte
	Remember bool
}

type View struct {
	dlg           *widget.Dialog
	resourceText  *widget.Label
	usernameInput *widget.TextInput
	secretInput   *widget.TextInput
	rememberCheck *widget.CheckBox
	saveToLabel   *widget.Label
	hintLabel     *widget.Label
	okBtn         *widget.Button
	cancelBtn     *widget.Button

	eng    widget.ModalShower
	secret []byte

	OnOK     func(Result)
	OnCancel func()
}

func NewView(eng widget.ModalShower, req Request) (*View, error) {
	dlg, named, err := loadDialog(dialogName, i18n.T("Dialog.Credentials.Title"))
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

func (v *View) SetErrorColor(c color.RGBA) {
	v.hintLabel.TextColor = c
}

func (v *View) bind(named map[string]widget.Widget) error {
	var ok bool
	if v.resourceText, ok = named["resource"].(*widget.Label); !ok {
		return fmt.Errorf("%w: resource", ErrWidgetMissing)
	}
	if v.usernameInput, ok = named["username"].(*widget.TextInput); !ok {
		return fmt.Errorf("%w: username", ErrWidgetMissing)
	}
	if v.secretInput, ok = named["secret"].(*widget.TextInput); !ok {
		return fmt.Errorf("%w: secret", ErrWidgetMissing)
	}
	if v.rememberCheck, ok = named["remember"].(*widget.CheckBox); !ok {
		return fmt.Errorf("%w: remember", ErrWidgetMissing)
	}
	if v.saveToLabel, ok = named["saveTo"].(*widget.Label); !ok {
		return fmt.Errorf("%w: saveTo", ErrWidgetMissing)
	}
	if v.hintLabel, ok = named["hint"].(*widget.Label); !ok {
		return fmt.Errorf("%w: hint", ErrWidgetMissing)
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
	v.resourceText.SetText(req.Resource)
	v.usernameInput.SetText(req.Username)
	v.hintLabel.SetText(i18n.T("Dialog.Credentials.Error.Empty"))
	v.hintLabel.SetVisible(req.Retry)
	v.saveToLabel.SetText(i18n.Tf("Dialog.Credentials.SaveTo", req.RememberTarget))
	v.saveToLabel.SetVisible(req.RememberTarget != "")
}

func (v *View) wire() {
	v.secretInput.OnChange = v.onSecretChanged
	v.okBtn.OnClick = v.confirm
	v.cancelBtn.OnClick = v.cancel
	v.dlg.DefaultAction = v.confirm
	v.dlg.CancelAction = v.cancel
}

func (v *View) onSecretChanged(text string) {
	if text != "" {
		v.hintLabel.SetVisible(false)
	}
}

func (v *View) confirm() {
	v.secret = v.secretInput.TakeSecret()
	if len(v.secret) == 0 {
		v.hintLabel.SetVisible(true)
		return
	}
	result := Result{
		Username: v.usernameInput.GetText(),
		Secret:   append([]byte(nil), v.secret...),
		Remember: v.rememberCheck.IsChecked(),
	}
	if v.OnOK != nil {
		v.OnOK(result)
	}
	clear(v.secret)
	v.secret = nil
}

func (v *View) cancel() {
	v.secretInput.WipeSecret()
	if v.OnCancel != nil {
		v.OnCancel()
	}
}
