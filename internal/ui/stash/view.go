package stash

import (
	"errors"
	"fmt"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/dialogs"
	"github.com/oops1/gogit/internal/ui/style"
)

const dialogName = "stash"

var ErrWidgetMissing = errors.New("stash: named widget missing")

var loadDialog = dialogs.Load

type Mode int

const (
	ModeApply Mode = iota
	ModeDrop
)

var modeKeys = map[Mode][2]string{
	ModeApply: {"Dialog.ApplyStash.Title", "Dialog.ApplyStash.OK"},
	ModeDrop:  {"Dialog.DropStash.Title", "Dialog.DropStash.OK"},
}

type Request struct {
	Index int
	Drop  bool
}

type View struct {
	dlg        *widget.Dialog
	mode       Mode
	count      int
	entryLabel *widget.Label
	entries    *widget.Dropdown
	dropBox    *widget.CheckBox
	okBtn      *widget.Button
	cancelBtn  *widget.Button

	OnOK     func(Request)
	OnCancel func()
}

func NewView(mode Mode) (*View, error) {
	keys := modeKeys[mode]
	dlg, named, err := loadDialog(dialogName, i18n.T(keys[0]))
	if err != nil {
		return nil, err
	}
	v := &View{dlg: dlg, mode: mode}
	if err := v.bind(named); err != nil {
		return nil, err
	}
	v.okBtn.SetText(i18n.T(keys[1]))
	v.dropBox.SetVisible(mode == ModeApply)
	v.okBtn.OnClick = v.confirm
	v.cancelBtn.OnClick = v.cancel
	v.dlg.DefaultAction = v.confirm
	v.dlg.CancelAction = v.cancel
	v.refresh()
	return v, nil
}

func (v *View) Dialog() *widget.Dialog { return v.dlg }

func (v *View) Restyle(t *widget.Theme) {
	p := style.Of(t)
	p.Lists(v.entries)
	p.Quiet(v.cancelBtn)
	p.Primary(v.okBtn)
	p.Body(v.entryLabel)
}

func (v *View) bind(named map[string]widget.Widget) error {
	var ok bool
	if v.entryLabel, ok = named["entryLabel"].(*widget.Label); !ok {
		return fmt.Errorf("%w: entryLabel", ErrWidgetMissing)
	}
	if v.entries, ok = named["entries"].(*widget.Dropdown); !ok {
		return fmt.Errorf("%w: entries", ErrWidgetMissing)
	}
	if v.dropBox, ok = named["dropAfterApply"].(*widget.CheckBox); !ok {
		return fmt.Errorf("%w: dropAfterApply", ErrWidgetMissing)
	}
	if v.okBtn, ok = named["ok"].(*widget.Button); !ok {
		return fmt.Errorf("%w: ok", ErrWidgetMissing)
	}
	if v.cancelBtn, ok = named["cancel"].(*widget.Button); !ok {
		return fmt.Errorf("%w: cancel", ErrWidgetMissing)
	}
	return nil
}

func (v *View) SetEntries(labels []string, selected int) {
	v.count = len(labels)
	v.entries.SetItems(labels)
	v.entries.SetSelected(min(max(selected, 0), max(v.count-1, 0)))
	v.refresh()
}

func (v *View) Request() Request {
	return Request{Index: v.entries.Selected(), Drop: v.mode == ModeDrop || v.dropBox.IsChecked()}
}

func (v *View) refresh() {
	v.okBtn.SetEnabled(v.count > 0)
}

func (v *View) confirm() {
	if v.count == 0 {
		return
	}
	if v.OnOK != nil {
		v.OnOK(v.Request())
	}
}

func (v *View) cancel() {
	if v.OnCancel != nil {
		v.OnCancel()
	}
}
