package rebase

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/dialogs"
	"github.com/oops1/gogit/internal/ui/style"
)

const dialogName = "rebase"

var ErrWidgetMissing = errors.New("rebase: named widget missing")

var loadDialog = dialogs.Load

const (
	hintOntoRequired  = "Dialog.Rebase.Hint.OntoRequired"
	hintOntoIsCurrent = "Dialog.Rebase.Hint.OntoIsCurrent"
	hintWillReplay    = "Dialog.Rebase.Hint.WillReplay"
)

type Known struct {
	Current    string
	Candidates []string
}

type Hint struct {
	Key  string
	Args []any
	OK   bool
}

func Validate(onto string, known Known) Hint {
	switch onto = strings.TrimSpace(onto); onto {
	case "":
		return Hint{Key: hintOntoRequired}
	case known.Current:
		return Hint{Key: hintOntoIsCurrent, Args: []any{onto}}
	}
	return Hint{Key: hintWillReplay, Args: []any{known.Current, onto}, OK: true}
}

type View struct {
	dlg       *widget.Dialog
	ontoLabel *widget.Label
	ontoList  *widget.Dropdown
	hintLabel *widget.Label
	okBtn     *widget.Button
	cancelBtn *widget.Button

	known   Known
	current Hint

	OnOK     func(onto string)
	OnCancel func()
}

func NewView() (*View, error) {
	dlg, named, err := loadDialog(dialogName, i18n.T("Dialog.Rebase.Title"))
	if err != nil {
		return nil, err
	}
	v := &View{dlg: dlg}
	if err := v.bind(named); err != nil {
		return nil, err
	}
	v.hintLabel.Muted = true
	v.wire()
	v.refresh()
	return v, nil
}

func (v *View) Dialog() *widget.Dialog { return v.dlg }

func (v *View) Restyle(t *widget.Theme) {
	p := style.Of(t)
	p.Lists(v.ontoList)
	p.Quiet(v.cancelBtn)
	p.Primary(v.okBtn)
	p.Body(v.ontoLabel)
	p.Hints(v.hintLabel)
}

func (v *View) bind(named map[string]widget.Widget) error {
	var ok bool
	if v.ontoLabel, ok = named["ontoLabel"].(*widget.Label); !ok {
		return fmt.Errorf("%w: ontoLabel", ErrWidgetMissing)
	}
	if v.ontoList, ok = named["onto"].(*widget.Dropdown); !ok {
		return fmt.Errorf("%w: onto", ErrWidgetMissing)
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
	v.ontoList.OnChange = func(int, string) { v.refresh() }
	v.okBtn.OnClick = v.confirm
	v.cancelBtn.OnClick = v.cancel
	v.dlg.DefaultAction = v.confirm
	v.dlg.CancelAction = v.cancel
}

func (v *View) SetKnown(known Known, selected string) {
	v.known = known
	v.ontoLabel.SetText(i18n.Tf("Dialog.Rebase.Onto", known.Current))
	v.ontoList.SetItems(known.Candidates)
	v.ontoList.SetSelected(max(slices.Index(known.Candidates, selected), 0))
	v.refresh()
}

func (v *View) Onto() string { return v.ontoList.SelectedText() }

func (v *View) refresh() {
	v.current = Validate(v.Onto(), v.known)
	v.hintLabel.SetText(i18n.Tf(v.current.Key, v.current.Args...))
	v.okBtn.SetEnabled(v.current.OK)
}

func (v *View) confirm() {
	if !v.current.OK {
		return
	}
	if v.OnOK != nil {
		v.OnOK(v.Onto())
	}
}

func (v *View) cancel() {
	if v.OnCancel != nil {
		v.OnCancel()
	}
}
