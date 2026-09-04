package commit

import (
	"errors"
	"fmt"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/dialogs"
)

const dialogName = "commit"

var ErrWidgetMissing = errors.New("commit: named widget missing")

var loadDialog = dialogs.Load

type ctrlEnterModal struct {
	*widget.Dialog
	confirm func()
}

func (m *ctrlEnterModal) HandleInputBinding(code widget.KeyCode, mod widget.KeyMod) bool {
	if code == widget.KeyEnter && mod&widget.ModCtrl != 0 {
		if m.confirm != nil {
			m.confirm()
		}
		return true
	}
	return m.Dialog.HandleInputBinding(code, mod)
}

type View struct {
	dlg   *widget.Dialog
	modal *ctrlEnterModal

	stagedLabel *widget.Label
	message     *widget.TextBox
	amendCheck  *widget.CheckBox
	okBtn       *widget.Button
	cancelBtn   *widget.Button

	eng widget.ModalShower

	lastMessage string
	draft       string

	OnOK     func(Model)
	OnCancel func()
}

func NewView(eng widget.ModalShower, initial Model) (*View, error) {
	dlg, named, err := loadDialog(dialogName, i18n.T("Dialog.Commit.Title"))
	if err != nil {
		return nil, err
	}
	v := &View{dlg: dlg, eng: eng}
	if err := v.bind(named); err != nil {
		return nil, err
	}
	v.modal = &ctrlEnterModal{Dialog: dlg, confirm: v.confirm}
	v.apply(initial)
	v.wire()
	return v, nil
}

func (v *View) Dialog() widget.ModalWidget { return v.modal }

func (v *View) bind(named map[string]widget.Widget) error {
	var ok bool
	if v.stagedLabel, ok = named["staged"].(*widget.Label); !ok {
		return fmt.Errorf("%w: staged", ErrWidgetMissing)
	}
	if v.message, ok = named["message"].(*widget.TextBox); !ok {
		return fmt.Errorf("%w: message", ErrWidgetMissing)
	}
	if v.amendCheck, ok = named["amend"].(*widget.CheckBox); !ok {
		return fmt.Errorf("%w: amend", ErrWidgetMissing)
	}
	if v.okBtn, ok = named["ok"].(*widget.Button); !ok {
		return fmt.Errorf("%w: ok", ErrWidgetMissing)
	}
	if v.cancelBtn, ok = named["cancel"].(*widget.Button); !ok {
		return fmt.Errorf("%w: cancel", ErrWidgetMissing)
	}
	return nil
}

func (v *View) apply(m Model) {
	v.lastMessage = m.LastMessage
	v.draft = m.Message
	v.amendCheck.SetChecked(m.Amend)
	if m.Amend {
		v.message.SetText(m.LastMessage)
	} else {
		v.message.SetText(m.Message)
	}
	v.stagedLabel.SetText(i18n.Tf("Dialog.Commit.Staged", m.Staged))
	v.refresh()
}

func (v *View) wire() {
	v.message.OnChange = func(string) { v.refresh() }
	v.amendCheck.OnChange = func(bool) { v.onAmendToggled() }
	v.okBtn.OnClick = v.confirm
	v.cancelBtn.OnClick = v.cancel
	v.dlg.CancelAction = v.cancel
}

func (v *View) onAmendToggled() {
	if v.amendCheck.IsChecked() {
		v.draft = v.message.GetText()
		v.message.SetText(v.lastMessage)
	} else {
		v.message.SetText(v.draft)
	}
	v.refresh()
}

func (v *View) refresh() {
	v.okBtn.SetEnabled(v.request().CanConfirm())
}

func (v *View) request() Model {
	return Model{
		Message:     v.message.GetText(),
		Amend:       v.amendCheck.IsChecked(),
		LastMessage: v.lastMessage,
	}
}

func (v *View) confirm() {
	m := v.request()
	if !m.CanConfirm() {
		return
	}
	if v.OnOK != nil {
		v.OnOK(m)
	}
}

func (v *View) cancel() {
	if v.OnCancel != nil {
		v.OnCancel()
	}
}
