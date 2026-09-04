package commit

import (
	"errors"
	"testing"

	"github.com/oops1/headless-gui/v3/engine"
	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/i18n"
)

func newTestView(t *testing.T, initial Model) *View {
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

func changeText(tb *widget.TextBox, text string) {
	tb.SetText(text)
	tb.OnChange(text)
}

func clickCheckBox(cb *widget.CheckBox) {
	cb.OnMouseButton(widget.MouseEvent{Button: widget.MouseLeft, Pressed: true})
	cb.OnMouseButton(widget.MouseEvent{Button: widget.MouseLeft, Pressed: false})
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
	if _, err := NewView(eng, Model{}); !errors.Is(err, wantErr) {
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
	if _, err := NewView(eng, Model{}); !errors.Is(err, ErrWidgetMissing) {
		t.Fatalf("err = %v, want %v", err, ErrWidgetMissing)
	}
}

func fullNamedWidgets() map[string]widget.Widget {
	return map[string]widget.Widget{
		"staged":  widget.NewWin10Label(""),
		"message": widget.NewTextBox(""),
		"amend":   widget.NewCheckBox(""),
		"ok":      widget.NewButton(""),
		"cancel":  widget.NewButton(""),
	}
}

func TestBindReturnsErrorForEachMissingOrMistypedWidget(t *testing.T) {
	for _, key := range []string{"staged", "message", "amend", "ok", "cancel"} {
		named := fullNamedWidgets()
		delete(named, key)
		v := &View{}
		if err := v.bind(named); err == nil {
			t.Fatalf("missing %q: expected error", key)
		}
	}
	for _, key := range []string{"message", "amend", "ok", "cancel"} {
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
	v := newTestView(t, Model{Message: "wip", Staged: 3})
	if v.message.GetText() != "wip" {
		t.Fatalf("message = %q, want wip", v.message.GetText())
	}
	if v.amendCheck.IsChecked() {
		t.Fatal("amend must start unchecked")
	}
	if v.okBtn.IsEnabled() != true {
		t.Fatal("ok must be enabled with a non-blank message")
	}
}

func TestNewViewWithBlankMessageDisablesOK(t *testing.T) {
	v := newTestView(t, Model{Staged: 0})
	if v.okBtn.IsEnabled() {
		t.Fatal("ok must start disabled with a blank message")
	}
}

func TestNewViewAmendTrueShowsLastMessage(t *testing.T) {
	v := newTestView(t, Model{Amend: true, LastMessage: "previous message"})
	if !v.amendCheck.IsChecked() {
		t.Fatal("amend must be checked")
	}
	if v.message.GetText() != "previous message" {
		t.Fatalf("message = %q, want the last commit message", v.message.GetText())
	}
}

func TestMessageChangeTogglesOKEnabled(t *testing.T) {
	v := newTestView(t, Model{})
	if v.okBtn.IsEnabled() {
		t.Fatal("ok must start disabled")
	}
	changeText(v.message, "add feature")
	if !v.okBtn.IsEnabled() {
		t.Fatal("ok must be enabled once a message is typed")
	}
	changeText(v.message, "   ")
	if v.okBtn.IsEnabled() {
		t.Fatal("ok must be disabled again for a blank message")
	}
}

func TestAmendToggleOnSwapsDraftForLastMessage(t *testing.T) {
	v := newTestView(t, Model{Message: "my draft", LastMessage: "last commit message"})
	clickCheckBox(v.amendCheck)
	if v.message.GetText() != "last commit message" {
		t.Fatalf("message = %q, want the last commit message", v.message.GetText())
	}

	clickCheckBox(v.amendCheck)
	if v.message.GetText() != "my draft" {
		t.Fatalf("message = %q, want the restored draft", v.message.GetText())
	}
}

func TestAmendToggleOnWithBlankDraftKeepsOKConsistentWithLastMessage(t *testing.T) {
	v := newTestView(t, Model{LastMessage: "last commit message"})
	if v.okBtn.IsEnabled() {
		t.Fatal("ok must start disabled")
	}
	clickCheckBox(v.amendCheck)
	if !v.okBtn.IsEnabled() {
		t.Fatal("ok must be enabled once amend fills in the last commit message")
	}
}

func TestRequestReflectsCurrentWidgetState(t *testing.T) {
	v := newTestView(t, Model{LastMessage: "prior"})
	changeText(v.message, "hello world")
	clickCheckBox(v.amendCheck)
	got := v.request()
	if got.Message != "prior" || !got.Amend || got.LastMessage != "prior" {
		t.Fatalf("request = %+v", got)
	}
}

func TestConfirmInvokesOnOKOnlyWithANonBlankMessage(t *testing.T) {
	v := newTestView(t, Model{})
	called := 0
	v.OnOK = func(Model) { called++ }

	v.confirm()
	if called != 0 {
		t.Fatal("confirm must be a no-op while the message is blank")
	}

	changeText(v.message, "fix bug")
	v.confirm()
	if called != 1 {
		t.Fatalf("confirm must invoke OnOK once, called = %d", called)
	}
}

func TestConfirmToleratesNilOnOK(t *testing.T) {
	v := newTestView(t, Model{Message: "fix bug"})
	v.confirm()
}

func TestCancelInvokesOnCancel(t *testing.T) {
	v := newTestView(t, Model{})
	called := 0
	v.OnCancel = func() { called++ }
	v.cancel()
	if called != 1 {
		t.Fatal("cancel must invoke OnCancel")
	}
}

func TestCancelToleratesNilOnCancel(t *testing.T) {
	v := newTestView(t, Model{})
	v.cancel()
}

func TestDialogHandlesCtrlEnterAsConfirmAndLeavesPlainEnterToTheFocusedWidget(t *testing.T) {
	v := newTestView(t, Model{Message: "fix bug"})
	okCalled := 0
	v.OnOK = func(Model) { okCalled++ }

	if v.modal.HandleInputBinding(widget.KeyEnter, 0) {
		t.Fatal("plain Enter must not be claimed by the dialog (the message box needs it for newlines)")
	}
	if okCalled != 0 {
		t.Fatal("plain Enter must not confirm")
	}

	if !v.modal.HandleInputBinding(widget.KeyEnter, widget.ModCtrl) {
		t.Fatal("Ctrl+Enter must be handled by the dialog")
	}
	if okCalled != 1 {
		t.Fatalf("Ctrl+Enter must confirm, called = %d", okCalled)
	}
}

func TestDialogWiresCancelActionToCancel(t *testing.T) {
	v := newTestView(t, Model{})
	cancelCalled := 0
	v.OnCancel = func() { cancelCalled++ }

	v.dlg.OnCancel()
	if cancelCalled != 1 {
		t.Fatalf("Escape must cancel, called = %d", cancelCalled)
	}
}

func TestDialogTitleUsesLocalizedText(t *testing.T) {
	v := newTestView(t, Model{})
	if v.dlg.Title != i18n.T("Dialog.Commit.Title") {
		t.Fatalf("title = %q", v.dlg.Title)
	}
	if v.Dialog() == nil {
		t.Fatal("Dialog() must return the modal wrapper")
	}
}
