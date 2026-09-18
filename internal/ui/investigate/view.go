package investigate

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/oops1/headless-gui/v3/widget"
	"github.com/oops1/headless-gui/v3/widget/datagrid"

	"github.com/oops1/gogit/internal/gitcore/diff"
	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/linelog"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/dialogs"
	"github.com/oops1/gogit/internal/ui/style"
)

const (
	dialogName      = "investigate"
	dialogMinWidth  = 640
	dialogMinHeight = 420
	windowMargin    = 48
	shortLength     = 7
)

var ErrWidgetMissing = errors.New("investigate: named widget missing")

var loadDialog = dialogs.LoadResizable

type Entry struct {
	Commit  hash.ObjectID
	Author  string
	When    time.Time
	Subject string
	Merge   bool
	Files   []linelog.File
}

type Row struct {
	Commit  string
	When    string
	Author  string
	Subject string
}

type Side struct {
	Title string
	Text  string
}

type View struct {
	dlg       *widget.Dialog
	pathLabel *widget.Label
	table     *widget.DataGridWidget
	diff      *widget.DiffView
	hintLabel *widget.Label
	cancelBtn *widget.Button
	closeBtn  *widget.Button

	rows     *datagrid.ObservableCollection
	entries  []Entry
	selected int
	running  bool
	done     int
	total    int
	failure  error

	OnCancel func()
	OnClose  func()
}

func NewView() (*View, error) {
	dlg, named, err := loadDialog(dialogName, i18n.T("Dialog.Investigate.Title"))
	if err != nil {
		return nil, err
	}
	v := &View{dlg: dlg, selected: -1, rows: datagrid.NewObservableCollection()}
	if err := v.bind(named); err != nil {
		return nil, err
	}
	dlg.SetMinSize(dialogMinWidth, dialogMinHeight)
	dlg.SetResizable(true)
	v.hintLabel.Muted = true
	v.buildColumns()
	v.table.Grid.SetItemsSource(v.rows)
	v.wire()
	v.refresh()
	return v, nil
}

func (v *View) Dialog() *widget.Dialog { return v.dlg }

func (v *View) Diff() *widget.DiffView { return v.diff }

func (v *View) FitWithin(width, height int) {
	size := v.dlg.Bounds()
	v.dlg.Resize(
		max(dialogMinWidth, min(size.Dx(), width-windowMargin)),
		max(dialogMinHeight, min(size.Dy(), height-windowMargin)),
	)
}

func (v *View) Restyle(t *widget.Theme) {
	p := style.Of(t)
	p.Quiet(v.cancelBtn, v.closeBtn)
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
	if v.diff, ok = named["diff"].(*widget.DiffView); !ok {
		return fmt.Errorf("%w: diff", ErrWidgetMissing)
	}
	if v.hintLabel, ok = named["hint"].(*widget.Label); !ok {
		return fmt.Errorf("%w: hint", ErrWidgetMissing)
	}
	if v.cancelBtn, ok = named["cancel"].(*widget.Button); !ok {
		return fmt.Errorf("%w: cancel", ErrWidgetMissing)
	}
	if v.closeBtn, ok = named["close"].(*widget.Button); !ok {
		return fmt.Errorf("%w: close", ErrWidgetMissing)
	}
	return nil
}

func (v *View) buildColumns() {
	commit := datagrid.NewTextColumn(i18n.T("Dialog.Investigate.Column.Commit"), "Commit")
	commit.SetWidth(datagrid.PixelWidth(90))
	when := datagrid.NewTextColumn(i18n.T("Dialog.Investigate.Column.When"), "When")
	when.SetWidth(datagrid.PixelWidth(140))
	author := datagrid.NewTextColumn(i18n.T("Dialog.Investigate.Column.Author"), "Author")
	author.SetWidth(datagrid.PixelWidth(150))
	subject := datagrid.NewTextColumn(i18n.T("Dialog.Investigate.Column.Subject"), "Subject")
	subject.SetWidth(datagrid.StarWidth(1))
	v.table.Grid.SetColumns([]datagrid.Column{commit, when, author, subject})
}

func (v *View) wire() {
	v.table.Grid.OnSelectionChanged = v.onSelected
	v.cancelBtn.OnClick = v.cancel
	v.closeBtn.OnClick = v.close
	v.dlg.CancelAction = v.close
}

func (v *View) SetTarget(path, lines, rev string) {
	v.pathLabel.SetText(i18n.Tf("Dialog.Investigate.Path", path, lines, rev))
}

func (v *View) Start() {
	v.entries, v.selected, v.failure = nil, -1, nil
	v.done, v.total, v.running = 0, 0, true
	v.rows.Clear()
	v.showSides(Side{}, Side{})
	v.refresh()
}

func (v *View) Append(entry Entry) {
	v.entries = append(v.entries, entry)
	v.rows.Add(Row{
		Commit:  entry.Commit.String()[:shortLength],
		When:    entry.When.Local().Format("2006-01-02 15:04"),
		Author:  entry.Author,
		Subject: entry.Subject,
	})
	if len(v.entries) == 1 {
		v.table.Grid.SetSelectedIndex(0)
		v.show(0)
	}
	v.refresh()
}

func (v *View) Progress(done, total int) {
	v.done, v.total = done, total
	v.refresh()
}

func (v *View) Finish(err error) {
	v.running, v.failure = false, err
	v.refresh()
}

func (v *View) Running() bool { return v.running }

func (v *View) Entries() []Entry { return slices.Clone(v.entries) }

func (v *View) Selected() (Entry, bool) {
	if v.selected < 0 {
		return Entry{}, false
	}
	return v.entries[v.selected], true
}

func (v *View) Hint() string { return v.hintLabel.Text() }

func (v *View) onSelected(ev datagrid.SelectionChangedEvent) {
	if ev.SelectedIndex < 0 || ev.SelectedIndex >= len(v.entries) {
		v.selected = -1
		v.showSides(Side{}, Side{})
		v.refresh()
		return
	}
	v.show(ev.SelectedIndex)
	v.refresh()
}

func (v *View) show(index int) {
	v.selected = index
	v.showSides(Sides(v.entries[index]))
}

func (v *View) showSides(left, right Side) {
	v.diff.SetText(widget.DiffLeft, left.Title, "", left.Text)
	v.diff.SetText(widget.DiffRight, right.Title, "", right.Text)
}

func (v *View) refresh() {
	v.cancelBtn.SetEnabled(v.running)
	key, args := v.hint()
	v.hintLabel.SetText(i18n.Tf(key, args...))
}

func (v *View) hint() (string, []any) {
	found := len(v.entries)
	switch {
	case errors.Is(v.failure, context.Canceled):
		return "Dialog.Investigate.Hint.Cancelled", []any{found}
	case v.failure != nil:
		return "Dialog.Investigate.Hint.Failed", []any{v.failure}
	case v.running:
		return "Dialog.Investigate.Hint.Running", []any{found, v.done, v.total}
	case found == 0:
		return "Dialog.Investigate.Hint.Empty", nil
	case v.selected >= 0 && v.entries[v.selected].Merge:
		return "Dialog.Investigate.Hint.Merge", nil
	}
	return "Dialog.Investigate.Hint.Found", []any{found}
}

func (v *View) cancel() {
	if v.OnCancel != nil {
		v.OnCancel()
	}
}

func (v *View) close() {
	if v.OnClose != nil {
		v.OnClose()
	}
}

func Sides(entry Entry) (Side, Side) {
	var left, right Side
	var oldText, newText strings.Builder
	var oldTitles, newTitles []string
	for _, file := range entry.Files {
		if !file.Created {
			oldTitles = append(oldTitles, file.OldPath)
		}
		newTitles = append(newTitles, file.NewPath)
		for _, hunk := range file.Hunks {
			header := fmt.Sprintf("@@ -%d,%d +%d,%d @@\n", hunk.OldStart, hunk.OldLines, hunk.NewStart, hunk.NewLines)
			oldText.WriteString(header)
			newText.WriteString(header)
			for _, line := range hunk.Lines {
				if line.Kind != diff.KindAdd {
					oldText.WriteString(line.Text + "\n")
				}
				if line.Kind != diff.KindDel {
					newText.WriteString(line.Text + "\n")
				}
			}
		}
	}
	left.Title, left.Text = strings.Join(oldTitles, ", "), oldText.String()
	right.Title, right.Text = strings.Join(newTitles, ", "), newText.String()
	return left, right
}
