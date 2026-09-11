package clone

import (
	"errors"
	"testing"

	"github.com/oops1/headless-gui/v3/engine"
	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/style"
)

func newTestView(t *testing.T, req Request) *View {
	t.Helper()
	widget.ClearStrings()
	t.Cleanup(widget.ClearStrings)
	if _, err := i18n.Install(""); err != nil {
		t.Fatal(err)
	}
	i18n.Apply("en")
	eng := engine.New(800, 600, 30)
	v, err := NewView(eng, req)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func clickButton(btn *widget.Button) {
	btn.OnMouseButton(widget.MouseEvent{Button: widget.MouseLeft, Pressed: true})
	btn.OnMouseButton(widget.MouseEvent{Button: widget.MouseLeft, Pressed: false})
}

func clickCheckBox(cb *widget.CheckBox) {
	cb.OnMouseButton(widget.MouseEvent{Button: widget.MouseLeft, Pressed: true})
	cb.OnMouseButton(widget.MouseEvent{Button: widget.MouseLeft, Pressed: false})
}

func changeText(in *widget.TextInput, text string) {
	in.SetText(text)
	if in.OnChange != nil {
		in.OnChange(text)
	}
}

func fullNamedWidgets() map[string]widget.Widget {
	return map[string]widget.Widget{
		"url":       widget.NewTextInput(""),
		"directory": widget.NewTextInput(""),
		"browse":    widget.NewButton(""),
		"check":     widget.NewButton(""),
		"status":    widget.NewWin10Label(""),
		"branch":    widget.NewDropdown(),
		"shallow":   widget.NewCheckBox(""),
		"depth":     widget.NewNumericUpDown(),
		"ok":        widget.NewButton(""),
		"cancel":    widget.NewButton(""),
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
	if _, err := NewView(eng, Request{}); !errors.Is(err, wantErr) {
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
	if _, err := NewView(eng, Request{}); !errors.Is(err, ErrWidgetMissing) {
		t.Fatalf("err = %v, want %v", err, ErrWidgetMissing)
	}
}

func TestBindReturnsErrorForEachMissingOrMistypedWidget(t *testing.T) {
	keys := []string{"url", "directory", "browse", "check", "status", "branch", "shallow", "depth", "ok", "cancel"}
	for _, key := range keys {
		named := fullNamedWidgets()
		delete(named, key)
		v := &View{}
		if err := v.bind(named); err == nil {
			t.Fatalf("missing %q: expected error", key)
		}
	}
	for _, key := range keys {
		if key == "status" {
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
	named["status"] = widget.NewButton("wrong-type")
	v := &View{}
	if err := v.bind(named); err == nil {
		t.Fatal("mistyped \"status\": expected error")
	}
}

func TestBindSucceedsWithAllWidgetsPresent(t *testing.T) {
	v := &View{}
	if err := v.bind(fullNamedWidgets()); err != nil {
		t.Fatal(err)
	}
}

func TestDialogReturnsUnderlyingDialog(t *testing.T) {
	v := newTestView(t, Request{})
	if v.Dialog() != v.dlg {
		t.Fatal("Dialog() must return the underlying dialog")
	}
}

func TestNewViewAppliesInitialRequestAndDisablesDepth(t *testing.T) {
	v := newTestView(t, Request{URL: "https://example.com/repo.git", Directory: "C:\\work\\repo"})

	if v.urlInput.GetText() != "https://example.com/repo.git" {
		t.Fatalf("url = %q", v.urlInput.GetText())
	}
	if v.dirInput.GetText() != "C:\\work\\repo" {
		t.Fatalf("directory = %q", v.dirInput.GetText())
	}
	if v.depthInput.IsEnabled() {
		t.Fatal("depth must start disabled")
	}
	if !v.okBtn.IsEnabled() {
		t.Fatal("ok must be enabled when both url and directory are set")
	}
}

func TestOKDisabledWhenURLOrDirectoryEmpty(t *testing.T) {
	tests := []struct {
		name string
		url  string
		dir  string
	}{
		{"both empty", "", ""},
		{"url empty", "", "dir"},
		{"directory empty", "url", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := newTestView(t, Request{})
			changeText(v.urlInput, tt.url)
			changeText(v.dirInput, tt.dir)
			if v.okBtn.IsEnabled() {
				t.Fatal("ok must be disabled")
			}
		})
	}
}

func TestOKEnabledOnceBothFieldsFilled(t *testing.T) {
	v := newTestView(t, Request{})
	changeText(v.urlInput, "https://example.com/repo.git")
	if v.okBtn.IsEnabled() {
		t.Fatal("ok must stay disabled until directory is set too")
	}
	changeText(v.dirInput, "dir")
	if !v.okBtn.IsEnabled() {
		t.Fatal("ok must be enabled once both fields are filled")
	}
}

func TestShallowTogglesDepthEnabled(t *testing.T) {
	v := newTestView(t, Request{})
	if v.depthInput.IsEnabled() {
		t.Fatal("depth must start disabled")
	}

	clickCheckBox(v.shallowCheck)
	if !v.depthInput.IsEnabled() {
		t.Fatal("depth must be enabled once shallow is checked")
	}

	clickCheckBox(v.shallowCheck)
	if v.depthInput.IsEnabled() {
		t.Fatal("depth must be disabled once shallow is unchecked")
	}
}

func TestDepthNeverGoesNegative(t *testing.T) {
	v := newTestView(t, Request{})
	v.depthInput.SetValue(-5)
	if v.depthInput.Value() < 0 {
		t.Fatalf("depth = %v, must not be negative", v.depthInput.Value())
	}
}

func TestCheckClickedWithEmptyURLSetsStatusAndDoesNotCallOnCheck(t *testing.T) {
	v := newTestView(t, Request{Directory: "dir"})
	called := 0
	v.OnCheck = func(string) { called++ }

	clickButton(v.checkBtn)

	if called != 0 {
		t.Fatalf("OnCheck called %d times, want 0", called)
	}
	if v.statusLabel.Text() != i18n.T("Dialog.Clone.Error.URL") {
		t.Fatalf("status = %q", v.statusLabel.Text())
	}
	if v.busy {
		t.Fatal("must not become busy on validation failure")
	}
}

func TestCheckClickedCallsOnCheckAndBecomesBusy(t *testing.T) {
	v := newTestView(t, Request{URL: "https://example.com/repo.git"})
	var gotURL string
	v.OnCheck = func(url string) { gotURL = url }

	clickButton(v.checkBtn)

	if gotURL != "https://example.com/repo.git" {
		t.Fatalf("OnCheck url = %q", gotURL)
	}
	if !v.busy {
		t.Fatal("must be busy while checking")
	}
	if v.checkBtn.IsEnabled() {
		t.Fatal("check must be disabled while busy")
	}
	if v.okBtn.IsEnabled() {
		t.Fatal("ok must be disabled while busy")
	}
}

func TestSetBusyRestoresButtonsAccordingToValidity(t *testing.T) {
	v := newTestView(t, Request{URL: "https://example.com/repo.git", Directory: "dir"})
	clickCheckBox(v.shallowCheck)

	v.SetBusy(true)
	if v.checkBtn.IsEnabled() || v.branchDrop.IsEnabled() || v.shallowCheck.IsEnabled() || v.depthInput.IsEnabled() {
		t.Fatal("busy must disable check, branch, shallow and depth")
	}

	v.SetBusy(false)
	if !v.checkBtn.IsEnabled() {
		t.Fatal("check must be re-enabled")
	}
	if !v.okBtn.IsEnabled() {
		t.Fatal("ok must be re-enabled once fields are valid again")
	}
	if !v.depthInput.IsEnabled() {
		t.Fatal("depth must be re-enabled because shallow is checked")
	}
}

func TestSetBranchesSelectsMatchingHead(t *testing.T) {
	v := newTestView(t, Request{})
	v.SetBranches([]string{"main", "develop", "feature"}, "develop")

	if v.branchDrop.Selected() != 1 {
		t.Fatalf("selected = %d, want 1", v.branchDrop.Selected())
	}
	if v.branchDrop.SelectedText() != "develop" {
		t.Fatalf("selected text = %q", v.branchDrop.SelectedText())
	}
}

func TestSetBranchesToleratesMissingHead(t *testing.T) {
	v := newTestView(t, Request{})
	v.SetBranches([]string{"main", "develop"}, "unknown")

	if got := v.branchDrop.Items(); len(got) != 2 {
		t.Fatalf("items = %v", got)
	}
}

func TestSetStatusUpdatesLabel(t *testing.T) {
	v := newTestView(t, Request{})
	v.SetStatus("custom status")
	if v.statusLabel.Text() != "custom status" {
		t.Fatalf("status = %q", v.statusLabel.Text())
	}
}

func TestConfirmValidatesEmptyURLBeforeDirectory(t *testing.T) {
	v := newTestView(t, Request{})
	called := 0
	v.OnOK = func(Result) { called++ }

	v.confirm()

	if called != 0 {
		t.Fatal("OnOK must not be called")
	}
	if v.statusLabel.Text() != i18n.T("Dialog.Clone.Error.URL") {
		t.Fatalf("status = %q", v.statusLabel.Text())
	}
}

func TestConfirmValidatesEmptyDirectory(t *testing.T) {
	v := newTestView(t, Request{URL: "https://example.com/repo.git"})
	called := 0
	v.OnOK = func(Result) { called++ }

	v.confirm()

	if called != 0 {
		t.Fatal("OnOK must not be called")
	}
	if v.statusLabel.Text() != i18n.T("Dialog.Clone.Error.Directory") {
		t.Fatalf("status = %q", v.statusLabel.Text())
	}
}

func TestConfirmDoesNothingWhileBusy(t *testing.T) {
	v := newTestView(t, Request{URL: "https://example.com/repo.git", Directory: "dir"})
	v.SetBusy(true)
	called := 0
	v.OnOK = func(Result) { called++ }

	v.confirm()

	if called != 0 {
		t.Fatal("OnOK must not fire while busy")
	}
}

func TestConfirmCallsOnOKWithTrimmedValuesAndZeroDepthWhenNotShallow(t *testing.T) {
	v := newTestView(t, Request{})
	changeText(v.urlInput, "  https://example.com/repo.git  ")
	changeText(v.dirInput, "  dir  ")
	v.SetBranches([]string{"main"}, "main")
	v.depthInput.SetValue(7)

	var got Result
	v.OnOK = func(r Result) { got = r }
	v.confirm()

	if got.URL != "https://example.com/repo.git" {
		t.Fatalf("url = %q", got.URL)
	}
	if got.Directory != "dir" {
		t.Fatalf("directory = %q", got.Directory)
	}
	if got.Branch != "main" {
		t.Fatalf("branch = %q", got.Branch)
	}
	if got.Depth != 0 {
		t.Fatalf("depth = %d, want 0 because shallow is unchecked", got.Depth)
	}
}

func TestConfirmCallsOnOKWithDepthWhenShallow(t *testing.T) {
	v := newTestView(t, Request{URL: "https://example.com/repo.git", Directory: "dir"})
	clickCheckBox(v.shallowCheck)
	v.depthInput.SetValue(5)

	var got Result
	v.OnOK = func(r Result) { got = r }
	v.confirm()

	if got.Depth != 5 {
		t.Fatalf("depth = %d, want 5", got.Depth)
	}
}

func TestCancelInvokesOnCancel(t *testing.T) {
	v := newTestView(t, Request{})
	called := 0
	v.OnCancel = func() { called++ }
	clickButton(v.cancelBtn)
	if called != 1 {
		t.Fatalf("OnCancel called %d times, want 1", called)
	}
}

func TestCancelToleratesNilOnCancel(t *testing.T) {
	v := newTestView(t, Request{})
	clickButton(v.cancelBtn)
}

func TestDialogWiresDefaultAndCancelActions(t *testing.T) {
	v := newTestView(t, Request{URL: "https://example.com/repo.git", Directory: "dir"})
	okCalled, cancelCalled := 0, 0
	v.OnOK = func(Result) { okCalled++ }
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

func TestBrowseShowsPickFolderDialogWithoutPanicking(t *testing.T) {
	v := newTestView(t, Request{})
	v.browse()
}

func TestOnFolderPickedSetsDirectoryAndRefreshes(t *testing.T) {
	v := newTestView(t, Request{URL: "https://example.com/repo.git"})
	v.onFolderPicked("/picked/dir", true)

	if v.dirInput.GetText() != "/picked/dir" {
		t.Fatalf("directory = %q", v.dirInput.GetText())
	}
	if !v.okBtn.IsEnabled() {
		t.Fatal("ok must become enabled once directory is picked")
	}
}

func TestOnFolderPickedIgnoresCancel(t *testing.T) {
	v := newTestView(t, Request{Directory: "unchanged"})
	v.onFolderPicked("ignored", false)
	if v.dirInput.GetText() != "unchanged" {
		t.Fatalf("directory = %q, must stay unchanged", v.dirInput.GetText())
	}
}

func TestTheDialogWearsTheColoursOfTheTheme(t *testing.T) {
	theme := widget.Win11DarkTheme()
	p := style.Of(theme)
	v := newTestView(t, Request{})

	v.Restyle(theme)

	if v.urlInput.Background != p.Field || v.urlInput.BorderColor != p.Border || v.urlInput.PaddingX != style.FieldPaddingX {
		t.Fatalf("field = %v on %v, want the shared field style", v.urlInput.BorderColor, v.urlInput.Background)
	}
	if v.dirInput.Background != p.Field || v.dirInput.BorderColor != p.Border || v.dirInput.PaddingX != style.FieldPaddingX {
		t.Fatalf("field = %v on %v, want the shared field style", v.dirInput.BorderColor, v.dirInput.Background)
	}
	if v.branchDrop.Background != p.Field || v.branchDrop.PaddingX != style.FieldPaddingX {
		t.Fatalf("list = %v, want the same style as the fields", v.branchDrop.Background)
	}
	if v.okBtn.Background != p.Accent || v.okBtn.TextColor != p.OnAccent {
		t.Fatalf("main button = %v on %v, want the accent", v.okBtn.TextColor, v.okBtn.Background)
	}
	if v.cancelBtn.Background != p.Field || v.cancelBtn.BorderColor != p.Border {
		t.Fatalf("quiet button = %v, want the field fill", v.cancelBtn.Background)
	}
}
