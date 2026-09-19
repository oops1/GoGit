package console

import (
	"errors"
	"image"
	"image/color"
	"slices"
	"testing"

	"github.com/oops1/headless-gui/v3/widget"
	"github.com/oops1/headless-gui/v3/widget/datagrid"

	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/style"
)

func newTestView(t *testing.T) *View {
	t.Helper()
	setupI18N(t)
	v, err := NewView()
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func fullNamedWidgets() map[string]widget.Widget {
	return map[string]widget.Widget{
		"output": widget.NewDataGridWidget(),
		"input":  NewInput(),
		"hint":   widget.NewLabel("", widget.CurrentTheme().LabelText),
		"clear":  widget.NewButton(""),
		"close":  widget.NewButton(""),
	}
}

func TestNewViewPropagatesLoadDialogError(t *testing.T) {
	setupI18N(t)
	wantErr := errors.New("boom")
	prev := loadDialog
	loadDialog = func(string, string) (*widget.Dialog, map[string]widget.Widget, error) { return nil, nil, wantErr }
	t.Cleanup(func() { loadDialog = prev })

	if _, err := NewView(); !errors.Is(err, wantErr) {
		t.Fatalf("err = %v", err)
	}
}

func TestNewViewReportsEveryMissingWidget(t *testing.T) {
	setupI18N(t)
	prev := loadDialog
	t.Cleanup(func() { loadDialog = prev })
	for name := range fullNamedWidgets() {
		named := fullNamedWidgets()
		delete(named, name)
		loadDialog = func(string, string) (*widget.Dialog, map[string]widget.Widget, error) {
			return widget.NewDialog("", 100, 100), named, nil
		}
		if _, err := NewView(); !errors.Is(err, ErrWidgetMissing) {
			t.Fatalf("without %s: err = %v", name, err)
		}
	}
}

func TestTheConsoleStartsEmptyAndReady(t *testing.T) {
	v := newTestView(t)

	if len(v.Lines()) != 0 || v.Busy() || v.clearBtn.IsEnabled() {
		t.Fatalf("lines = %#v, busy = %v", v.Lines(), v.Busy())
	}
	if v.hint.Text() != i18n.T("Dialog.Console.Hint.Ready") {
		t.Fatalf("hint = %q", v.hint.Text())
	}
	if v.input.Placeholder != i18n.T("Dialog.Console.Placeholder") {
		t.Fatalf("placeholder = %q", v.input.Placeholder)
	}
	if got := v.output.Grid.Columns(); len(got) != 1 {
		t.Fatalf("columns = %d", len(got))
	}
}

func TestSubmitEchoesTheCommandAndWaitsForTheAnswer(t *testing.T) {
	v := newTestView(t)
	var asked string
	v.OnSubmit = func(line string) { asked = line }

	v.SetText("  status --porcelain  ")
	v.input.OnEnter()

	if asked != "status --porcelain" {
		t.Fatalf("asked = %q", asked)
	}
	if v.Text() != "" {
		t.Fatalf("the input must be cleared, got %q", v.Text())
	}
	if !v.Busy() || v.input.IsEnabled() {
		t.Fatal("the console must be busy while the command runs")
	}
	if got := v.Lines(); len(got) != 1 || got[0] != (Line{Text: promptPrefix + "status --porcelain", Kind: LineCommand}) {
		t.Fatalf("lines = %#v", got)
	}
	if v.hint.Text() != i18n.T("Dialog.Console.Hint.Running") {
		t.Fatalf("hint = %q", v.hint.Text())
	}
}

func TestFinishAppendsTheOutputAndFreesTheInput(t *testing.T) {
	v := newTestView(t)
	v.OnSubmit = func(string) {}
	v.SetText("status")
	v.Submit()

	v.Finish("M  a.txt\n?? b.txt\n", nil)

	if !slices.Equal(v.Texts(), []string{"> status", "M  a.txt", "?? b.txt"}) {
		t.Fatalf("texts = %#v", v.Texts())
	}
	if v.Busy() || !v.input.IsEnabled() || !v.clearBtn.IsEnabled() {
		t.Fatal("the console must be ready again")
	}
}

func TestFinishShowsTheFailureInItsOwnColour(t *testing.T) {
	v := newTestView(t)
	v.OnSubmit = func(string) {}
	v.SetText("frobnicate")
	v.Submit()

	v.Finish("", detail(ErrUnknownCommand, "frobnicate"))

	got := v.Lines()
	if len(got) != 2 || got[1].Kind != LineFailure {
		t.Fatalf("lines = %#v", got)
	}
	if got[1].Text != i18n.Tf("Console.Error.UnknownCommand", "frobnicate") {
		t.Fatalf("failure = %q", got[1].Text)
	}
}

func TestAnEmptyOutputAddsNoLines(t *testing.T) {
	v := newTestView(t)
	v.OnSubmit = func(string) {}
	v.SetText("status")
	v.Submit()

	v.Finish("", nil)

	if !slices.Equal(v.Texts(), []string{"> status"}) {
		t.Fatalf("texts = %#v", v.Texts())
	}
}

func TestSubmitIgnoresBlankLinesAndARunningCommand(t *testing.T) {
	v := newTestView(t)
	calls := 0
	v.OnSubmit = func(string) { calls++ }

	v.SetText("   ")
	v.Submit()
	v.SetText("status")
	v.Submit()
	v.SetText("log")
	v.Submit()

	if calls != 1 || len(v.Lines()) != 1 {
		t.Fatalf("calls = %d, lines = %#v", calls, v.Lines())
	}
}

func TestSubmitWithoutAHandlerDoesNothing(t *testing.T) {
	v := newTestView(t)

	v.SetText("status")
	v.Submit()

	if len(v.Lines()) != 0 || v.Busy() {
		t.Fatalf("lines = %#v, busy = %v", v.Lines(), v.Busy())
	}
}

func TestTheArrowKeysWalkTheHistory(t *testing.T) {
	v := newTestView(t)
	v.OnSubmit = func(string) {}
	for _, line := range []string{"status", "log"} {
		v.SetText(line)
		v.Submit()
		v.Finish("", nil)
	}

	v.input.OnKeyEvent(widget.KeyEvent{Code: widget.KeyUp, Pressed: true})
	if v.Text() != "log" {
		t.Fatalf("text = %q", v.Text())
	}
	v.input.OnKeyEvent(widget.KeyEvent{Code: widget.KeyUp, Pressed: true})
	if v.Text() != "status" {
		t.Fatalf("text = %q", v.Text())
	}
	v.input.OnKeyEvent(widget.KeyEvent{Code: widget.KeyDown, Pressed: true})
	if v.Text() != "log" {
		t.Fatalf("text = %q", v.Text())
	}
	if !slices.Equal(v.History(), []string{"status", "log"}) {
		t.Fatalf("history = %#v", v.History())
	}
}

func TestTheArrowKeysStopAtTheEndsOfTheHistory(t *testing.T) {
	v := newTestView(t)

	v.SetText("kept")
	v.input.OnKeyEvent(widget.KeyEvent{Code: widget.KeyUp, Pressed: true})
	v.input.OnKeyEvent(widget.KeyEvent{Code: widget.KeyDown, Pressed: true})

	if v.Text() != "kept" {
		t.Fatalf("text = %q", v.Text())
	}
}

func TestOtherKeysReachTheTextField(t *testing.T) {
	v := newTestView(t)

	v.input.SetFocused(true)
	v.input.OnKeyEvent(widget.KeyEvent{Code: widget.KeyUnknown, Rune: 'a', Pressed: true})
	v.input.OnKeyEvent(widget.KeyEvent{Code: widget.KeyUp, Pressed: false})

	if v.Text() != "a" {
		t.Fatalf("text = %q", v.Text())
	}
}

func TestAnInputWithoutHandlersKeepsTyping(t *testing.T) {
	in := NewInput()
	in.SetFocused(true)

	in.OnKeyEvent(widget.KeyEvent{Code: widget.KeyUp, Pressed: true})
	in.OnKeyEvent(widget.KeyEvent{Code: widget.KeyDown, Pressed: true})
	in.OnKeyEvent(widget.KeyEvent{Code: widget.KeyUnknown, Rune: 'z', Pressed: true})

	if in.GetText() != "z" {
		t.Fatalf("text = %q", in.GetText())
	}
}

func TestTheXAMLLoaderBuildsTheInput(t *testing.T) {
	Register()
	built, err := buildFromXAML(nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := built.(*Input); !ok {
		t.Fatalf("built = %T", built)
	}
}

func TestClearEmptiesTheTranscript(t *testing.T) {
	v := newTestView(t)
	v.OnSubmit = func(string) {}
	v.SetText("status")
	v.Submit()
	v.Finish("done", nil)

	v.clearBtn.OnClick()

	if len(v.Lines()) != 0 || v.clearBtn.IsEnabled() {
		t.Fatalf("lines = %#v", v.Lines())
	}
}

func TestTheTranscriptForgetsTheOldestLines(t *testing.T) {
	v := newTestView(t)
	v.OnSubmit = func(string) {}
	v.SetText("status")
	v.Submit()
	for range maxOutputLines {
		v.append(Line{Text: "line"})
	}

	got := v.Lines()
	if len(got) != maxOutputLines || got[0].Text != "line" {
		t.Fatalf("lines = %d, first = %q", len(got), got[0].Text)
	}
}

func TestCloseReachesTheCallback(t *testing.T) {
	v := newTestView(t)
	closed := 0
	v.OnClose = func() { closed++ }

	v.Dialog().CancelAction()
	v.closeBtn.OnClick()

	if closed != 2 {
		t.Fatalf("closed %d times", closed)
	}
}

func TestClosingWithoutACallbackIsSafe(t *testing.T) {
	v := newTestView(t)
	v.Dialog().CancelAction()
}

func TestRestyleAcceptsBothThemes(t *testing.T) {
	v := newTestView(t)
	v.OnSubmit = func(string) {}
	v.SetText("status")
	v.Submit()

	for _, theme := range []*widget.Theme{widget.Win11DarkTheme(), widget.Win11LightTheme()} {
		v.Restyle(theme)
		if v.failureColor != style.Of(theme).DeletedText {
			t.Fatalf("failure colour = %v", v.failureColor)
		}
	}
}

type recordingDrawCtx struct {
	texts  []string
	colors []color.RGBA
}

func (c *recordingDrawCtx) FillRect(int, int, int, int, color.RGBA)                   {}
func (c *recordingDrawCtx) FillRectAlpha(int, int, int, int, color.RGBA)              {}
func (c *recordingDrawCtx) FillRoundRect(int, int, int, int, int, color.RGBA)         {}
func (c *recordingDrawCtx) DrawLineAA(int, int, int, int, float64, color.RGBA)        {}
func (c *recordingDrawCtx) StrokePolylineAA([]image.Point, float64, bool, color.RGBA) {}
func (c *recordingDrawCtx) StrokeEllipseAA(int, int, int, int, float64, color.RGBA)   {}
func (c *recordingDrawCtx) FillPolygonAA([]image.Point, color.RGBA)                   {}
func (c *recordingDrawCtx) FillEllipseAA(int, int, int, int, color.RGBA)              {}
func (c *recordingDrawCtx) DrawBorder(int, int, int, int, color.RGBA)                 {}
func (c *recordingDrawCtx) DrawText(text string, x, y int, col color.RGBA) {
	c.DrawTextSize(text, x, y, 12, col)
}
func (c *recordingDrawCtx) MeasureText(text string, _ float64) int          { return len(text) * 6 }
func (c *recordingDrawCtx) SetClip(image.Rectangle)                         {}
func (c *recordingDrawCtx) ClearClip()                                      {}
func (c *recordingDrawCtx) DrawHLine(int, int, int, color.RGBA)             {}
func (c *recordingDrawCtx) DrawVLine(int, int, int, color.RGBA)             {}
func (c *recordingDrawCtx) DrawImage(image.Image, int, int)                 {}
func (c *recordingDrawCtx) DrawImageScaled(image.Image, int, int, int, int) {}

func (c *recordingDrawCtx) DrawTextSize(text string, _, _ int, _ float64, col color.RGBA) {
	c.texts = append(c.texts, text)
	c.colors = append(c.colors, col)
}

func cellContext(item any, dc datagrid.DrawContextBridge) datagrid.CellDrawContext {
	return datagrid.CellDrawContext{
		Rect:      image.Rect(0, 0, 400, outputRowHeight),
		Item:      item,
		DrawCtx:   dc,
		TextColor: color.RGBA{A: 0xFF},
		FontSize:  12,
	}
}

func TestEachKindOfLineGetsItsOwnColour(t *testing.T) {
	v := newTestView(t)
	fallback := color.RGBA{A: 0xFF}
	tests := []struct {
		name string
		line Line
		want color.RGBA
	}{
		{"output", Line{Text: "plain"}, fallback},
		{"command", Line{Text: "> status", Kind: LineCommand}, v.commandColor},
		{"failure", Line{Text: "boom", Kind: LineFailure}, v.failureColor},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dc := &recordingDrawCtx{}
			v.drawLineCell(cellContext(tt.line, dc))
			if len(dc.texts) != 1 || dc.texts[0] != tt.line.Text {
				t.Fatalf("texts = %#v", dc.texts)
			}
			if dc.colors[0] != tt.want {
				t.Fatalf("colour = %v, want %v", dc.colors[0], tt.want)
			}
		})
	}
}

func TestADrawnRowThatIsNotALineIsSkipped(t *testing.T) {
	v := newTestView(t)
	dc := &recordingDrawCtx{}

	v.drawLineCell(cellContext("not a line", dc))

	if len(dc.texts) != 0 {
		t.Fatalf("texts = %#v", dc.texts)
	}
}
