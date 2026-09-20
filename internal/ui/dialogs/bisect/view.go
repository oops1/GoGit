package bisect

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

const dialogName = "bisect_start"

var ErrWidgetMissing = errors.New("bisect: named widget missing")

var loadDialog = dialogs.Load

const (
	hintBadRequired  = "Dialog.Bisect.Hint.BadRequired"
	hintGoodRequired = "Dialog.Bisect.Hint.GoodRequired"
	hintSameRevision = "Dialog.Bisect.Hint.SameRevision"
	hintWillSearch   = "Dialog.Bisect.Hint.WillSearch"
)

type Known struct {
	Bad  string
	Good string
	Revs []string
}

type Choice struct {
	Bad  string
	Good string
}

type Hint struct {
	Key  string
	Args []any
	OK   bool
}

func Validate(choice Choice) Hint {
	bad, good := strings.TrimSpace(choice.Bad), strings.TrimSpace(choice.Good)
	switch {
	case bad == "":
		return Hint{Key: hintBadRequired}
	case good == "":
		return Hint{Key: hintGoodRequired}
	case bad == good:
		return Hint{Key: hintSameRevision, Args: []any{bad}}
	}
	return Hint{Key: hintWillSearch, Args: []any{good, bad}, OK: true}
}

type View struct {
	dlg       *widget.Dialog
	header    *widget.Label
	badLabel  *widget.Label
	goodLabel *widget.Label
	badList   *widget.Dropdown
	goodList  *widget.Dropdown
	hintLabel *widget.Label
	okBtn     *widget.Button
	cancelBtn *widget.Button

	current Hint

	OnOK     func(Choice)
	OnCancel func()
}

func NewView() (*View, error) {
	dlg, named, err := loadDialog(dialogName, i18n.T("Dialog.Bisect.Title"))
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
	p.Lists(v.badList, v.goodList)
	p.Quiet(v.cancelBtn)
	p.Primary(v.okBtn)
	p.Body(v.header, v.badLabel, v.goodLabel)
	p.Hints(v.hintLabel)
}

func (v *View) bind(named map[string]widget.Widget) error {
	labels := map[string]**widget.Label{"header": &v.header, "badLabel": &v.badLabel, "goodLabel": &v.goodLabel, "hint": &v.hintLabel}
	for name, target := range labels {
		label, ok := named[name].(*widget.Label)
		if !ok {
			return fmt.Errorf("%w: %s", ErrWidgetMissing, name)
		}
		*target = label
	}
	lists := map[string]**widget.Dropdown{"bad": &v.badList, "good": &v.goodList}
	for name, target := range lists {
		list, ok := named[name].(*widget.Dropdown)
		if !ok {
			return fmt.Errorf("%w: %s", ErrWidgetMissing, name)
		}
		*target = list
	}
	buttons := map[string]**widget.Button{"ok": &v.okBtn, "cancel": &v.cancelBtn}
	for name, target := range buttons {
		button, ok := named[name].(*widget.Button)
		if !ok {
			return fmt.Errorf("%w: %s", ErrWidgetMissing, name)
		}
		*target = button
	}
	return nil
}

func (v *View) wire() {
	v.badList.OnChange = func(int, string) { v.refresh() }
	v.goodList.OnChange = func(int, string) { v.refresh() }
	v.okBtn.OnClick = v.confirm
	v.cancelBtn.OnClick = v.cancel
	v.dlg.DefaultAction = v.confirm
	v.dlg.CancelAction = v.cancel
}

func (v *View) SetKnown(known Known) {
	v.badList.SetItems(known.Revs)
	v.goodList.SetItems(known.Revs)
	v.badList.SetSelected(max(slices.Index(known.Revs, known.Bad), 0))
	v.goodList.SetSelected(max(slices.Index(known.Revs, known.Good), 0))
	v.refresh()
}

func (v *View) Choice() Choice {
	return Choice{Bad: v.badList.SelectedText(), Good: v.goodList.SelectedText()}
}

func (v *View) refresh() {
	v.current = Validate(v.Choice())
	v.hintLabel.SetText(i18n.Tf(v.current.Key, v.current.Args...))
	v.okBtn.SetEnabled(v.current.OK)
}

func (v *View) confirm() {
	if !v.current.OK {
		return
	}
	if v.OnOK != nil {
		v.OnOK(v.Choice())
	}
}

func (v *View) cancel() {
	if v.OnCancel != nil {
		v.OnCancel()
	}
}
