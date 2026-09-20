package flow

import (
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/oops1/headless-gui/v3/widget"
	"github.com/oops1/headless-gui/v3/widget/datagrid"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/style"
)

const (
	logDialogName  = "flow_select_log"
	logShortLength = 7
	logDateLayout  = "2006-01-02 15:04"
)

type LogCommit struct {
	Commit  hash.ObjectID
	When    time.Time
	Author  string
	Message string
}

type LogRow struct {
	Commit  string
	When    string
	Author  string
	Message string
}

type LogView struct {
	dlg         *widget.Dialog
	headerLabel *widget.Label
	table       *widget.DataGridWidget
	hintLabel   *widget.Label
	okBtn       *widget.Button
	cancelBtn   *widget.Button

	commits  []LogCommit
	selected int

	OnOK     func(LogCommit)
	OnCancel func()
}

func NewLogView() (*LogView, error) {
	dlg, named, err := loadDialog(logDialogName, i18n.T("Dialog.FlowLog.Title"))
	if err != nil {
		return nil, err
	}
	v := &LogView{dlg: dlg, selected: -1}
	if err := errors.Join(
		bindWidget(named, "header", &v.headerLabel),
		bindWidget(named, "commits", &v.table),
		bindWidget(named, "hint", &v.hintLabel),
		bindWidget(named, "ok", &v.okBtn),
		bindWidget(named, "cancel", &v.cancelBtn),
	); err != nil {
		return nil, err
	}
	v.hintLabel.Muted = true
	v.buildColumns()
	v.table.Grid.OnSelectionChanged = v.pick
	v.okBtn.OnClick = v.confirm
	v.cancelBtn.OnClick = v.cancel
	v.dlg.CancelAction = v.cancel
	v.refresh()
	return v, nil
}

func (v *LogView) Dialog() *widget.Dialog { return v.dlg }

func (v *LogView) Restyle(t *widget.Theme) {
	p := style.Of(t)
	p.Quiet(v.cancelBtn)
	p.Primary(v.okBtn)
	p.Body(v.headerLabel)
	p.Hints(v.hintLabel)
}

func (v *LogView) buildColumns() {
	commit := datagrid.NewTextColumn(i18n.T("Dialog.FlowLog.Column.Commit"), "Commit")
	commit.SetWidth(datagrid.PixelWidth(90))
	when := datagrid.NewTextColumn(i18n.T("Dialog.FlowLog.Column.When"), "When")
	when.SetWidth(datagrid.PixelWidth(140))
	author := datagrid.NewTextColumn(i18n.T("Dialog.FlowLog.Column.Author"), "Author")
	author.SetWidth(datagrid.PixelWidth(140))
	message := datagrid.NewTextColumn(i18n.T("Dialog.FlowLog.Column.Message"), "Message")
	message.SetWidth(datagrid.StarWidth(1))
	v.table.Grid.SetColumns([]datagrid.Column{commit, when, author, message})
}

func (v *LogView) SetCommits(commits []LogCommit) {
	v.commits = slices.Clone(commits)
	v.selected = -1
	items := make([]any, 0, len(v.commits))
	for _, commit := range v.commits {
		items = append(items, LogRow{
			Commit:  shortCommit(commit.Commit),
			When:    commit.When.Local().Format(logDateLayout),
			Author:  commit.Author,
			Message: logSubject(commit.Message),
		})
	}
	v.table.Grid.SetItemsSource(datagrid.NewObservableCollectionFrom(items))
	v.refresh()
}

func (v *LogView) Commits() []LogCommit { return slices.Clone(v.commits) }

func (v *LogView) pick(ev datagrid.SelectionChangedEvent) {
	row, ok := ev.SelectedItem.(LogRow)
	if !ok {
		v.selected = -1
		v.refresh()
		return
	}
	v.selected = slices.IndexFunc(v.commits, func(commit LogCommit) bool {
		return shortCommit(commit.Commit) == row.Commit && logSubject(commit.Message) == row.Message
	})
	v.refresh()
}

func (v *LogView) refresh() {
	v.okBtn.SetEnabled(v.selected >= 0)
	v.hintLabel.SetText(hintText(v.hint()))
}

func (v *LogView) hint() Hint {
	switch {
	case len(v.commits) == 0:
		return Hint{Key: "Dialog.FlowLog.Hint.Empty"}
	case v.selected < 0:
		return Hint{Key: "Dialog.FlowLog.Hint.PickOne"}
	}
	commit := v.commits[v.selected]
	return Hint{Key: "Dialog.FlowLog.Hint.Ready", Args: []any{shortCommit(commit.Commit)}, OK: true}
}

func (v *LogView) confirm() {
	if v.selected < 0 || v.OnOK == nil {
		return
	}
	v.OnOK(v.commits[v.selected])
}

func (v *LogView) cancel() {
	if v.OnCancel != nil {
		v.OnCancel()
	}
}

func shortCommit(id hash.ObjectID) string { return id.String()[:logShortLength] }

func logSubject(message string) string {
	line, _, _ := strings.Cut(message, "\n")
	return line
}
