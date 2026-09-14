package switchchanges

import (
	"errors"
	"fmt"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/config"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/dialogs"
	"github.com/oops1/gogit/internal/ui/style"
)

const dialogName = "switch_changes"

var ErrWidgetMissing = errors.New("switchchanges: named widget missing")

var loadDialog = dialogs.Load

type View struct {
	dlg           *widget.Dialog
	messageLabel  *widget.Label
	pathsLabel    *widget.Label
	modeStash     *widget.RadioButton
	modeMerge     *widget.RadioButton
	modeOverwrite *widget.RadioButton
	rememberBox   *widget.CheckBox
	rememberHint  *widget.Label
	okBtn         *widget.Button
	cancelBtn     *widget.Button

	OnChoose func(Choice)
	OnCancel func()
}

func NewView() (*View, error) {
	dlg, named, err := loadDialog(dialogName, i18n.T("Dialog.SwitchChanges.Title"))
	if err != nil {
		return nil, err
	}
	v := &View{dlg: dlg}
	if err := v.bind(named); err != nil {
		return nil, err
	}
	v.rememberHint.Muted = true
	v.okBtn.OnClick = v.confirm
	v.cancelBtn.OnClick = v.cancel
	v.dlg.DefaultAction = v.confirm
	v.dlg.CancelAction = v.cancel
	return v, nil
}

func (v *View) Dialog() *widget.Dialog { return v.dlg }

func (v *View) Restyle(t *widget.Theme) {
	p := style.Of(t)
	p.Quiet(v.cancelBtn)
	p.Primary(v.okBtn)
	p.Body(v.messageLabel, v.pathsLabel)
	p.Hints(v.rememberHint)
}

func (v *View) bind(named map[string]widget.Widget) error {
	var ok bool
	if v.messageLabel, ok = named["message"].(*widget.Label); !ok {
		return fmt.Errorf("%w: message", ErrWidgetMissing)
	}
	if v.pathsLabel, ok = named["paths"].(*widget.Label); !ok {
		return fmt.Errorf("%w: paths", ErrWidgetMissing)
	}
	if v.modeStash, ok = named["modeStash"].(*widget.RadioButton); !ok {
		return fmt.Errorf("%w: modeStash", ErrWidgetMissing)
	}
	if v.modeMerge, ok = named["modeMerge"].(*widget.RadioButton); !ok {
		return fmt.Errorf("%w: modeMerge", ErrWidgetMissing)
	}
	if v.modeOverwrite, ok = named["modeOverwrite"].(*widget.RadioButton); !ok {
		return fmt.Errorf("%w: modeOverwrite", ErrWidgetMissing)
	}
	if v.rememberBox, ok = named["remember"].(*widget.CheckBox); !ok {
		return fmt.Errorf("%w: remember", ErrWidgetMissing)
	}
	if v.rememberHint, ok = named["rememberHint"].(*widget.Label); !ok {
		return fmt.Errorf("%w: rememberHint", ErrWidgetMissing)
	}
	if v.okBtn, ok = named["ok"].(*widget.Button); !ok {
		return fmt.Errorf("%w: ok", ErrWidgetMissing)
	}
	if v.cancelBtn, ok = named["cancel"].(*widget.Button); !ok {
		return fmt.Errorf("%w: cancel", ErrWidgetMissing)
	}
	return nil
}

func (v *View) SetBlocked(target string, paths []string) {
	v.messageLabel.SetText(i18n.Tf("Dialog.SwitchChanges.Message", target))
	v.pathsLabel.SetText(ListPaths(paths))
}

func (v *View) Choice() Choice {
	mode := config.SwitchChangesStash
	switch {
	case v.modeMerge.IsSelected():
		mode = config.SwitchChangesMerge
	case v.modeOverwrite.IsSelected():
		mode = config.SwitchChangesOverwrite
	}
	return Choice{Mode: mode, Remember: v.rememberBox.IsChecked()}
}

func (v *View) confirm() {
	if v.OnChoose != nil {
		v.OnChoose(v.Choice())
	}
}

func (v *View) cancel() {
	if v.OnCancel != nil {
		v.OnCancel()
	}
}
