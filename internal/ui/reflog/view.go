package reflog

import (
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/oops1/headless-gui/v3/widget"
	"github.com/oops1/headless-gui/v3/widget/datagrid"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/dialogs"
	"github.com/oops1/gogit/internal/ui/style"
)

const dialogName = "reflog"

var ErrWidgetMissing = errors.New("reflog: named widget missing")

var loadDialog = dialogs.Load

const shortLength = 7

type Record struct {
	Selector string
	Commit   hash.ObjectID
	When     time.Time
	Who      string
	Message  string
}

type Row struct {
	Selector string
	Commit   string
	When     string
	Who      string
	Message  string
}

type View struct {
	dlg       *widget.Dialog
	refLabel  *widget.Label
	table     *widget.DataGridWidget
	hintLabel *widget.Label
	resetBtn  *widget.Button
	closeBtn  *widget.Button

	records  []Record
	selected int

	OnReset func(record Record)
	OnClose func()
}

func NewView() (*View, error) {
	dlg, named, err := loadDialog(dialogName, i18n.T("Dialog.Reflog.Title"))
	if err != nil {
		return nil, err
	}
	v := &View{dlg: dlg, selected: -1}
	if err := v.bind(named); err != nil {
		return nil, err
	}
	v.hintLabel.Muted = true
	v.buildColumns()
	v.wire()
	v.refresh()
	return v, nil
}

func (v *View) Dialog() *widget.Dialog { return v.dlg }

func (v *View) Restyle(t *widget.Theme) {
	p := style.Of(t)
	p.Quiet(v.closeBtn)
	p.Primary(v.resetBtn)
	p.Body(v.refLabel)
	p.Hints(v.hintLabel)
}

func (v *View) bind(named map[string]widget.Widget) error {
	var ok bool
	if v.refLabel, ok = named["refLabel"].(*widget.Label); !ok {
		return fmt.Errorf("%w: refLabel", ErrWidgetMissing)
	}
	if v.table, ok = named["records"].(*widget.DataGridWidget); !ok {
		return fmt.Errorf("%w: records", ErrWidgetMissing)
	}
	if v.hintLabel, ok = named["hint"].(*widget.Label); !ok {
		return fmt.Errorf("%w: hint", ErrWidgetMissing)
	}
	if v.resetBtn, ok = named["reset"].(*widget.Button); !ok {
		return fmt.Errorf("%w: reset", ErrWidgetMissing)
	}
	if v.closeBtn, ok = named["close"].(*widget.Button); !ok {
		return fmt.Errorf("%w: close", ErrWidgetMissing)
	}
	return nil
}

func (v *View) buildColumns() {
	selector := datagrid.NewTextColumn(i18n.T("Dialog.Reflog.Column.Selector"), "Selector")
	selector.SetWidth(datagrid.PixelWidth(110))
	commit := datagrid.NewTextColumn(i18n.T("Dialog.Reflog.Column.Commit"), "Commit")
	commit.SetWidth(datagrid.PixelWidth(90))
	when := datagrid.NewTextColumn(i18n.T("Dialog.Reflog.Column.When"), "When")
	when.SetWidth(datagrid.PixelWidth(140))
	who := datagrid.NewTextColumn(i18n.T("Dialog.Reflog.Column.Who"), "Who")
	who.SetWidth(datagrid.PixelWidth(120))
	message := datagrid.NewTextColumn(i18n.T("Dialog.Reflog.Column.Message"), "Message")
	message.SetWidth(datagrid.StarWidth(1))
	v.table.Grid.SetColumns([]datagrid.Column{selector, commit, when, who, message})
}

func (v *View) wire() {
	v.table.Grid.OnSelectionChanged = v.onSelected
	v.resetBtn.OnClick = v.resetHere
	v.closeBtn.OnClick = v.close
	v.dlg.CancelAction = v.close
}

func (v *View) SetRecords(ref string, records []Record) {
	v.refLabel.SetText(i18n.Tf("Dialog.Reflog.Ref", ref, len(records)))
	v.records = slices.Clone(records)
	v.selected = -1
	items := make([]any, 0, len(v.records))
	for _, record := range v.records {
		items = append(items, Row{
			Selector: record.Selector,
			Commit:   record.Commit.String()[:shortLength],
			When:     record.When.Local().Format("2006-01-02 15:04"),
			Who:      record.Who,
			Message:  record.Message,
		})
	}
	v.table.Grid.SetItemsSource(datagrid.NewObservableCollectionFrom(items))
	v.refresh()
}

func (v *View) Records() []Record { return slices.Clone(v.records) }

func (v *View) onSelected(ev datagrid.SelectionChangedEvent) {
	row, ok := ev.SelectedItem.(Row)
	if !ok {
		v.selected = -1
		v.refresh()
		return
	}
	v.selected = slices.IndexFunc(v.records, func(record Record) bool { return record.Selector == row.Selector })
	v.refresh()
}

func (v *View) refresh() {
	v.resetBtn.SetEnabled(v.selected >= 0)
	key, args := v.hint()
	v.hintLabel.SetText(i18n.Tf(key, args...))
}

func (v *View) hint() (string, []any) {
	switch {
	case len(v.records) == 0:
		return "Dialog.Reflog.Hint.Empty", nil
	case v.selected < 0:
		return "Dialog.Reflog.Hint.PickOne", nil
	}
	record := v.records[v.selected]
	return "Dialog.Reflog.Hint.Ready", []any{record.Selector, record.Commit.String()[:shortLength]}
}

func (v *View) resetHere() {
	if v.selected < 0 || v.OnReset == nil {
		return
	}
	v.OnReset(v.records[v.selected])
}

func (v *View) close() {
	if v.OnClose != nil {
		v.OnClose()
	}
}
