package settings

import (
	"errors"
	"testing"

	"github.com/oops1/headless-gui/v3/engine"
	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/config"
	"github.com/oops1/gogit/internal/i18n"
)

func newTestView(t *testing.T, languages []string, initial Model) *View {
	t.Helper()
	widget.ClearStrings()
	t.Cleanup(widget.ClearStrings)
	if _, err := i18n.Install(""); err != nil {
		t.Fatal(err)
	}
	i18n.Apply("en")
	eng := engine.New(800, 600, 30)
	v, err := NewView(eng, languages, initial)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func clickCheckBox(cb *widget.CheckBox) {
	cb.OnMouseButton(widget.MouseEvent{Button: widget.MouseLeft, Pressed: true})
	cb.OnMouseButton(widget.MouseEvent{Button: widget.MouseLeft, Pressed: false})
}

func fullNamedWidgets() map[string]widget.Widget {
	return map[string]widget.Widget{
		"tabs":          widget.NewTabControl(),
		"language":      widget.NewDropdown(),
		"theme":         widget.NewDropdown(),
		"showToolbar":   widget.NewCheckBox(""),
		"showStatusBar": widget.NewCheckBox(""),
		"logMaxCount":   widget.NewNumericUpDown(),
		"autoFetch":     widget.NewCheckBox(""),
		"fetchInterval": widget.NewNumericUpDown(),
		"workTreeDepth": widget.NewNumericUpDown(),
		"ok":            widget.NewButton(""),
		"cancel":        widget.NewButton(""),
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
	if _, err := NewView(eng, nil, Model{}); !errors.Is(err, wantErr) {
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
	if _, err := NewView(eng, nil, Model{}); !errors.Is(err, ErrWidgetMissing) {
		t.Fatalf("err = %v, want %v", err, ErrWidgetMissing)
	}
}

func TestBindReturnsErrorForEachMissingOrMistypedWidget(t *testing.T) {
	keys := []string{"tabs", "language", "theme", "showToolbar", "showStatusBar", "logMaxCount", "autoFetch", "fetchInterval", "workTreeDepth", "ok", "cancel"}
	for _, key := range keys {
		named := fullNamedWidgets()
		delete(named, key)
		v := &View{}
		if err := v.bind(named); err == nil {
			t.Fatalf("missing %q: expected error", key)
		}
	}
	for _, key := range keys {
		named := fullNamedWidgets()
		named[key] = widget.NewWin10Label("wrong-type")
		v := &View{}
		if err := v.bind(named); err == nil {
			t.Fatalf("mistyped %q: expected error", key)
		}
	}
}

func TestBindSucceedsWithAllWidgetsPresent(t *testing.T) {
	v := &View{}
	if err := v.bind(fullNamedWidgets()); err != nil {
		t.Fatal(err)
	}
}

func TestNewViewAppliesInitialModelToWidgets(t *testing.T) {
	initial := Model{
		Language:      "ru",
		Theme:         config.ThemeDark,
		ShowToolbar:   false,
		ShowStatusBar: true,
		LogMaxCount:   777,
		AutoFetch:     true,
		FetchInterval: 90,
		WorkTreeDepth: 6,
	}
	v := newTestView(t, []string{"en", "ru"}, initial)

	if v.language.Selected() != 1 {
		t.Fatalf("language selection = %d, want 1", v.language.Selected())
	}
	if v.theme.Selected() != themeIndex(config.ThemeDark) {
		t.Fatalf("theme selection = %d", v.theme.Selected())
	}
	if v.showToolbar.IsChecked() {
		t.Fatal("show toolbar must be unchecked")
	}
	if !v.showStatusBar.IsChecked() {
		t.Fatal("show status bar must be checked")
	}
	if v.logMaxCount.Value() != 777 {
		t.Fatalf("logMaxCount = %v", v.logMaxCount.Value())
	}
	if !v.autoFetch.IsChecked() {
		t.Fatal("auto-fetch must be checked")
	}
	if v.fetchInterval.Value() != 90 {
		t.Fatalf("fetchInterval = %v", v.fetchInterval.Value())
	}
	if v.workTreeDepth.Value() != 6 {
		t.Fatalf("workTreeDepth = %v", v.workTreeDepth.Value())
	}
}

func TestPopulateLanguagesUsesLocalizedLabelsAndFallsBackToCode(t *testing.T) {
	v := newTestView(t, []string{"en", "xx"}, Model{})
	items := v.language.Items()
	if len(items) != 2 || items[0] != "English" || items[1] != "xx" {
		t.Fatalf("language items = %v", items)
	}
}

func TestSetLanguageSelectionFallsBackWhenCodeNotInList(t *testing.T) {
	v := newTestView(t, []string{"en", "ru"}, Model{Language: "de"})
	if v.language.Selected() != 0 {
		t.Fatalf("selection = %d, want 0", v.language.Selected())
	}
}

func TestThemeIndexAndThemeAtRoundTripKnownValues(t *testing.T) {
	for i, theme := range themeOrder {
		if themeIndex(theme) != i {
			t.Fatalf("themeIndex(%q) = %d, want %d", theme, themeIndex(theme), i)
		}
		if themeAt(i) != theme {
			t.Fatalf("themeAt(%d) = %q, want %q", i, themeAt(i), theme)
		}
	}
}

func TestThemeIndexFallsBackToZeroForUnknownTheme(t *testing.T) {
	if themeIndex("bogus") != 0 {
		t.Fatal("unknown theme must map to index 0")
	}
}

func TestThemeAtFallsBackToSystemForOutOfRangeIndex(t *testing.T) {
	if themeAt(-1) != config.ThemeSystem {
		t.Fatal("negative index must fall back to system theme")
	}
	if themeAt(len(themeOrder)) != config.ThemeSystem {
		t.Fatal("index past the end must fall back to system theme")
	}
}

func TestRequestReadsCurrentWidgetValues(t *testing.T) {
	initial := Model{
		Language:      "en",
		Theme:         config.ThemeSystem,
		ShowToolbar:   false,
		ShowStatusBar: false,
		LogMaxCount:   500,
		AutoFetch:     false,
		FetchInterval: 300,
		WorkTreeDepth: 0,
	}
	v := newTestView(t, []string{"en", "ru"}, initial)
	v.language.SetSelected(1)
	v.theme.SetSelected(themeIndex(config.ThemeLight))
	clickCheckBox(v.showToolbar)
	clickCheckBox(v.autoFetch)
	v.logMaxCount.SetValue(1234)
	v.fetchInterval.SetValue(456)
	v.workTreeDepth.SetValue(8)

	got := v.request()
	want := Model{
		Language:      "ru",
		Theme:         config.ThemeLight,
		ShowToolbar:   true,
		ShowStatusBar: false,
		LogMaxCount:   1234,
		AutoFetch:     true,
		FetchInterval: 456,
		WorkTreeDepth: 8,
	}
	if got != want {
		t.Fatalf("request = %+v, want %+v", got, want)
	}
}

func TestRequestFallsBackToEmptyLanguageWhenNoLanguagesConfigured(t *testing.T) {
	v := newTestView(t, nil, Model{})
	got := v.request()
	if got.Language != "en" {
		t.Fatalf("language = %q, want en (normalized fallback)", got.Language)
	}
}

func TestConfirmInvokesOnOKWithCurrentModel(t *testing.T) {
	v := newTestView(t, []string{"en", "ru"}, Model{Language: "ru", LogMaxCount: 500, FetchInterval: 300})
	var got Model
	called := 0
	v.OnOK = func(m Model) { got = m; called++ }

	v.confirm()

	if called != 1 {
		t.Fatalf("OnOK called = %d, want 1", called)
	}
	if got.Language != "ru" {
		t.Fatalf("model = %+v", got)
	}
}

func TestConfirmToleratesNilOnOK(t *testing.T) {
	v := newTestView(t, []string{"en"}, Model{})
	v.confirm()
}

func TestCancelInvokesOnCancel(t *testing.T) {
	v := newTestView(t, []string{"en"}, Model{})
	called := 0
	v.OnCancel = func() { called++ }
	v.cancel()
	if called != 1 {
		t.Fatal("cancel must invoke OnCancel")
	}
}

func TestCancelToleratesNilOnCancel(t *testing.T) {
	v := newTestView(t, []string{"en"}, Model{})
	v.cancel()
}

func TestDialogWiresDefaultAndCancelActionsToConfirmAndCancel(t *testing.T) {
	v := newTestView(t, []string{"en"}, Model{})

	okCalled, cancelCalled := 0, 0
	v.OnOK = func(Model) { okCalled++ }
	v.OnCancel = func() { cancelCalled++ }

	if !v.dlg.HandleInputBinding(widget.KeyEnter, 0) {
		t.Fatal("dialog must handle Enter")
	}
	if okCalled != 1 {
		t.Fatalf("Enter must confirm, called = %d", okCalled)
	}

	v.dlg.OnCancel()
	if cancelCalled != 1 {
		t.Fatalf("Escape must cancel, called = %d", cancelCalled)
	}
}

func TestDialogTitleUsesLocalizedText(t *testing.T) {
	v := newTestView(t, []string{"en"}, Model{})
	if v.Dialog().Title != i18n.T("Dialog.Settings.Title") {
		t.Fatalf("title = %q", v.Dialog().Title)
	}
}

func TestOKAndCancelButtonsInvokeConfirmAndCancel(t *testing.T) {
	v := newTestView(t, []string{"en"}, Model{})
	okCalled, cancelCalled := 0, 0
	v.OnOK = func(Model) { okCalled++ }
	v.OnCancel = func() { cancelCalled++ }

	v.okBtn.OnClick()
	if okCalled != 1 {
		t.Fatal("ok button must invoke confirm")
	}
	v.cancelBtn.OnClick()
	if cancelCalled != 1 {
		t.Fatal("cancel button must invoke cancel")
	}
}
