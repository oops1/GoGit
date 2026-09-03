package addrepo

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/oops1/headless-gui/v3/engine"
	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/i18n"
)

func newTestView(t *testing.T, initial Request) *View {
	t.Helper()
	widget.ClearStrings()
	t.Cleanup(widget.ClearStrings)
	if _, err := i18n.Install(""); err != nil {
		t.Fatal(err)
	}
	i18n.Apply("en")
	eng := engine.New(800, 600, 30)
	v, err := NewView(eng, initial)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func clickRadio(rb *widget.RadioButton) {
	rb.OnMouseButton(widget.MouseEvent{Button: widget.MouseLeft, Pressed: true})
	rb.OnMouseButton(widget.MouseEvent{Button: widget.MouseLeft, Pressed: false})
}

func clickCheckBox(cb *widget.CheckBox) {
	cb.OnMouseButton(widget.MouseEvent{Button: widget.MouseLeft, Pressed: true})
	cb.OnMouseButton(widget.MouseEvent{Button: widget.MouseLeft, Pressed: false})
}

func changeText(in *widget.TextInput, text string) {
	in.SetText(text)
	in.OnChange(text)
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

func TestNewViewOpenModeHidesBareAndNameFields(t *testing.T) {
	v := newTestView(t, Request{Mode: ModeOpen})
	if v.bareCheck.IsVisible() || v.nameInput.IsVisible() {
		t.Fatal("bare and name must be hidden in open mode")
	}
}

func TestNewViewCreateModeShowsBareAndNameFields(t *testing.T) {
	v := newTestView(t, Request{Mode: ModeCreate})
	if !v.bareCheck.IsVisible() || !v.nameInput.IsVisible() {
		t.Fatal("bare and name must be visible in create mode")
	}
}

func TestNewViewAppliesInitialRequestToWidgets(t *testing.T) {
	dir := t.TempDir()
	v := newTestView(t, Request{Path: dir, Name: "custom", Bare: true, Mode: ModeCreate})
	if v.pathInput.GetText() != dir {
		t.Fatalf("path = %q", v.pathInput.GetText())
	}
	if v.nameInput.GetText() != "custom" {
		t.Fatalf("name = %q", v.nameInput.GetText())
	}
	if !v.bareCheck.IsChecked() {
		t.Fatal("bare must be checked")
	}
	if !v.modeCreate.IsSelected() || v.modeOpen.IsSelected() {
		t.Fatal("create mode must be selected")
	}
}

func TestNewViewAutoFillsNameFromDirectoryWhenNameIsBlank(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "myrepo")
	v := newTestView(t, Request{Path: dir, Mode: ModeCreate})
	if v.nameInput.GetText() != "myrepo" {
		t.Fatalf("name = %q, want myrepo", v.nameInput.GetText())
	}
}

func TestBindReturnsErrorForEachMissingOrMistypedWidget(t *testing.T) {
	full := func() map[string]widget.Widget {
		return map[string]widget.Widget{
			"path":       widget.NewTextInput(""),
			"browse":     widget.NewButton(""),
			"hint":       widget.NewWin10Label(""),
			"modeOpen":   widget.NewRadioButton("", "g"),
			"modeCreate": widget.NewRadioButton("", "g"),
			"bare":       widget.NewCheckBox(""),
			"name":       widget.NewTextInput(""),
			"ok":         widget.NewButton(""),
			"cancel":     widget.NewButton(""),
		}
	}
	for _, key := range []string{"path", "browse", "hint", "modeOpen", "modeCreate", "bare", "name", "ok", "cancel"} {
		named := full()
		delete(named, key)
		v := &View{}
		if err := v.bind(named); err == nil {
			t.Fatalf("missing %q: expected error", key)
		}
	}
	for _, key := range []string{"path", "browse", "hint", "modeOpen", "modeCreate", "bare", "name", "ok", "cancel"} {
		named := full()
		named[key] = widget.NewWin10Label("wrong-type")
		v := &View{}
		if key == "hint" {
			continue
		}
		if err := v.bind(named); err == nil {
			t.Fatalf("mistyped %q: expected error", key)
		}
	}
}

func TestBindSucceedsWithAllWidgetsPresent(t *testing.T) {
	named := map[string]widget.Widget{
		"path":       widget.NewTextInput(""),
		"browse":     widget.NewButton(""),
		"hint":       widget.NewWin10Label(""),
		"modeOpen":   widget.NewRadioButton("", "g"),
		"modeCreate": widget.NewRadioButton("", "g"),
		"bare":       widget.NewCheckBox(""),
		"name":       widget.NewTextInput(""),
		"ok":         widget.NewButton(""),
		"cancel":     widget.NewButton(""),
	}
	v := &View{}
	if err := v.bind(named); err != nil {
		t.Fatal(err)
	}
}

func TestPathChangeRecomputesHintAndOKState(t *testing.T) {
	v := newTestView(t, Request{Mode: ModeOpen})
	if v.okBtn.IsEnabled() {
		t.Fatal("ok must start disabled with an empty path")
	}

	missing := filepath.Join(t.TempDir(), "nope")
	changeText(v.pathInput, missing)
	if v.okBtn.IsEnabled() {
		t.Fatal("ok must stay disabled for a missing path")
	}
	if v.current.Key != hintPathNotFound {
		t.Fatalf("hint key = %q", v.current.Key)
	}

	dir := t.TempDir()
	changeText(v.pathInput, dir)
	if v.current.Key != hintNotARepository {
		t.Fatalf("hint key = %q", v.current.Key)
	}

	target := filepath.Join(dir, "repo")
	initRepo(t, target, false)
	changeText(v.pathInput, target)
	if !v.okBtn.IsEnabled() || v.current.Key != hintOpenFound {
		t.Fatalf("hint = %+v, ok enabled = %v", v.current, v.okBtn.IsEnabled())
	}
	if got := v.hintLabel.Text(); got == "" {
		t.Fatal("hint label must be populated")
	}
}

func TestNameChangeRecomputesHintAndOKState(t *testing.T) {
	dir := t.TempDir()
	v := newTestView(t, Request{Path: dir, Mode: ModeCreate})
	if v.current.Key != hintWillCreate || !v.okBtn.IsEnabled() {
		t.Fatalf("hint = %+v", v.current)
	}

	changeText(v.nameInput, "")
	if v.current.Key != hintNameRequired || v.okBtn.IsEnabled() {
		t.Fatalf("hint = %+v, ok enabled = %v", v.current, v.okBtn.IsEnabled())
	}

	changeText(v.nameInput, "custom")
	if v.current.Key != hintWillCreate || !v.okBtn.IsEnabled() {
		t.Fatalf("hint = %+v", v.current)
	}
}

func TestModeChangeTogglesVisibilityAndAutoFillsName(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "myrepo")
	v := newTestView(t, Request{Path: dir, Mode: ModeOpen})
	if v.nameInput.IsVisible() {
		t.Fatal("name must start hidden in open mode")
	}

	clickRadio(v.modeCreate)

	if !v.nameInput.IsVisible() || !v.bareCheck.IsVisible() {
		t.Fatal("switching to create mode must reveal name and bare")
	}
	if v.nameInput.GetText() != "myrepo" {
		t.Fatalf("name = %q, want auto-filled myrepo", v.nameInput.GetText())
	}

	clickRadio(v.modeOpen)
	if v.nameInput.IsVisible() || v.bareCheck.IsVisible() {
		t.Fatal("switching back to open mode must hide name and bare")
	}
}

func TestAutoFillNameDoesNotOverwriteExistingName(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "myrepo")
	v := newTestView(t, Request{Path: dir, Name: "kept", Mode: ModeCreate})
	if v.nameInput.GetText() != "kept" {
		t.Fatalf("name = %q, want kept", v.nameInput.GetText())
	}
}

func TestAutoFillNameSkipsWhenPathHasNoUsableBase(t *testing.T) {
	v := newTestView(t, Request{Mode: ModeCreate})
	v.autoFillName()
	if v.nameInput.GetText() != "" {
		t.Fatalf("name = %q, want empty", v.nameInput.GetText())
	}
}

func TestBareCheckboxToggleUpdatesRequest(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "myrepo")
	v := newTestView(t, Request{Path: dir, Name: "myrepo", Mode: ModeCreate})
	if v.request().Bare {
		t.Fatal("bare must start false")
	}
	clickCheckBox(v.bareCheck)
	if !v.request().Bare {
		t.Fatal("bare must be true after toggling")
	}
}

func TestConfirmInvokesOnOKOnlyWhenHintIsOK(t *testing.T) {
	v := newTestView(t, Request{Mode: ModeOpen})
	called := 0
	v.OnOK = func(Request) { called++ }

	v.confirm()
	if called != 0 {
		t.Fatal("confirm must be a no-op while the hint is not OK")
	}

	dir := t.TempDir()
	target := filepath.Join(dir, "repo")
	initRepo(t, target, false)
	changeText(v.pathInput, target)

	v.confirm()
	if called != 1 {
		t.Fatalf("confirm must invoke OnOK once, called = %d", called)
	}
}

func TestConfirmToleratesNilOnOK(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "repo")
	initRepo(t, target, false)
	v := newTestView(t, Request{Path: target, Mode: ModeOpen})
	v.confirm()
}

func TestCancelInvokesOnCancel(t *testing.T) {
	v := newTestView(t, Request{})
	called := 0
	v.OnCancel = func() { called++ }
	v.cancel()
	if called != 1 {
		t.Fatal("cancel must invoke OnCancel")
	}
}

func TestCancelToleratesNilOnCancel(t *testing.T) {
	v := newTestView(t, Request{})
	v.cancel()
}

func TestDialogWiresDefaultAndCancelActionsToConfirmAndCancel(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "repo")
	initRepo(t, target, false)
	v := newTestView(t, Request{Path: target, Mode: ModeOpen})

	okCalled, cancelCalled := 0, 0
	v.OnOK = func(Request) { okCalled++ }
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

func TestOnFolderPickedSetsPathAndRecomputesHint(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "repo")
	initRepo(t, target, false)
	v := newTestView(t, Request{Mode: ModeOpen})

	v.onFolderPicked(target, true)

	if v.pathInput.GetText() != target {
		t.Fatalf("path = %q", v.pathInput.GetText())
	}
	if v.current.Key != hintOpenFound {
		t.Fatalf("hint key = %q", v.current.Key)
	}
}

func TestOnFolderPickedIgnoresCancel(t *testing.T) {
	v := newTestView(t, Request{Path: "unchanged"})
	v.onFolderPicked("ignored", false)
	if v.pathInput.GetText() != "unchanged" {
		t.Fatalf("path = %q, must stay unchanged", v.pathInput.GetText())
	}
}

func TestDialogTitleUsesLocalizedText(t *testing.T) {
	v := newTestView(t, Request{})
	if v.Dialog().Title != i18n.T("Dialog.AddRepo.Title") {
		t.Fatalf("title = %q", v.Dialog().Title)
	}
}
