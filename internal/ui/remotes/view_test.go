package remotes

import (
	"errors"
	"testing"

	"github.com/oops1/headless-gui/v3/engine"
	"github.com/oops1/headless-gui/v3/widget"
	"github.com/oops1/headless-gui/v3/widget/datagrid"

	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/style"
)

func newTestView(t *testing.T, entries []Entry) *View {
	t.Helper()
	widget.ClearStrings()
	t.Cleanup(widget.ClearStrings)
	if _, err := i18n.Install(""); err != nil {
		t.Fatal(err)
	}
	i18n.Apply("en")
	eng := engine.New(800, 600, 30)
	v, err := NewView(eng, entries)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func clickButton(btn *widget.Button) {
	btn.OnMouseButton(widget.MouseEvent{Button: widget.MouseLeft, Pressed: true})
	btn.OnMouseButton(widget.MouseEvent{Button: widget.MouseLeft, Pressed: false})
}

func fullNamedWidgets() map[string]widget.Widget {
	return map[string]widget.Widget{
		"table":  widget.NewDataGridWidget(),
		"name":   widget.NewTextInput(""),
		"url":    widget.NewTextInput(""),
		"add":    widget.NewButton(""),
		"edit":   widget.NewButton(""),
		"remove": widget.NewButton(""),
		"close":  widget.NewButton(""),
		"error":  widget.NewWin10Label(""),
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
	if _, err := NewView(eng, nil); !errors.Is(err, wantErr) {
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
	if _, err := NewView(eng, nil); !errors.Is(err, ErrWidgetMissing) {
		t.Fatalf("err = %v, want %v", err, ErrWidgetMissing)
	}
}

func TestBindReturnsErrorForEachMissingOrMistypedWidget(t *testing.T) {
	keys := []string{"table", "name", "url", "add", "edit", "remove", "close", "error"}
	for _, key := range keys {
		named := fullNamedWidgets()
		delete(named, key)
		v := &View{}
		if err := v.bind(named); err == nil {
			t.Fatalf("missing %q: expected error", key)
		}
	}
	for _, key := range keys {
		if key == "error" {
			continue
		}
		named := fullNamedWidgets()
		named[key] = widget.NewWin10Label("wrong-type")
		v := &View{}
		if err := v.bind(named); err == nil {
			t.Fatalf("mistyped %q: expected error", key)
		}
	}
	named := fullNamedWidgets()
	named["error"] = widget.NewButton("wrong-type")
	v := &View{}
	if err := v.bind(named); err == nil {
		t.Fatal("mistyped \"error\": expected error")
	}
}

func TestBindSucceedsWithAllWidgetsPresent(t *testing.T) {
	v := &View{}
	if err := v.bind(fullNamedWidgets()); err != nil {
		t.Fatal(err)
	}
}

func TestDialogReturnsUnderlyingDialog(t *testing.T) {
	v := newTestView(t, nil)
	if v.Dialog() != v.dlg {
		t.Fatal("Dialog() must return the underlying dialog")
	}
}

func TestNewViewBuildsThreeColumnsAndPopulatesTable(t *testing.T) {
	entries := []Entry{
		{Name: "origin", FetchURL: "https://example.com/a.git", PushURL: "https://example.com/a.git"},
		{Name: "upstream", FetchURL: "https://example.com/b.git", PushURL: "ssh://example.com/b.git"},
	}
	v := newTestView(t, entries)

	if got := len(v.table.Grid.Columns()); got != 3 {
		t.Fatalf("columns = %d, want 3", got)
	}
	if got := v.table.Grid.ItemsSource().Count(); got != 2 {
		t.Fatalf("rows = %d, want 2", got)
	}
	if got := v.table.Grid.ItemsSource().Get(1).(Entry); got.Name != "upstream" {
		t.Fatalf("row 1 = %+v", got)
	}
}

func TestSetEntriesReplacesRowsAndClearsSelection(t *testing.T) {
	v := newTestView(t, []Entry{{Name: "origin", FetchURL: "u"}})
	v.onSelectionChanged(datagrid.SelectionChangedEvent{SelectedIndex: 0, SelectedItem: v.entries[0]})
	if !v.hasSelection {
		t.Fatal("selection must be set up before SetEntries")
	}

	v.SetEntries([]Entry{{Name: "other", FetchURL: "v"}})

	if v.table.Grid.ItemsSource().Count() != 1 {
		t.Fatalf("rows = %d, want 1", v.table.Grid.ItemsSource().Count())
	}
	if v.hasSelection {
		t.Fatal("selection must be cleared by SetEntries")
	}
	if v.editBtn.IsEnabled() || v.removeBtn.IsEnabled() {
		t.Fatal("edit and remove must be disabled without selection")
	}
}

func TestSelectionChangedPopulatesFieldsAndEnablesButtons(t *testing.T) {
	entries := []Entry{{Name: "origin", FetchURL: "https://example.com/a.git", PushURL: "https://example.com/a.git"}}
	v := newTestView(t, entries)

	v.onSelectionChanged(datagrid.SelectionChangedEvent{SelectedIndex: 0, SelectedItem: entries[0]})

	if v.nameInput.GetText() != "origin" {
		t.Fatalf("name = %q", v.nameInput.GetText())
	}
	if v.urlInput.GetText() != "https://example.com/a.git" {
		t.Fatalf("url = %q", v.urlInput.GetText())
	}
	if !v.editBtn.IsEnabled() || !v.removeBtn.IsEnabled() {
		t.Fatal("edit and remove must be enabled once a row is selected")
	}
}

func TestSelectionChangedIgnoresNonEntryItem(t *testing.T) {
	v := newTestView(t, []Entry{{Name: "origin", FetchURL: "u"}})
	v.onSelectionChanged(datagrid.SelectionChangedEvent{SelectedIndex: -1, SelectedItem: nil})

	if v.hasSelection {
		t.Fatal("hasSelection must be false for a non-Entry item")
	}
	if v.editBtn.IsEnabled() || v.removeBtn.IsEnabled() {
		t.Fatal("edit and remove must stay disabled")
	}
}

func TestAddClickedValidatesEmptyName(t *testing.T) {
	v := newTestView(t, nil)
	called := 0
	v.OnAdd = func(string, string) { called++ }

	clickButton(v.addBtn)

	if called != 0 {
		t.Fatal("OnAdd must not fire")
	}
	if v.errorLabel.Text() != i18n.T("Dialog.Remotes.Error.Name") {
		t.Fatalf("error = %q", v.errorLabel.Text())
	}
}

func TestAddClickedValidatesEmptyURL(t *testing.T) {
	v := newTestView(t, nil)
	v.nameInput.SetText("origin")
	called := 0
	v.OnAdd = func(string, string) { called++ }

	clickButton(v.addBtn)

	if called != 0 {
		t.Fatal("OnAdd must not fire")
	}
	if v.errorLabel.Text() != i18n.T("Dialog.Remotes.Error.URL") {
		t.Fatalf("error = %q", v.errorLabel.Text())
	}
}

func TestAddClickedValidatesDuplicateName(t *testing.T) {
	v := newTestView(t, []Entry{{Name: "origin", FetchURL: "https://example.com/a.git"}})
	v.nameInput.SetText("origin")
	v.urlInput.SetText("https://example.com/b.git")
	called := 0
	v.OnAdd = func(string, string) { called++ }

	clickButton(v.addBtn)

	if called != 0 {
		t.Fatal("OnAdd must not fire for a duplicate name")
	}
	if v.errorLabel.Text() != i18n.T("Dialog.Remotes.Error.Duplicate") {
		t.Fatalf("error = %q", v.errorLabel.Text())
	}
}

func TestAddClickedCallsOnAddAndClearsError(t *testing.T) {
	v := newTestView(t, nil)
	v.SetError("stale error")
	v.nameInput.SetText("  origin  ")
	v.urlInput.SetText("  https://example.com/a.git  ")
	var gotName, gotURL string
	v.OnAdd = func(name, url string) { gotName, gotURL = name, url }

	clickButton(v.addBtn)

	if gotName != "origin" || gotURL != "https://example.com/a.git" {
		t.Fatalf("OnAdd got (%q, %q)", gotName, gotURL)
	}
	if v.errorLabel.Text() != "" {
		t.Fatalf("error = %q, want cleared", v.errorLabel.Text())
	}
}

func TestEditClickedDoesNothingWithoutSelection(t *testing.T) {
	v := newTestView(t, nil)
	called := 0
	v.OnEdit = func(string, string) { called++ }

	v.onEditClicked()

	if called != 0 {
		t.Fatal("OnEdit must not fire without a selection")
	}
}

func TestEditClickedAllowsKeepingTheSameName(t *testing.T) {
	entries := []Entry{{Name: "origin", FetchURL: "https://example.com/a.git"}}
	v := newTestView(t, entries)
	v.onSelectionChanged(datagrid.SelectionChangedEvent{SelectedIndex: 0, SelectedItem: entries[0]})
	v.urlInput.SetText("https://example.com/new.git")

	var gotName, gotURL string
	v.OnEdit = func(name, url string) { gotName, gotURL = name, url }
	clickButton(v.editBtn)

	if gotName != "origin" || gotURL != "https://example.com/new.git" {
		t.Fatalf("OnEdit got (%q, %q)", gotName, gotURL)
	}
}

func TestEditClickedValidatesEmptyNameAndURL(t *testing.T) {
	entries := []Entry{{Name: "origin", FetchURL: "https://example.com/a.git"}}
	v := newTestView(t, entries)
	v.onSelectionChanged(datagrid.SelectionChangedEvent{SelectedIndex: 0, SelectedItem: entries[0]})
	v.nameInput.SetText("")

	called := 0
	v.OnEdit = func(string, string) { called++ }
	clickButton(v.editBtn)

	if called != 0 {
		t.Fatal("OnEdit must not fire with an empty name")
	}
	if v.errorLabel.Text() != i18n.T("Dialog.Remotes.Error.Name") {
		t.Fatalf("error = %q", v.errorLabel.Text())
	}

	v.nameInput.SetText("origin")
	v.urlInput.SetText("")
	clickButton(v.editBtn)

	if called != 0 {
		t.Fatal("OnEdit must not fire with an empty url")
	}
	if v.errorLabel.Text() != i18n.T("Dialog.Remotes.Error.URL") {
		t.Fatalf("error = %q", v.errorLabel.Text())
	}
}

func TestEditClickedValidatesDuplicateAgainstAnotherEntry(t *testing.T) {
	entries := []Entry{
		{Name: "origin", FetchURL: "https://example.com/a.git"},
		{Name: "upstream", FetchURL: "https://example.com/b.git"},
	}
	v := newTestView(t, entries)
	v.onSelectionChanged(datagrid.SelectionChangedEvent{SelectedIndex: 0, SelectedItem: entries[0]})
	v.nameInput.SetText("upstream")

	called := 0
	v.OnEdit = func(string, string) { called++ }
	clickButton(v.editBtn)

	if called != 0 {
		t.Fatal("OnEdit must not fire for a name that collides with another entry")
	}
	if v.errorLabel.Text() != i18n.T("Dialog.Remotes.Error.Duplicate") {
		t.Fatalf("error = %q", v.errorLabel.Text())
	}
}

func TestRemoveClickedDoesNothingWithoutSelection(t *testing.T) {
	v := newTestView(t, nil)
	called := 0
	v.OnRemove = func(string) { called++ }

	v.onRemoveClicked()

	if called != 0 {
		t.Fatal("OnRemove must not fire without a selection")
	}
}

func TestRemoveClickedCallsOnRemoveWithSelectedName(t *testing.T) {
	entries := []Entry{{Name: "origin", FetchURL: "https://example.com/a.git"}}
	v := newTestView(t, entries)
	v.onSelectionChanged(datagrid.SelectionChangedEvent{SelectedIndex: 0, SelectedItem: entries[0]})

	var got string
	v.OnRemove = func(name string) { got = name }
	clickButton(v.removeBtn)

	if got != "origin" {
		t.Fatalf("OnRemove got %q", got)
	}
}

func TestRemoveClickedToleratesNilCallback(t *testing.T) {
	entries := []Entry{{Name: "origin", FetchURL: "https://example.com/a.git"}}
	v := newTestView(t, entries)
	v.onSelectionChanged(datagrid.SelectionChangedEvent{SelectedIndex: 0, SelectedItem: entries[0]})
	clickButton(v.removeBtn)
}

func TestCloseClickedInvokesCallback(t *testing.T) {
	v := newTestView(t, nil)
	called := 0
	v.OnClose = func() { called++ }
	clickButton(v.closeBtn)
	if called != 1 {
		t.Fatalf("OnClose called %d times, want 1", called)
	}
}

func TestCloseClickToleratesNilCallback(t *testing.T) {
	v := newTestView(t, nil)
	clickButton(v.closeBtn)
}

func TestDialogCancelActionInvokesClose(t *testing.T) {
	v := newTestView(t, nil)
	called := 0
	v.OnClose = func() { called++ }
	v.dlg.OnCancel()
	if called != 1 {
		t.Fatalf("Escape must close, called = %d", called)
	}
}

func TestSetErrorUpdatesLabel(t *testing.T) {
	v := newTestView(t, nil)
	v.SetError("custom error")
	if v.errorLabel.Text() != "custom error" {
		t.Fatalf("error = %q", v.errorLabel.Text())
	}
	if got := v.Error(); got != "custom error" {
		t.Fatalf("Error() = %q, want %q", got, "custom error")
	}
}

func TestTheDialogWearsTheColoursOfTheTheme(t *testing.T) {
	theme := widget.Win11DarkTheme()
	p := style.Of(theme)
	v := newTestView(t, nil)

	v.Restyle(theme)

	if v.nameInput.Background != p.Field || v.nameInput.BorderColor != p.Border || v.nameInput.PaddingX != style.FieldPaddingX {
		t.Fatalf("field = %v on %v, want the shared field style", v.nameInput.BorderColor, v.nameInput.Background)
	}
	if v.urlInput.Background != p.Field || v.urlInput.BorderColor != p.Border || v.urlInput.PaddingX != style.FieldPaddingX {
		t.Fatalf("field = %v on %v, want the shared field style", v.urlInput.BorderColor, v.urlInput.Background)
	}
	if v.closeBtn.Background != p.Field || v.closeBtn.BorderColor != p.Border {
		t.Fatalf("quiet button = %v, want the field fill", v.closeBtn.Background)
	}
}
