package search

import (
	"errors"
	"image/color"
	"testing"

	"github.com/oops1/headless-gui/v3/engine"
	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/style"
)

func newTestView(t *testing.T) *View {
	t.Helper()
	widget.ClearStrings()
	t.Cleanup(widget.ClearStrings)
	if _, err := i18n.Install(""); err != nil {
		t.Fatal(err)
	}
	i18n.Apply("en")
	eng := engine.New(800, 600, 30)
	v, err := NewView(eng)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func clickButton(btn *widget.Button) {
	btn.OnMouseButton(widget.MouseEvent{Button: widget.MouseLeft, Pressed: true})
	btn.OnMouseButton(widget.MouseEvent{Button: widget.MouseLeft, Pressed: false})
}

func fullWidgetSet() map[string]widget.Widget {
	return map[string]widget.Widget{
		"root":        widget.NewTextInput(""),
		"browse":      widget.NewButton(""),
		"includeBare": widget.NewCheckBox(""),
		"scan":        widget.NewButton(""),
		"status":      widget.NewWin10Label(""),
		"results":     widget.NewDataGridWidget(),
		"cancel":      widget.NewButton(""),
		"add":         widget.NewButton(""),
	}
}

func TestNewViewPropagatesLoadDialogError(t *testing.T) {
	widget.ClearStrings()
	defer widget.ClearStrings()
	prev := loadDialog
	wantErr := errors.New("boom")
	loadDialog = func(name, title string) (*widget.Dialog, map[string]widget.Widget, error) {
		return nil, nil, wantErr
	}
	defer func() { loadDialog = prev }()

	eng := engine.New(800, 600, 30)
	if _, err := NewView(eng); !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
}

func TestNewViewPropagatesBindError(t *testing.T) {
	widget.ClearStrings()
	defer widget.ClearStrings()
	prev := loadDialog
	loadDialog = func(name, title string) (*widget.Dialog, map[string]widget.Widget, error) {
		return widget.NewDialog(title, 10, 10), map[string]widget.Widget{}, nil
	}
	defer func() { loadDialog = prev }()

	eng := engine.New(800, 600, 30)
	if _, err := NewView(eng); !errors.Is(err, ErrWidgetMissing) {
		t.Fatalf("err = %v, want %v", err, ErrWidgetMissing)
	}
}

func TestBindReturnsErrorForEachMissingOrMistypedWidget(t *testing.T) {
	for _, key := range []string{"root", "browse", "includeBare", "scan", "status", "results", "cancel", "add"} {
		named := fullWidgetSet()
		delete(named, key)
		v := &View{}
		if err := v.bind(named); err == nil {
			t.Fatalf("missing %q: expected error", key)
		}
	}
	for key := range fullWidgetSet() {
		named := fullWidgetSet()
		named[key] = widget.NewWin10Label("wrong-type")
		if key == "status" {
			named[key] = widget.NewButton("wrong-type")
		}
		v := &View{}
		if err := v.bind(named); err == nil {
			t.Fatalf("mistyped %q: expected error", key)
		}
	}
}

func TestBindSucceedsWithAllWidgetsPresent(t *testing.T) {
	v := &View{}
	if err := v.bind(fullWidgetSet()); err != nil {
		t.Fatal(err)
	}
}

func TestNewViewSetsDialogTitle(t *testing.T) {
	v := newTestView(t)
	if v.Dialog().Title != i18n.T("Dialog.Search.Title") {
		t.Fatalf("title = %q", v.Dialog().Title)
	}
}

func TestBrowseClickInvokesCallback(t *testing.T) {
	v := newTestView(t)
	called := 0
	v.OnBrowse = func() { called++ }

	clickButton(v.browseBtn)

	if called != 1 {
		t.Fatalf("OnBrowse called %d times, want 1", called)
	}
}

func TestBrowseClickToleratesNilCallback(t *testing.T) {
	v := newTestView(t)
	clickButton(v.browseBtn)
}

func TestScanClickInvokesCallbackWithRootAndIncludeBare(t *testing.T) {
	v := newTestView(t)
	v.SetRoot("/some/dir")
	v.includeBare.SetChecked(true)
	var gotRoot string
	var gotBare bool
	v.OnScan = func(root string, includeBare bool) {
		gotRoot, gotBare = root, includeBare
	}

	clickButton(v.scanBtn)

	if gotRoot != "/some/dir" || !gotBare {
		t.Fatalf("OnScan args = %q, %v", gotRoot, gotBare)
	}
}

func TestScanClickToleratesNilCallback(t *testing.T) {
	v := newTestView(t)
	clickButton(v.scanBtn)
}

func TestCancelClickInvokesCallback(t *testing.T) {
	v := newTestView(t)
	called := 0
	v.OnCancel = func() { called++ }

	clickButton(v.cancelBtn)

	if called != 1 {
		t.Fatalf("OnCancel called %d times, want 1", called)
	}
}

func TestCancelClickToleratesNilCallback(t *testing.T) {
	v := newTestView(t)
	clickButton(v.cancelBtn)
}

func TestAddClickToleratesNilCallback(t *testing.T) {
	v := newTestView(t)
	clickButton(v.addBtn)
}

func TestAddClickWithNoSelectionAddsAllFoundPaths(t *testing.T) {
	v := newTestView(t)
	v.SetResults([]Found{{Path: "a"}, {Path: "b", Bare: true}})
	var got []string
	v.OnAdd = func(paths []string) { got = paths }

	clickButton(v.addBtn)

	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("OnAdd paths = %v, want [a b]", got)
	}
}

func TestAddClickWithSelectionAddsOnlySelectedPaths(t *testing.T) {
	v := newTestView(t)
	v.SetResults([]Found{{Path: "a"}, {Path: "b"}, {Path: "c"}})
	v.resultsTable.Grid.SetSelectedIndex(1)
	var got []string
	v.OnAdd = func(paths []string) { got = paths }

	clickButton(v.addBtn)

	if len(got) != 1 || got[0] != "b" {
		t.Fatalf("OnAdd paths = %v, want [b]", got)
	}
}

func TestSetRootAndRoot(t *testing.T) {
	v := newTestView(t)
	v.SetRoot("C:\\repos")
	if got := v.Root(); got != "C:\\repos" {
		t.Fatalf("Root() = %q", got)
	}
}

func TestSetResultsPopulatesItemsSource(t *testing.T) {
	v := newTestView(t)
	v.SetResults([]Found{{Path: "a"}, {Path: "b"}})
	if got := v.resultsTable.Grid.ItemsSource().Count(); got != 2 {
		t.Fatalf("items source length = %d, want 2", got)
	}
}

func TestSetStatusSetsTextAndColor(t *testing.T) {
	v := newTestView(t)
	want := color.RGBA{R: 1, G: 2, B: 3, A: 4}
	v.SetStatus("hello", want)
	if v.statusLabel.Text() != "hello" {
		t.Fatalf("status text = %q", v.statusLabel.Text())
	}
	if v.statusLabel.TextColor != want {
		t.Fatalf("status color = %+v, want %+v", v.statusLabel.TextColor, want)
	}
}

func TestSetScanningDisablesScanAndAddButtons(t *testing.T) {
	v := newTestView(t)
	v.SetScanning(true)
	if v.scanBtn.IsEnabled() || v.addBtn.IsEnabled() {
		t.Fatal("scan and add must be disabled while scanning")
	}
	v.SetScanning(false)
	if !v.scanBtn.IsEnabled() || !v.addBtn.IsEnabled() {
		t.Fatal("scan and add must be enabled once scanning stops")
	}
}

func TestDialogDefaultActionTriggersAdd(t *testing.T) {
	v := newTestView(t)
	v.SetResults([]Found{{Path: "a"}})
	var got []string
	v.OnAdd = func(paths []string) { got = paths }

	v.Dialog().DefaultAction()

	if len(got) != 1 || got[0] != "a" {
		t.Fatalf("DefaultAction did not trigger add, got %v", got)
	}
}

func TestDialogCancelActionTriggersCancel(t *testing.T) {
	v := newTestView(t)
	called := 0
	v.OnCancel = func() { called++ }

	v.Dialog().CancelAction()

	if called != 1 {
		t.Fatalf("CancelAction: OnCancel called %d times, want 1", called)
	}
}

func TestTheDialogWearsTheColoursOfTheTheme(t *testing.T) {
	theme := widget.Win11DarkTheme()
	p := style.Of(theme)
	v := newTestView(t)

	v.Restyle(theme)

	if v.rootInput.Background != p.Field || v.rootInput.BorderColor != p.Border || v.rootInput.PaddingX != style.FieldPaddingX {
		t.Fatalf("field = %v on %v, want the shared field style", v.rootInput.BorderColor, v.rootInput.Background)
	}
	if v.addBtn.Background != p.Accent || v.addBtn.TextColor != p.OnAccent {
		t.Fatalf("main button = %v on %v, want the accent", v.addBtn.TextColor, v.addBtn.Background)
	}
	if v.cancelBtn.Background != p.Field || v.cancelBtn.BorderColor != p.Border {
		t.Fatalf("quiet button = %v, want the field fill", v.cancelBtn.Background)
	}
}
