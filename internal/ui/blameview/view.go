package blameview

import (
	"errors"
	"fmt"
	"image/color"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/oops1/headless-gui/v3/widget"
	"github.com/oops1/headless-gui/v3/widget/datagrid"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/dialogs"
	"github.com/oops1/gogit/internal/ui/journal"
	"github.com/oops1/gogit/internal/ui/style"
)

const dialogName = "blame"

var ErrWidgetMissing = errors.New("blameview: named widget missing")

var loadDialog = dialogs.Load

const (
	shortLength = 7

	rowHeight       = 18
	fontSize        = 9.0
	textHeightRatio = 1.4
	cellPadding     = 4
	badgePadding    = 1
	badgeSize       = 16
	whenWidth       = 74
	numberWidth     = 44

	commitWidth = 58
)

type Line struct {
	Commit  hash.ObjectID
	Author  string
	When    time.Time
	Summary string
	Path    string
	Number  int
	Text    string
}

type Row struct {
	Commit string
	Author string
	When   string
	Number string
	Text   string
}

type View struct {
	dlg       *widget.Dialog
	pathLabel *widget.Label
	table     *widget.DataGridWidget
	hintLabel *widget.Label
	closeBtn  *widget.Button
	lookBtn   *widget.Button

	lines     []Line
	selected  int
	linkColor color.RGBA

	OnInvestigate func(line Line)
	OnCommit      func(id hash.ObjectID)
	OnClose       func()
}

func NewView() (*View, error) {
	dlg, named, err := loadDialog(dialogName, i18n.T("Dialog.Blame.Title"))
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
	p.Quiet(v.lookBtn, v.closeBtn)
	p.Body(v.pathLabel)
	p.Hints(v.hintLabel)
	v.linkColor = p.Accent
}

func (v *View) bind(named map[string]widget.Widget) error {
	var ok bool
	if v.pathLabel, ok = named["pathLabel"].(*widget.Label); !ok {
		return fmt.Errorf("%w: pathLabel", ErrWidgetMissing)
	}
	if v.table, ok = named["lines"].(*widget.DataGridWidget); !ok {
		return fmt.Errorf("%w: lines", ErrWidgetMissing)
	}
	if v.hintLabel, ok = named["hint"].(*widget.Label); !ok {
		return fmt.Errorf("%w: hint", ErrWidgetMissing)
	}
	if v.lookBtn, ok = named["investigate"].(*widget.Button); !ok {
		return fmt.Errorf("%w: investigate", ErrWidgetMissing)
	}
	if v.closeBtn, ok = named["close"].(*widget.Button); !ok {
		return fmt.Errorf("%w: close", ErrWidgetMissing)
	}
	return nil
}

func (v *View) buildColumns() {
	v.table.Grid.RowHeight = rowHeight
	v.table.Grid.FontSize = fontSize
	v.table.Grid.ZebraStripes = false
	commit := datagrid.NewTemplateColumn(i18n.T("Dialog.Blame.Column.Commit"), v.drawCommitCell)
	commit.SetWidth(datagrid.PixelWidth(commitWidth))
	when := datagrid.NewTextColumn(i18n.T("Dialog.Blame.Column.When"), "When")
	when.SetWidth(datagrid.PixelWidth(whenWidth))
	author := datagrid.NewTemplateColumn(i18n.T("Dialog.Blame.Column.Author"), v.drawAuthorCell)
	author.SetWidth(datagrid.PixelWidth(badgeSize + 2*badgePadding))
	number := datagrid.NewTextColumn(i18n.T("Dialog.Blame.Column.Line"), "Number")
	number.SetWidth(datagrid.PixelWidth(numberWidth))
	text := datagrid.NewTextColumn(i18n.T("Dialog.Blame.Column.Text"), "Text")
	text.SetWidth(datagrid.StarWidth(1))
	v.table.Grid.SetColumns([]datagrid.Column{commit, when, author, number, text})
}

func (v *View) drawCommitCell(cdc datagrid.CellDrawContext) {
	row, ok := cdc.Item.(Row)
	if !ok || row.Commit == "" {
		return
	}
	width := cdc.DrawCtx.MeasureText(row.Commit, cdc.FontSize)
	x := cdc.Rect.Min.X + cellPadding
	y := cdc.Rect.Min.Y + (cdc.Rect.Dy()-int(cdc.FontSize*textHeightRatio))/2
	cdc.DrawCtx.DrawTextSize(row.Commit, x, y, cdc.FontSize, v.linkColor)
	cdc.DrawCtx.FillRect(x, y+int(cdc.FontSize*textHeightRatio), width, 1, v.linkColor)
}

func (v *View) drawAuthorCell(cdc datagrid.CellDrawContext) {
	row, ok := cdc.Item.(Row)
	if !ok {
		return
	}
	initials := journal.Initials(row.Author)
	if initials == "" {
		return
	}
	size := min(badgeSize, cdc.Rect.Dy()-2*badgePadding)
	if size <= 0 {
		return
	}
	x := cdc.Rect.Min.X + badgePadding
	y := cdc.Rect.Min.Y + (cdc.Rect.Dy()-size)/2
	cdc.DrawCtx.DrawImageScaled(journal.AuthorBadge(row.Author, size), x, y, size, size)
	textWidth := cdc.DrawCtx.MeasureText(initials, cdc.FontSize)
	textHeight := int(cdc.FontSize * textHeightRatio)
	cdc.DrawCtx.DrawTextSize(initials, x+(size-textWidth)/2, y+(size-textHeight)/2, cdc.FontSize,
		journal.AuthorBadgeTextColor(row.Author))
}

func (v *View) commitOf(index int) (hash.ObjectID, bool) {
	if index < 0 || index >= len(v.lines) {
		return hash.ObjectID{}, false
	}
	return v.lines[index].Commit, true
}

func (v *View) onRowActivated(index int, _ interface{}) {
	if v.OnCommit == nil {
		return
	}
	if id, ok := v.commitOf(index); ok {
		v.OnCommit(id)
	}
}

func (v *View) wire() {
	v.table.Grid.OnSelectionChanged = v.onSelected
	v.table.Grid.OnRowActivated = v.onRowActivated
	v.lookBtn.OnClick = v.investigate
	v.closeBtn.OnClick = v.close
	v.dlg.CancelAction = v.close
}

func (v *View) SetLines(path string, lines []Line) {
	v.pathLabel.SetText(i18n.Tf("Dialog.Blame.Path", path, len(lines)))
	v.lines = slices.Clone(lines)
	v.selected = -1
	items := make([]any, 0, len(v.lines))
	for _, line := range v.lines {
		items = append(items, Row{
			Commit: line.Commit.String()[:shortLength],
			Author: line.Author,
			When:   line.When.Local().Format("2006-01-02"),
			Number: strconv.Itoa(line.Number),
			Text:   strings.TrimRight(line.Text, "\r\n"),
		})
	}
	v.table.Grid.SetItemsSource(datagrid.NewObservableCollectionFrom(items))
	v.refresh()
}

func (v *View) Lines() []Line { return slices.Clone(v.lines) }

func (v *View) Selected() (Line, bool) {
	if v.selected < 0 || v.selected >= len(v.lines) {
		return Line{}, false
	}
	return v.lines[v.selected], true
}

func (v *View) onSelected(ev datagrid.SelectionChangedEvent) {
	row, ok := ev.SelectedItem.(Row)
	if !ok {
		v.selected = -1
		v.refresh()
		return
	}
	v.selected = slices.IndexFunc(v.lines, func(line Line) bool { return strconv.Itoa(line.Number) == row.Number })
	v.refresh()
}

func (v *View) refresh() {
	_, chosen := v.Selected()
	v.lookBtn.SetEnabled(chosen)
	key, args := v.hint()
	v.hintLabel.SetText(i18n.Tf(key, args...))
}

func (v *View) hint() (string, []any) {
	line, chosen := v.Selected()
	switch {
	case len(v.lines) == 0:
		return "Dialog.Blame.Hint.Empty", nil
	case !chosen:
		return "Dialog.Blame.Hint.PickOne", nil
	}
	return "Dialog.Blame.Hint.Line", []any{line.Summary, line.Path}
}

func (v *View) investigate() {
	line, chosen := v.Selected()
	if chosen && v.OnInvestigate != nil {
		v.OnInvestigate(line)
	}
}

func (v *View) close() {
	if v.OnClose != nil {
		v.OnClose()
	}
}
