package toolbar

import (
	"errors"
	"fmt"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/dialogs"
	"github.com/oops1/gogit/internal/ui/style"
)

const dialogName = "toolbar_configure"

var ErrWidgetMissing = errors.New("toolbar: named widget missing")

var loadDialog = dialogs.Load

type Result struct {
	Items    []string
	Captions bool
}

type View struct {
	dlg            *widget.Dialog
	model          *Model
	availableLabel *widget.Label
	selectedLabel  *widget.Label
	available      *widget.ListView
	selected       *widget.ListView
	addBtn         *widget.Button
	removeBtn      *widget.Button
	upBtn          *widget.Button
	downBtn        *widget.Button
	resetBtn       *widget.Button
	captionsBox    *widget.CheckBox
	okBtn          *widget.Button
	cancelBtn      *widget.Button

	OnOK     func(Result)
	OnCancel func()
}

func NewView(model *Model) (*View, error) {
	dlg, named, err := loadDialog(dialogName, i18n.T("Dialog.Toolbar.Title"))
	if err != nil {
		return nil, err
	}
	v := &View{dlg: dlg, model: model}
	if err := v.bind(named); err != nil {
		return nil, err
	}
	v.captionsBox.SetChecked(model.Captions())
	v.available.OnSelect = func(int, string) { v.refresh() }
	v.selected.OnSelect = func(int, string) { v.refresh() }
	v.addBtn.OnClick = v.add
	v.removeBtn.OnClick = v.remove
	v.upBtn.OnClick = func() { v.move(-1) }
	v.downBtn.OnClick = func() { v.move(1) }
	v.resetBtn.OnClick = v.reset
	v.okBtn.OnClick = v.confirm
	v.cancelBtn.OnClick = v.cancel
	v.dlg.DefaultAction = v.confirm
	v.dlg.CancelAction = v.cancel
	v.render(0, 0)
	return v, nil
}

func (v *View) Dialog() *widget.Dialog { return v.dlg }

func (v *View) Model() *Model { return v.model }

func (v *View) Restyle(t *widget.Theme) {
	p := style.Of(t)
	p.Body(v.availableLabel, v.selectedLabel)
	p.Quiet(v.addBtn, v.removeBtn, v.upBtn, v.downBtn, v.resetBtn, v.cancelBtn)
	p.Primary(v.okBtn)
}

func (v *View) bind(named map[string]widget.Widget) error {
	var ok bool
	if v.availableLabel, ok = named["availableLabel"].(*widget.Label); !ok {
		return fmt.Errorf("%w: availableLabel", ErrWidgetMissing)
	}
	if v.selectedLabel, ok = named["selectedLabel"].(*widget.Label); !ok {
		return fmt.Errorf("%w: selectedLabel", ErrWidgetMissing)
	}
	if v.available, ok = named["available"].(*widget.ListView); !ok {
		return fmt.Errorf("%w: available", ErrWidgetMissing)
	}
	if v.selected, ok = named["selected"].(*widget.ListView); !ok {
		return fmt.Errorf("%w: selected", ErrWidgetMissing)
	}
	if v.captionsBox, ok = named["captions"].(*widget.CheckBox); !ok {
		return fmt.Errorf("%w: captions", ErrWidgetMissing)
	}
	return v.bindButtons(named)
}

func (v *View) bindButtons(named map[string]widget.Widget) error {
	targets := []struct {
		name string
		into **widget.Button
	}{
		{"add", &v.addBtn},
		{"remove", &v.removeBtn},
		{"moveUp", &v.upBtn},
		{"moveDown", &v.downBtn},
		{"reset", &v.resetBtn},
		{"ok", &v.okBtn},
		{"cancel", &v.cancelBtn},
	}
	for _, target := range targets {
		btn, ok := named[target.name].(*widget.Button)
		if !ok {
			return fmt.Errorf("%w: %s", ErrWidgetMissing, target.name)
		}
		*target.into = btn
	}
	return nil
}

func (v *View) render(available, selected int) {
	v.available.SetItems(labelsOf(v.model.Available()))
	v.selected.SetItems(labelsOf(v.model.Selected()))
	v.available.SetSelected(clampIndex(available, len(v.available.Items())))
	v.selected.SetSelected(clampIndex(selected, len(v.selected.Items())))
	v.refresh()
}

func labelsOf(entries []Entry) []string {
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		out = append(out, entry.Label)
	}
	return out
}

func clampIndex(at, count int) int {
	if count == 0 {
		return -1
	}
	return min(max(at, 0), count-1)
}

func (v *View) refresh() {
	selected := v.selected.Selected()
	count := len(v.model.Items())
	v.addBtn.SetEnabled(v.available.Selected() >= 0)
	v.removeBtn.SetEnabled(selected >= 0)
	v.upBtn.SetEnabled(selected > 0)
	v.downBtn.SetEnabled(selected >= 0 && selected < count-1)
	v.resetBtn.SetEnabled(!v.model.IsDefault())
	v.okBtn.SetEnabled(v.model.HasCommands())
}

func (v *View) add() {
	at := v.model.Add(v.available.Selected(), v.insertAt())
	if at < 0 {
		return
	}
	v.render(v.available.Selected(), at)
}

func (v *View) insertAt() int {
	if at := v.selected.Selected(); at >= 0 {
		return at + 1
	}
	return -1
}

func (v *View) remove() {
	at := v.selected.Selected()
	if !v.model.Remove(at) {
		return
	}
	v.render(v.available.Selected(), at)
}

func (v *View) move(by int) {
	at := v.selected.Selected()
	if !v.model.Move(at, at+by) {
		return
	}
	v.render(v.available.Selected(), at+by)
}

func (v *View) reset() {
	v.model.Reset()
	v.render(0, 0)
}

func (v *View) Result() Result {
	return Result{Items: v.model.Items(), Captions: v.captionsBox.IsChecked()}
}

func (v *View) confirm() {
	if !v.model.HasCommands() || v.OnOK == nil {
		return
	}
	v.OnOK(v.Result())
}

func (v *View) cancel() {
	if v.OnCancel != nil {
		v.OnCancel()
	}
}
