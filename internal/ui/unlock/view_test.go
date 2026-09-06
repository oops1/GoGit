package unlock

import (
	"errors"
	"testing"

	"github.com/oops1/headless-gui/v3/engine"
	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/i18n"
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

func changeText(in *widget.TextInput, text string) {
	in.SetText(text)
	in.OnChange(text)
}

func fullWidgetSet() map[string]widget.Widget {
	return map[string]widget.Widget{
		"hint":     widget.NewWin10Label(""),
		"password": widget.NewPasswordInput(""),
		"ok":       widget.NewButton(""),
		"cancel":   widget.NewButton(""),
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
	for _, key := range []string{"hint", "password", "ok", "cancel"} {
		named := fullWidgetSet()
		delete(named, key)
		v := &View{}
		if err := v.bind(named); err == nil {
			t.Fatalf("missing %q: expected error", key)
		}
	}
	for _, key := range []string{"password", "ok", "cancel"} {
		named := fullWidgetSet()
		named[key] = widget.NewWin10Label("wrong-type")
		v := &View{}
		if err := v.bind(named); err == nil {
			t.Fatalf("mistyped %q: expected error", key)
		}
	}
	named := fullWidgetSet()
	named["hint"] = widget.NewButton("wrong-type")
	v := &View{}
	if err := v.bind(named); err == nil {
		t.Fatal("mistyped \"hint\": expected error")
	}
}

func TestBindSucceedsWithAllWidgetsPresent(t *testing.T) {
	v := &View{}
	if err := v.bind(fullWidgetSet()); err != nil {
		t.Fatal(err)
	}
}

func TestNewViewSetsInitialState(t *testing.T) {
	v := newTestView(t, Request{})

	if v.Dialog().Title != i18n.T("Dialog.Unlock.Title") {
		t.Fatalf("title = %q", v.Dialog().Title)
	}
	if v.hintLabel.IsVisible() {
		t.Fatal("hint must be hidden when Retry is false")
	}
	if v.hintLabel.Text() != i18n.T("Dialog.Unlock.Wrong") {
		t.Fatalf("hint text = %q", v.hintLabel.Text())
	}
}

func TestNewViewWithRetryShowsWrongPasswordHint(t *testing.T) {
	v := newTestView(t, Request{Retry: true})
	if !v.hintLabel.IsVisible() {
		t.Fatal("hint must be visible when Retry is true")
	}
}

func TestTypingPasswordHidesHint(t *testing.T) {
	v := newTestView(t, Request{Retry: true})
	changeText(v.passwordInput, "x")
	if v.hintLabel.IsVisible() {
		t.Fatal("hint must hide once the user edits the password")
	}
}

func TestConfirmCallsOnOKAndZeroesInternalCopy(t *testing.T) {
	v := newTestView(t, Request{})
	v.passwordInput.SetText("m4sterpass")

	var got Result
	called := 0
	v.OnOK = func(r Result) {
		called++
		got = r
	}

	clickButton(v.okBtn)

	if called != 1 {
		t.Fatalf("OnOK called %d times, want 1", called)
	}
	if string(got.Password) != "m4sterpass" {
		t.Fatalf("password = %q", got.Password)
	}
	if v.password != nil {
		t.Fatalf("internal password copy must be nil after OnOK, got %v", v.password)
	}
	if v.passwordInput.GetText() != "" {
		t.Fatal("the password input must be cleared after confirming")
	}
}

func TestConfirmToleratesNilOnOKAndStillZeroes(t *testing.T) {
	v := newTestView(t, Request{})
	v.passwordInput.SetText("m4sterpass")

	clickButton(v.okBtn)

	if v.password != nil {
		t.Fatalf("internal password copy must be nil, got %v", v.password)
	}
}

func TestCancelClearsPasswordInputAndInvokesCallback(t *testing.T) {
	v := newTestView(t, Request{})
	v.passwordInput.SetText("m4sterpass")
	called := 0
	v.OnCancel = func() { called++ }

	clickButton(v.cancelBtn)

	if called != 1 {
		t.Fatalf("OnCancel called %d times, want 1", called)
	}
	if v.passwordInput.GetText() != "" {
		t.Fatal("the password input must be cleared on cancel")
	}
}

func TestCancelToleratesNilCallback(t *testing.T) {
	v := newTestView(t, Request{})
	clickButton(v.cancelBtn)
}

func TestDialogDefaultAndCancelActionsAreWired(t *testing.T) {
	v := newTestView(t, Request{})
	v.passwordInput.SetText("m4sterpass")
	okCalled, cancelCalled := 0, 0
	v.OnOK = func(Result) { okCalled++ }
	v.OnCancel = func() { cancelCalled++ }

	v.Dialog().DefaultAction()
	if okCalled != 1 {
		t.Fatalf("DefaultAction: OnOK called %d times, want 1", okCalled)
	}

	v.Dialog().CancelAction()
	if cancelCalled != 1 {
		t.Fatalf("CancelAction: OnCancel called %d times, want 1", cancelCalled)
	}
}
