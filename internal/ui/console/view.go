package console

import (
	"errors"
	"fmt"
	"image/color"
	"slices"
	"strings"

	"github.com/oops1/headless-gui/v3/widget"
	"github.com/oops1/headless-gui/v3/widget/datagrid"

	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/dialogs"
	"github.com/oops1/gogit/internal/ui/style"
)

const dialogName = "console"

const (
	promptPrefix    = "> "
	maxOutputLines  = 5000
	cellPadding     = 6
	cellTextHeight  = 14
	outputRowHeight = 18
)

var ErrWidgetMissing = errors.New("console: named widget missing")

var loadDialog = dialogs.Load

type LineKind uint8

const (
	LineOutput LineKind = iota
	LineCommand
	LineFailure
)

type Line struct {
	Text string
	Kind LineKind
}

type View struct {
	dlg      *widget.Dialog
	output   *widget.DataGridWidget
	input    *Input
	hint     *widget.Label
	clearBtn *widget.Button
	closeBtn *widget.Button

	lines   []Line
	history History
	busy    bool

	commandColor color.RGBA
	failureColor color.RGBA

	OnSubmit func(line string)
	OnClose  func()
}

func NewView() (*View, error) {
	Register()
	dlg, named, err := loadDialog(dialogName, i18n.T("Dialog.Console.Title"))
	if err != nil {
		return nil, err
	}
	v := &View{dlg: dlg}
	if err := v.bind(named); err != nil {
		return nil, err
	}
	v.applyColors(widget.CurrentTheme())
	v.buildColumns()
	v.wire()
	v.input.Placeholder = i18n.T("Dialog.Console.Placeholder")
	v.hint.Muted = true
	v.refresh()
	return v, nil
}

func (v *View) Dialog() *widget.Dialog { return v.dlg }

func (v *View) Restyle(t *widget.Theme) {
	p := style.Of(t)
	p.Quiet(v.clearBtn, v.closeBtn)
	p.Hints(v.hint)
	p.Fields(v.input.TextInput)
	v.applyColors(t)
	v.showLines()
}

func (v *View) applyColors(t *widget.Theme) {
	p := style.Of(t)
	v.commandColor = p.Accent
	v.failureColor = p.DeletedText
}

func (v *View) bind(named map[string]widget.Widget) error {
	var ok bool
	if v.output, ok = named["output"].(*widget.DataGridWidget); !ok {
		return fmt.Errorf("%w: output", ErrWidgetMissing)
	}
	if v.input, ok = named["input"].(*Input); !ok {
		return fmt.Errorf("%w: input", ErrWidgetMissing)
	}
	if v.hint, ok = named["hint"].(*widget.Label); !ok {
		return fmt.Errorf("%w: hint", ErrWidgetMissing)
	}
	if v.clearBtn, ok = named["clear"].(*widget.Button); !ok {
		return fmt.Errorf("%w: clear", ErrWidgetMissing)
	}
	if v.closeBtn, ok = named["close"].(*widget.Button); !ok {
		return fmt.Errorf("%w: close", ErrWidgetMissing)
	}
	return nil
}

func (v *View) buildColumns() {
	text := datagrid.NewTemplateColumn("", v.drawLineCell)
	text.SetWidth(datagrid.StarWidth(1))
	v.output.Grid.SetColumns([]datagrid.Column{text})
	v.output.Grid.HeaderHeight = 0
	v.output.Grid.RowHeight = outputRowHeight
	v.output.Grid.EmptyStateText = i18n.T("Dialog.Console.Empty")
	v.output.Grid.EmptyStateColor = widget.CurrentTheme().SecondaryText
}

func (v *View) wire() {
	v.input.OnEnter = v.Submit
	v.input.OnPrevious = v.showPrevious
	v.input.OnNext = v.showNext
	v.clearBtn.OnClick = v.Clear
	v.closeBtn.OnClick = v.close
	v.dlg.CancelAction = v.close
}

func (v *View) drawLineCell(cdc datagrid.CellDrawContext) {
	line, ok := cdc.Item.(Line)
	if !ok {
		return
	}
	r := cdc.Rect
	textX := r.Min.X + cellPadding
	textY := r.Min.Y + (r.Dy()-cellTextHeight)/2
	maxWidth := r.Max.X - textX - cellPadding
	shown := datagrid.EllipsizeText(cdc.DrawCtx, line.Text, maxWidth, cdc.FontSize)
	cdc.DrawCtx.DrawTextSize(shown, textX, textY, cdc.FontSize, v.lineColor(line.Kind, cdc.TextColor))
}

func (v *View) lineColor(kind LineKind, fallback color.RGBA) color.RGBA {
	switch kind {
	case LineCommand:
		return v.commandColor
	case LineFailure:
		return v.failureColor
	}
	return fallback
}

func (v *View) Submit() {
	if v.busy || v.OnSubmit == nil {
		return
	}
	line := strings.TrimSpace(v.input.GetText())
	if line == "" {
		return
	}
	v.history.Add(line)
	v.input.SetText("")
	v.append(Line{Text: promptPrefix + line, Kind: LineCommand})
	v.setBusy(true)
	v.OnSubmit(line)
}

func (v *View) Finish(output string, err error) {
	for _, text := range outputLines(output) {
		v.append(Line{Text: text})
	}
	if err != nil {
		v.append(Line{Text: Message(err), Kind: LineFailure})
	}
	v.setBusy(false)
}

func outputLines(output string) []string {
	trimmed := strings.TrimRight(output, "\n")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "\n")
}

func (v *View) append(line Line) {
	v.lines = append(v.lines, line)
	if len(v.lines) > maxOutputLines {
		v.lines = slices.Clone(v.lines[len(v.lines)-maxOutputLines:])
	}
	v.showLines()
}

func (v *View) showLines() {
	items := make([]any, 0, len(v.lines))
	for _, line := range v.lines {
		items = append(items, line)
	}
	v.output.Grid.SetItemsSource(datagrid.NewObservableCollectionFrom(items))
	v.output.Grid.ScrollBy(len(items) * outputRowHeight)
}

func (v *View) Clear() {
	v.lines = nil
	v.showLines()
	v.refresh()
}

func (v *View) Lines() []Line { return slices.Clone(v.lines) }

func (v *View) Texts() []string {
	texts := make([]string, 0, len(v.lines))
	for _, line := range v.lines {
		texts = append(texts, line.Text)
	}
	return texts
}

func (v *View) History() []string { return v.history.Items() }

func (v *View) Busy() bool { return v.busy }

func (v *View) SetText(text string) { v.input.SetText(text) }

func (v *View) Text() string { return v.input.GetText() }

func (v *View) setBusy(busy bool) {
	v.busy = busy
	v.input.SetEnabled(!busy)
	v.refresh()
}

func (v *View) refresh() {
	v.clearBtn.SetEnabled(!v.busy && len(v.lines) > 0)
	key := "Dialog.Console.Hint.Ready"
	if v.busy {
		key = "Dialog.Console.Hint.Running"
	}
	v.hint.SetText(i18n.T(key))
}

func (v *View) showPrevious() {
	if text, ok := v.history.Previous(); ok {
		v.input.SetText(text)
	}
}

func (v *View) showNext() {
	if text, ok := v.history.Next(); ok {
		v.input.SetText(text)
	}
}

func (v *View) close() {
	if v.OnClose != nil {
		v.OnClose()
	}
}
