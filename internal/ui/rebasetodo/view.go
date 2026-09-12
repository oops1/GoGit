package rebasetodo

import (
	"errors"
	"fmt"
	"slices"

	"github.com/oops1/headless-gui/v3/widget"
	"github.com/oops1/headless-gui/v3/widget/datagrid"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/dialogs"
	"github.com/oops1/gogit/internal/ui/style"
)

const dialogName = "rebase_todo"

var ErrWidgetMissing = errors.New("rebasetodo: named widget missing")

var loadDialog = dialogs.Load

const (
	ActionPick   = "pick"
	ActionReword = "reword"
	ActionEdit   = "edit"
	ActionSquash = "squash"
	ActionFixup  = "fixup"
	ActionDrop   = "drop"
)

var Actions = []string{ActionPick, ActionReword, ActionEdit, ActionSquash, ActionFixup, ActionDrop}

var actionKeys = map[string]string{
	ActionPick:   "Dialog.RebaseTodo.Action.Pick",
	ActionReword: "Dialog.RebaseTodo.Action.Reword",
	ActionEdit:   "Dialog.RebaseTodo.Action.Edit",
	ActionSquash: "Dialog.RebaseTodo.Action.Squash",
	ActionFixup:  "Dialog.RebaseTodo.Action.Fixup",
	ActionDrop:   "Dialog.RebaseTodo.Action.Drop",
}

const (
	hintNothingToDo = "Dialog.RebaseTodo.Hint.NothingToDo"
	hintFoldsFirst  = "Dialog.RebaseTodo.Hint.FoldsFirst"
	hintReady       = "Dialog.RebaseTodo.Hint.Ready"
)

type Step struct {
	Action  string
	Commit  hash.ObjectID
	Subject string
}

type Row struct {
	Action  string
	Commit  string
	Subject string
}

type Hint struct {
	Key  string
	Args []any
	OK   bool
}

func Validate(steps []Step) Hint {
	kept := 0
	for _, step := range steps {
		switch {
		case step.Action == ActionDrop:
			continue
		case folds(step.Action) && kept == 0:
			return Hint{Key: hintFoldsFirst, Args: []any{i18n.T(actionKeys[step.Action])}}
		}
		kept++
	}
	if kept == 0 {
		return Hint{Key: hintNothingToDo}
	}
	return Hint{Key: hintReady, Args: []any{kept}, OK: true}
}

func folds(action string) bool { return action == ActionSquash || action == ActionFixup }

func ActionName(action string) string {
	key, known := actionKeys[action]
	if !known {
		return action
	}
	return i18n.T(key)
}

type View struct {
	dlg         *widget.Dialog
	ontoLabel   *widget.Label
	table       *widget.DataGridWidget
	actionLabel *widget.Label
	actionList  *widget.Dropdown
	upBtn       *widget.Button
	downBtn     *widget.Button
	hintLabel   *widget.Label
	okBtn       *widget.Button
	cancelBtn   *widget.Button

	steps    []Step
	selected int
	current  Hint

	OnOK     func(steps []Step)
	OnCancel func()
}

func NewView() (*View, error) {
	dlg, named, err := loadDialog(dialogName, i18n.T("Dialog.RebaseTodo.Title"))
	if err != nil {
		return nil, err
	}
	v := &View{dlg: dlg, selected: -1}
	if err := v.bind(named); err != nil {
		return nil, err
	}
	v.hintLabel.Muted = true
	v.buildColumns()
	v.actionList.SetItems(actionNames())
	v.wire()
	v.refresh()
	return v, nil
}

func actionNames() []string {
	names := make([]string, 0, len(Actions))
	for _, action := range Actions {
		names = append(names, ActionName(action))
	}
	return names
}

func (v *View) Dialog() *widget.Dialog { return v.dlg }

func (v *View) Restyle(t *widget.Theme) {
	p := style.Of(t)
	p.Lists(v.actionList)
	p.Quiet(v.cancelBtn, v.upBtn, v.downBtn)
	p.Primary(v.okBtn)
	p.Body(v.ontoLabel, v.actionLabel)
	p.Hints(v.hintLabel)
}

func (v *View) bind(named map[string]widget.Widget) error {
	var ok bool
	if v.ontoLabel, ok = named["ontoLabel"].(*widget.Label); !ok {
		return fmt.Errorf("%w: ontoLabel", ErrWidgetMissing)
	}
	if v.table, ok = named["steps"].(*widget.DataGridWidget); !ok {
		return fmt.Errorf("%w: steps", ErrWidgetMissing)
	}
	if v.actionLabel, ok = named["actionLabel"].(*widget.Label); !ok {
		return fmt.Errorf("%w: actionLabel", ErrWidgetMissing)
	}
	if v.actionList, ok = named["action"].(*widget.Dropdown); !ok {
		return fmt.Errorf("%w: action", ErrWidgetMissing)
	}
	if v.upBtn, ok = named["up"].(*widget.Button); !ok {
		return fmt.Errorf("%w: up", ErrWidgetMissing)
	}
	if v.downBtn, ok = named["down"].(*widget.Button); !ok {
		return fmt.Errorf("%w: down", ErrWidgetMissing)
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

func (v *View) buildColumns() {
	action := datagrid.NewTextColumn(i18n.T("Dialog.RebaseTodo.Column.Action"), "Action")
	action.SetWidth(datagrid.PixelWidth(150))
	commit := datagrid.NewTextColumn(i18n.T("Dialog.RebaseTodo.Column.Commit"), "Commit")
	commit.SetWidth(datagrid.PixelWidth(90))
	subject := datagrid.NewTextColumn(i18n.T("Dialog.RebaseTodo.Column.Subject"), "Subject")
	subject.SetWidth(datagrid.StarWidth(1))
	v.table.Grid.SetColumns([]datagrid.Column{action, commit, subject})
}

func (v *View) wire() {
	v.table.Grid.OnSelectionChanged = v.onSelected
	v.actionList.OnChange = func(int, string) { v.applyAction() }
	v.upBtn.OnClick = func() { v.move(-1) }
	v.downBtn.OnClick = func() { v.move(1) }
	v.okBtn.OnClick = v.confirm
	v.cancelBtn.OnClick = v.cancel
	v.dlg.DefaultAction = v.confirm
	v.dlg.CancelAction = v.cancel
}

func (v *View) SetSteps(onto string, steps []Step) {
	v.ontoLabel.SetText(i18n.Tf("Dialog.RebaseTodo.Onto", onto, len(steps)))
	v.steps = slices.Clone(steps)
	v.selected = -1
	v.showSteps()
	v.refresh()
}

func (v *View) Steps() []Step { return slices.Clone(v.steps) }

func (v *View) showSteps() {
	items := make([]any, 0, len(v.steps))
	for _, step := range v.steps {
		items = append(items, Row{Action: ActionName(step.Action), Commit: short(step.Commit), Subject: step.Subject})
	}
	v.table.Grid.SetItemsSource(datagrid.NewObservableCollectionFrom(items))
	if v.selected >= 0 && v.selected < len(v.steps) {
		v.table.Grid.SetSelectedIndex(v.selected)
	}
}

const shortLength = 7

func short(id hash.ObjectID) string { return id.String()[:shortLength] }

func (v *View) onSelected(ev datagrid.SelectionChangedEvent) {
	row, ok := ev.SelectedItem.(Row)
	if !ok {
		v.selected = -1
		v.refresh()
		return
	}
	v.selected = slices.IndexFunc(v.steps, func(step Step) bool { return short(step.Commit) == row.Commit })
	if v.selected >= 0 {
		v.actionList.SetSelected(slices.Index(Actions, v.steps[v.selected].Action))
	}
	v.refresh()
}

func (v *View) applyAction() {
	chosen := v.actionList.Selected()
	if v.selected < 0 || v.selected >= len(v.steps) || chosen < 0 || chosen >= len(Actions) || v.steps[v.selected].Action == Actions[chosen] {
		return
	}
	v.steps[v.selected].Action = Actions[chosen]
	v.showSteps()
	v.refresh()
}

func (v *View) move(by int) {
	to := v.selected + by
	if v.selected < 0 || to < 0 || to >= len(v.steps) {
		return
	}
	v.steps[v.selected], v.steps[to] = v.steps[to], v.steps[v.selected]
	v.selected = to
	v.showSteps()
	v.refresh()
}

func (v *View) refresh() {
	v.current = Validate(v.steps)
	v.hintLabel.SetText(i18n.Tf(v.current.Key, v.current.Args...))
	v.okBtn.SetEnabled(v.current.OK)
	v.upBtn.SetEnabled(v.selected > 0)
	v.downBtn.SetEnabled(v.selected >= 0 && v.selected < len(v.steps)-1)
}

func (v *View) confirm() {
	if !v.current.OK {
		return
	}
	if v.OnOK != nil {
		v.OnOK(v.Steps())
	}
}

func (v *View) cancel() {
	if v.OnCancel != nil {
		v.OnCancel()
	}
}
