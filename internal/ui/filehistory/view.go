package filehistory

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

const dialogName = "file_history"

var ErrWidgetMissing = errors.New("filehistory: named widget missing")

var loadDialog = dialogs.Load

const shortLength = 7

type Entry struct {
	Commit  hash.ObjectID
	Author  string
	When    time.Time
	Subject string
	Path    string
	Old     string
}

func (e Entry) Renamed() bool { return e.Old != "" && e.Old != e.Path }

type Row struct {
	Commit  string
	Author  string
	When    string
	Subject string
}

type View struct {
	dlg       *widget.Dialog
	pathLabel *widget.Label
	table     *widget.DataGridWidget
	hintLabel *widget.Label
	blameBtn  *widget.Button
	closeBtn  *widget.Button

	entries  []Entry
	selected int

	OnBlame func(entry Entry)
	OnClose func()
}

func NewView() (*View, error) {
	dlg, named, err := loadDialog(dialogName, i18n.T("Dialog.FileHistory.Title"))
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
	p.Quiet(v.blameBtn, v.closeBtn)
	p.Body(v.pathLabel)
	p.Hints(v.hintLabel)
}

func (v *View) bind(named map[string]widget.Widget) error {
	var ok bool
	if v.pathLabel, ok = named["pathLabel"].(*widget.Label); !ok {
		return fmt.Errorf("%w: pathLabel", ErrWidgetMissing)
	}
	if v.table, ok = named["commits"].(*widget.DataGridWidget); !ok {
		return fmt.Errorf("%w: commits", ErrWidgetMissing)
	}
	if v.hintLabel, ok = named["hint"].(*widget.Label); !ok {
		return fmt.Errorf("%w: hint", ErrWidgetMissing)
	}
	if v.blameBtn, ok = named["blame"].(*widget.Button); !ok {
		return fmt.Errorf("%w: blame", ErrWidgetMissing)
	}
	if v.closeBtn, ok = named["close"].(*widget.Button); !ok {
		return fmt.Errorf("%w: close", ErrWidgetMissing)
	}
	return nil
}

func (v *View) buildColumns() {
	commit := datagrid.NewTextColumn(i18n.T("Dialog.FileHistory.Column.Commit"), "Commit")
	commit.SetWidth(datagrid.PixelWidth(90))
	when := datagrid.NewTextColumn(i18n.T("Dialog.FileHistory.Column.When"), "When")
	when.SetWidth(datagrid.PixelWidth(150))
	author := datagrid.NewTextColumn(i18n.T("Dialog.FileHistory.Column.Author"), "Author")
	author.SetWidth(datagrid.PixelWidth(150))
	subject := datagrid.NewTextColumn(i18n.T("Dialog.FileHistory.Column.Subject"), "Subject")
	subject.SetWidth(datagrid.StarWidth(1))
	v.table.Grid.SetColumns([]datagrid.Column{commit, when, author, subject})
}

func (v *View) wire() {
	v.table.Grid.OnSelectionChanged = v.onSelected
	v.blameBtn.OnClick = v.blameSelected
	v.closeBtn.OnClick = v.close
	v.dlg.CancelAction = v.close
}

func (v *View) SetEntries(path string, entries []Entry) {
	v.pathLabel.SetText(i18n.Tf("Dialog.FileHistory.Path", path, len(entries)))
	v.entries = slices.Clone(entries)
	v.selected = -1
	items := make([]any, 0, len(v.entries))
	for _, entry := range v.entries {
		items = append(items, Row{
			Commit:  entry.Commit.String()[:shortLength],
			Author:  entry.Author,
			When:    entry.When.Local().Format("2006-01-02 15:04"),
			Subject: entry.Subject,
		})
	}
	v.table.Grid.SetItemsSource(datagrid.NewObservableCollectionFrom(items))
	v.refresh()
}

func (v *View) Entries() []Entry { return slices.Clone(v.entries) }

func (v *View) onSelected(ev datagrid.SelectionChangedEvent) {
	row, ok := ev.SelectedItem.(Row)
	if !ok {
		v.selected = -1
		v.refresh()
		return
	}
	v.selected = slices.IndexFunc(v.entries, func(entry Entry) bool {
		return entry.Commit.String()[:shortLength] == row.Commit
	})
	v.refresh()
}

func (v *View) refresh() {
	v.blameBtn.SetEnabled(v.selected >= 0)
	key, args := v.hint()
	v.hintLabel.SetText(i18n.Tf(key, args...))
}

func (v *View) hint() (string, []any) {
	switch {
	case len(v.entries) == 0:
		return "Dialog.FileHistory.Hint.Empty", nil
	case v.selected < 0:
		return "Dialog.FileHistory.Hint.PickOne", nil
	case v.entries[v.selected].Renamed():
		return "Dialog.FileHistory.Hint.Renamed", []any{v.entries[v.selected].Old}
	}
	return "Dialog.FileHistory.Hint.Ready", nil
}

func (v *View) blameSelected() {
	if v.selected < 0 || v.OnBlame == nil {
		return
	}
	v.OnBlame(v.entries[v.selected])
}

func (v *View) close() {
	if v.OnClose != nil {
		v.OnClose()
	}
}
