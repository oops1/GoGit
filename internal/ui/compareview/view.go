package compareview

import (
	"errors"
	"fmt"
	"slices"

	"github.com/oops1/headless-gui/v3/widget"
	"github.com/oops1/headless-gui/v3/widget/datagrid"

	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/dialogs"
	"github.com/oops1/gogit/internal/ui/style"
)

const dialogName = "compare_refs"

var ErrWidgetMissing = errors.New("compareview: named widget missing")

var loadDialog = dialogs.Load

type Change struct {
	Status  string
	Path    string
	Old     string
	Added   int
	Deleted int
}

type Row struct {
	Status  string
	Path    string
	Changes string
}

type Summary struct {
	Left    string
	Right   string
	Ahead   int
	Behind  int
	Same    bool
	Changes []Change
}

type View struct {
	dlg         *widget.Dialog
	sidesLabel  *widget.Label
	leftList    *widget.Dropdown
	rightList   *widget.Dropdown
	countsLabel *widget.Label
	table       *widget.DataGridWidget
	hintLabel   *widget.Label
	closeBtn    *widget.Button

	summary Summary

	OnCompare func(left, right string)
	OnClose   func()
}

func NewView() (*View, error) {
	dlg, named, err := loadDialog(dialogName, i18n.T("Dialog.CompareRefs.Title"))
	if err != nil {
		return nil, err
	}
	v := &View{dlg: dlg}
	if err := v.bind(named); err != nil {
		return nil, err
	}
	v.hintLabel.Muted = true
	v.buildColumns()
	v.wire()
	return v, nil
}

func (v *View) Dialog() *widget.Dialog { return v.dlg }

func (v *View) Restyle(t *widget.Theme) {
	p := style.Of(t)
	p.Lists(v.leftList, v.rightList)
	p.Quiet(v.closeBtn)
	p.Body(v.sidesLabel, v.countsLabel)
	p.Hints(v.hintLabel)
}

func (v *View) bind(named map[string]widget.Widget) error {
	var ok bool
	if v.sidesLabel, ok = named["sidesLabel"].(*widget.Label); !ok {
		return fmt.Errorf("%w: sidesLabel", ErrWidgetMissing)
	}
	if v.leftList, ok = named["left"].(*widget.Dropdown); !ok {
		return fmt.Errorf("%w: left", ErrWidgetMissing)
	}
	if v.rightList, ok = named["right"].(*widget.Dropdown); !ok {
		return fmt.Errorf("%w: right", ErrWidgetMissing)
	}
	if v.countsLabel, ok = named["countsLabel"].(*widget.Label); !ok {
		return fmt.Errorf("%w: countsLabel", ErrWidgetMissing)
	}
	if v.table, ok = named["changes"].(*widget.DataGridWidget); !ok {
		return fmt.Errorf("%w: changes", ErrWidgetMissing)
	}
	if v.hintLabel, ok = named["hint"].(*widget.Label); !ok {
		return fmt.Errorf("%w: hint", ErrWidgetMissing)
	}
	if v.closeBtn, ok = named["close"].(*widget.Button); !ok {
		return fmt.Errorf("%w: close", ErrWidgetMissing)
	}
	return nil
}

func (v *View) buildColumns() {
	status := datagrid.NewTextColumn(i18n.T("Dialog.CompareRefs.Column.Status"), "Status")
	status.SetWidth(datagrid.PixelWidth(110))
	path := datagrid.NewTextColumn(i18n.T("Dialog.CompareRefs.Column.Path"), "Path")
	path.SetWidth(datagrid.StarWidth(1))
	changes := datagrid.NewTextColumn(i18n.T("Dialog.CompareRefs.Column.Lines"), "Changes")
	changes.SetWidth(datagrid.PixelWidth(120))
	v.table.Grid.SetColumns([]datagrid.Column{status, path, changes})
}

func (v *View) wire() {
	v.leftList.OnChange = func(int, string) { v.askForCompare() }
	v.rightList.OnChange = func(int, string) { v.askForCompare() }
	v.closeBtn.OnClick = v.close
	v.dlg.CancelAction = v.close
}

func (v *View) SetSides(sides []string, left, right string) {
	v.leftList.SetItems(sides)
	v.rightList.SetItems(sides)
	v.leftList.SetSelected(max(slices.Index(sides, left), 0))
	v.rightList.SetSelected(max(slices.Index(sides, right), 0))
}

func (v *View) Sides() (string, string) {
	return v.leftList.SelectedText(), v.rightList.SelectedText()
}

func (v *View) SetSummary(summary Summary) {
	v.summary = summary
	v.sidesLabel.SetText(i18n.Tf("Dialog.CompareRefs.Sides", summary.Left, summary.Right))
	v.countsLabel.SetText(i18n.Tf("Dialog.CompareRefs.Counts", summary.Ahead, summary.Behind))
	items := make([]any, 0, len(summary.Changes))
	for _, change := range summary.Changes {
		items = append(items, Row{
			Status:  change.Status,
			Path:    pathOf(change),
			Changes: i18n.Tf("Dialog.CompareRefs.Lines", change.Added, change.Deleted),
		})
	}
	v.table.Grid.SetItemsSource(datagrid.NewObservableCollectionFrom(items))
	key, args := v.hint()
	v.hintLabel.SetText(i18n.Tf(key, args...))
}

func pathOf(change Change) string {
	if change.Old != "" && change.Old != change.Path {
		return change.Old + " → " + change.Path
	}
	return change.Path
}

func (v *View) Summary() Summary { return v.summary }

func (v *View) hint() (string, []any) {
	switch {
	case v.summary.Same:
		return "Dialog.CompareRefs.Hint.Same", nil
	case len(v.summary.Changes) == 0:
		return "Dialog.CompareRefs.Hint.NoChanges", nil
	}
	return "Dialog.CompareRefs.Hint.Changes", []any{len(v.summary.Changes)}
}

func (v *View) askForCompare() {
	left, right := v.Sides()
	if left == "" || right == "" || v.OnCompare == nil {
		return
	}
	v.OnCompare(left, right)
}

func (v *View) close() {
	if v.OnClose != nil {
		v.OnClose()
	}
}
