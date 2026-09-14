package keypassphrase

import (
	"errors"
	"fmt"
	"image/color"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/dialogs"
	"github.com/oops1/gogit/internal/ui/style"
)

const dialogName = "keypassphrase"

var ErrWidgetMissing = errors.New("keypassphrase: named widget missing")

var loadDialog = dialogs.Load

type Request struct {
	Host  string
	Path  string
	Retry bool
}

type Result struct {
	Passphrase []byte
	Remember   bool
}

type View struct {
	dlg             *widget.Dialog
	keyText         *widget.Label
	hostText        *widget.Label
	passphraseInput *widget.TextInput
	rememberCheck   *widget.CheckBox
	hintLabel       *widget.Label
	okBtn           *widget.Button
	cancelBtn       *widget.Button

	eng        widget.ModalShower
	passphrase []byte

	OnOK     func(Result)
	OnCancel func()
}

func NewView(eng widget.ModalShower, req Request) (*View, error) {
	dlg, named, err := loadDialog(dialogName, i18n.T("Dialog.KeyPassphrase.Title"))
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

func (v *View) Restyle(t *widget.Theme) {
	p := style.Of(t)
	p.Fields(v.passphraseInput)
	p.Quiet(v.cancelBtn)
	p.Primary(v.okBtn)
	p.Body(v.keyText)
	p.Hints(v.hostText, v.hintLabel)
}

func (v *View) SetErrorColor(c color.RGBA) {
	v.hintLabel.TextColor = c
}

func (v *View) bind(named map[string]widget.Widget) error {
	var ok bool
	if v.keyText, ok = named["key"].(*widget.Label); !ok {
		return fmt.Errorf("%w: key", ErrWidgetMissing)
	}
	if v.hostText, ok = named["host"].(*widget.Label); !ok {
		return fmt.Errorf("%w: host", ErrWidgetMissing)
	}
	if v.passphraseInput, ok = named["passphrase"].(*widget.TextInput); !ok {
		return fmt.Errorf("%w: passphrase", ErrWidgetMissing)
	}
	if v.rememberCheck, ok = named["remember"].(*widget.CheckBox); !ok {
		return fmt.Errorf("%w: remember", ErrWidgetMissing)
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
	v.keyText.SetText(req.Path)
	v.hostText.SetText(i18n.Tf("Dialog.KeyPassphrase.Host", req.Host))
	v.hintLabel.SetText(i18n.T("Dialog.KeyPassphrase.Wrong"))
	v.hintLabel.SetVisible(req.Retry)
}

func (v *View) wire() {
	v.passphraseInput.OnChange = func(string) { v.hintLabel.SetVisible(false) }
	v.okBtn.OnClick = v.confirm
	v.cancelBtn.OnClick = v.cancel
	v.dlg.DefaultAction = v.confirm
	v.dlg.CancelAction = v.cancel
}

func (v *View) confirm() {
	v.passphrase = v.passphraseInput.TakeSecret()
	result := Result{
		Passphrase: append([]byte(nil), v.passphrase...),
		Remember:   v.rememberCheck.IsChecked(),
	}
	if v.OnOK != nil {
		v.OnOK(result)
	}
	clear(v.passphrase)
	v.passphrase = nil
}

func (v *View) cancel() {
	v.passphraseInput.WipeSecret()
	if v.OnCancel != nil {
		v.OnCancel()
	}
}
