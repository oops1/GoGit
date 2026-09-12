package reset

import (
	"errors"
	"fmt"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/dialogs"
	"github.com/oops1/gogit/internal/ui/style"
)

const dialogName = "reset"

var ErrWidgetMissing = errors.New("reset: named widget missing")

var loadDialog = dialogs.Load

type Mode int

const (
	ModeSoft Mode = iota
	ModeMixed
	ModeHard
)

type modeText struct {
	name string
	hint string
}

var modes = []modeText{
	{"Dialog.Reset.Mode.Soft", "Dialog.Reset.Hint.Soft"},
	{"Dialog.Reset.Mode.Mixed", "Dialog.Reset.Hint.Mixed"},
	{"Dialog.Reset.Mode.Hard", "Dialog.Reset.Hint.Hard"},
}

type Known struct {
	Branch string
	Commit string
}

type Hint struct {
	Key  string
	Args []any
}

func Validate(mode Mode, known Known) Hint {
	if mode < ModeSoft || int(mode) >= len(modes) {
		mode = ModeMixed
	}
	return Hint{Key: modes[mode].hint, Args: []any{known.Branch, known.Commit}}
}

func ModeNames() []string {
	names := make([]string, 0, len(modes))
	for _, m := range modes {
		names = append(names, i18n.T(m.name))
	}
	return names
}

type View struct {
	dlg       *widget.Dialog
	modeLabel *widget.Label
	modeList  *widget.Dropdown
	hintLabel *widget.Label
	okBtn     *widget.Button
	cancelBtn *widget.Button

	known Known

	OnOK     func(mode Mode)
	OnCancel func()
}

func NewView() (*View, error) {
	dlg, named, err := loadDialog(dialogName, i18n.T("Dialog.Reset.Title"))
	if err != nil {
		return nil, err
	}
	v := &View{dlg: dlg}
	if err := v.bind(named); err != nil {
		return nil, err
	}
	v.hintLabel.Muted = true
	v.modeList.SetItems(ModeNames())
	v.modeList.SetSelected(int(ModeMixed))
	v.wire()
	v.refresh()
	return v, nil
}

func (v *View) Dialog() *widget.Dialog { return v.dlg }

func (v *View) Restyle(t *widget.Theme) {
	p := style.Of(t)
	p.Lists(v.modeList)
	p.Quiet(v.cancelBtn)
	p.Primary(v.okBtn)
	p.Body(v.modeLabel)
	p.Hints(v.hintLabel)
}

func (v *View) bind(named map[string]widget.Widget) error {
	var ok bool
	if v.modeLabel, ok = named["modeLabel"].(*widget.Label); !ok {
		return fmt.Errorf("%w: modeLabel", ErrWidgetMissing)
	}
	if v.modeList, ok = named["mode"].(*widget.Dropdown); !ok {
		return fmt.Errorf("%w: mode", ErrWidgetMissing)
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

func (v *View) wire() {
	v.modeList.OnChange = func(int, string) { v.refresh() }
	v.okBtn.OnClick = v.confirm
	v.cancelBtn.OnClick = v.cancel
	v.dlg.DefaultAction = v.confirm
	v.dlg.CancelAction = v.cancel
}

func (v *View) SetKnown(known Known, mode Mode) {
	v.known = known
	v.modeLabel.SetText(i18n.Tf("Dialog.Reset.Target", known.Branch, known.Commit))
	v.modeList.SetSelected(int(mode))
	v.refresh()
}

func (v *View) Mode() Mode { return Mode(v.modeList.Selected()) }

func (v *View) refresh() {
	hint := Validate(v.Mode(), v.known)
	v.hintLabel.SetText(i18n.Tf(hint.Key, hint.Args...))
}

func (v *View) confirm() {
	if v.OnOK != nil {
		v.OnOK(v.Mode())
	}
}

func (v *View) cancel() {
	if v.OnCancel != nil {
		v.OnCancel()
	}
}
