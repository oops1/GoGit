package masterpassword

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"unicode"
	"unicode/utf8"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/dialogs"
	"github.com/oops1/gogit/internal/ui/style"
)

const (
	dialogName        = "master_password"
	MinPasswordLength = 8
)

var ErrWidgetMissing = errors.New("masterpassword: named widget missing")

var loadDialog = dialogs.Load

type Request struct {
	Change       bool
	Backup       bool
	WrongCurrent bool
}

type Result struct {
	Current  []byte
	Password []byte
}

func (r *Result) Wipe() {
	clear(r.Current)
	clear(r.Password)
	r.Current = nil
	r.Password = nil
}

type View struct {
	dlg           *widget.Dialog
	promptLabel   *widget.Label
	currentLabel  *widget.Label
	strengthLabel *widget.Label
	hintLabel     *widget.Label
	currentInput  *widget.TextInput
	passwordInput *widget.TextInput
	confirmInput  *widget.TextInput
	okBtn         *widget.Button
	cancelBtn     *widget.Button

	change bool

	OnOK     func(Result)
	OnCancel func()
}

func NewView(req Request) (*View, error) {
	dlg, named, err := loadDialog(dialogName, i18n.T("Dialog.MasterPassword.Title"))
	if err != nil {
		return nil, err
	}
	v := &View{dlg: dlg}
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
	p.Fields(v.currentInput, v.passwordInput, v.confirmInput)
	p.Quiet(v.cancelBtn)
	p.Primary(v.okBtn)
	p.Hints(v.strengthLabel)
}

func (v *View) bind(named map[string]widget.Widget) error {
	labels := map[string]**widget.Label{
		"prompt":       &v.promptLabel,
		"currentLabel": &v.currentLabel,
		"strength":     &v.strengthLabel,
		"hint":         &v.hintLabel,
	}
	for name, target := range labels {
		label, ok := named[name].(*widget.Label)
		if !ok {
			return fmt.Errorf("%w: %s", ErrWidgetMissing, name)
		}
		*target = label
	}
	inputs := map[string]**widget.TextInput{
		"current":  &v.currentInput,
		"password": &v.passwordInput,
		"confirm":  &v.confirmInput,
	}
	for name, target := range inputs {
		input, ok := named[name].(*widget.TextInput)
		if !ok {
			return fmt.Errorf("%w: %s", ErrWidgetMissing, name)
		}
		*target = input
	}
	buttons := map[string]**widget.Button{
		"ok":     &v.okBtn,
		"cancel": &v.cancelBtn,
	}
	for name, target := range buttons {
		button, ok := named[name].(*widget.Button)
		if !ok {
			return fmt.Errorf("%w: %s", ErrWidgetMissing, name)
		}
		*target = button
	}
	return nil
}

func promptKey(req Request) string {
	switch {
	case req.Change:
		return "Dialog.MasterPassword.PromptChange"
	case req.Backup:
		return "Dialog.MasterPassword.PromptBackup"
	default:
		return "Dialog.MasterPassword.PromptSet"
	}
}

func (v *View) applyRequest(req Request) {
	v.change = req.Change
	v.promptLabel.SetText(i18n.T(promptKey(req)))
	v.currentLabel.SetVisible(req.Change)
	v.currentInput.SetVisible(req.Change)
	v.strengthLabel.SetText("")
	v.hintLabel.SetVisible(false)
	if req.Change && req.WrongCurrent {
		v.showHint(i18n.T("Dialog.MasterPassword.WrongCurrent"))
	}
}

func (v *View) wire() {
	v.currentInput.OnChange = func(string) { v.hintLabel.SetVisible(false) }
	v.passwordInput.OnChange = func(text string) {
		v.hintLabel.SetVisible(false)
		v.strengthLabel.SetText(StrengthText(text))
	}
	v.confirmInput.OnChange = func(string) { v.hintLabel.SetVisible(false) }
	v.okBtn.OnClick = v.confirm
	v.cancelBtn.OnClick = v.cancel
	v.dlg.DefaultAction = v.confirm
	v.dlg.CancelAction = v.cancel
}

func (v *View) showHint(text string) {
	v.hintLabel.SetText(text)
	v.hintLabel.SetVisible(true)
}

func Validate(change bool, current, password, repeat []byte) string {
	switch {
	case change && len(current) == 0:
		return i18n.T("Dialog.MasterPassword.NeedCurrent")
	case utf8.RuneCount(password) < MinPasswordLength:
		return i18n.Tf("Dialog.MasterPassword.TooShort", MinPasswordLength)
	case subtle.ConstantTimeCompare(password, repeat) != 1:
		return i18n.T("Dialog.MasterPassword.Mismatch")
	default:
		return ""
	}
}

func StrengthText(password string) string {
	if password == "" {
		return ""
	}
	return i18n.T(strengthKey(password))
}

func strengthKey(password string) string {
	var lower, upper, digit, other bool
	for _, r := range password {
		switch {
		case unicode.IsLower(r):
			lower = true
		case unicode.IsUpper(r):
			upper = true
		case unicode.IsDigit(r):
			digit = true
		default:
			other = true
		}
	}
	score := 0
	for _, present := range []bool{lower, upper, digit, other} {
		if present {
			score++
		}
	}
	length := utf8.RuneCountInString(password)
	if length >= 12 {
		score++
	}
	if length >= 16 {
		score++
	}
	switch {
	case length < MinPasswordLength || score <= 2:
		return "Dialog.MasterPassword.Strength.Weak"
	case score <= 4:
		return "Dialog.MasterPassword.Strength.Fair"
	default:
		return "Dialog.MasterPassword.Strength.Strong"
	}
}

func (v *View) confirm() {
	current := v.currentInput.TakeSecret()
	password := v.passwordInput.TakeSecret()
	repeat := v.confirmInput.TakeSecret()
	defer clear(repeat)
	v.strengthLabel.SetText("")
	if hint := Validate(v.change, current, password, repeat); hint != "" {
		clear(current)
		clear(password)
		v.showHint(hint)
		return
	}
	result := Result{Current: current, Password: password}
	if v.OnOK != nil {
		v.OnOK(Result{Current: append([]byte(nil), current...), Password: append([]byte(nil), password...)})
	}
	result.Wipe()
}

func (v *View) cancel() {
	v.currentInput.WipeSecret()
	v.passwordInput.WipeSecret()
	v.confirmInput.WipeSecret()
	if v.OnCancel != nil {
		v.OnCancel()
	}
}
