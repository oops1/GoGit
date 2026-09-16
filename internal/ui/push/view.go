package push

import (
	"errors"
	"fmt"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/dialogs"
	"github.com/oops1/gogit/internal/ui/style"
)

const dialogName = "push"

var ErrWidgetMissing = errors.New("push: named widget missing")

var loadDialog = dialogs.Load

type Known struct {
	Branch string
	Remote string
}

type View struct {
	dlg         *widget.Dialog
	targetLabel *widget.Label
	bypassCheck *widget.CheckBox
	okBtn       *widget.Button
	cancelBtn   *widget.Button

	OnOK     func(noVerify bool)
	OnCancel func()
}

func NewView() (*View, error) {
	dlg, named, err := loadDialog(dialogName, i18n.T("Dialog.Push.Title"))
	if err != nil {
		return nil, err
	}
	v := &View{dlg: dlg}
	if err := v.bind(named); err != nil {
		return nil, err
	}
	v.wire()
	return v, nil
}

func (v *View) Dialog() *widget.Dialog { return v.dlg }

func (v *View) Restyle(t *widget.Theme) {
	p := style.Of(t)
	p.Quiet(v.cancelBtn)
	p.Primary(v.okBtn)
	p.Body(v.targetLabel)
}

func (v *View) bind(named map[string]widget.Widget) error {
	var ok bool
	if v.targetLabel, ok = named["target"].(*widget.Label); !ok {
		return fmt.Errorf("%w: target", ErrWidgetMissing)
	}
	if v.bypassCheck, ok = named["bypassHooks"].(*widget.CheckBox); !ok {
		return fmt.Errorf("%w: bypassHooks", ErrWidgetMissing)
	}
	if v.okBtn, ok = named["ok"].(*widget.Button); !ok {
		return fmt.Errorf("%w: ok", ErrWidgetMissing)
	}
	if v.cancelBtn, ok = named["cancel"].(*widget.Button); !ok {
		return fmt.Errorf("%w: cancel", ErrWidgetMissing)
	}
	return nil
}

func (v *View) wire() {
	v.okBtn.OnClick = v.confirm
	v.cancelBtn.OnClick = v.cancel
	v.dlg.DefaultAction = v.confirm
	v.dlg.CancelAction = v.cancel
}

func (v *View) SetKnown(known Known) {
	v.targetLabel.SetText(i18n.Tf("Dialog.Push.Target", known.Branch, known.Remote))
}

func (v *View) NoVerify() bool { return v.bypassCheck.IsChecked() }

func (v *View) confirm() {
	if v.OnOK != nil {
		v.OnOK(v.NoVerify())
	}
}

func (v *View) cancel() {
	if v.OnCancel != nil {
		v.OnCancel()
	}
}
