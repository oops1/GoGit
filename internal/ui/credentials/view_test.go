package credentials

import (
	"errors"
	"image/color"
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

func changeText(in *widget.TextInput, text string) {
	in.SetText(text)
	in.OnChange(text)
}

func fullWidgetSet() map[string]widget.Widget {
	return map[string]widget.Widget{
		"resource": widget.NewWin10Label(""),
		"username": widget.NewTextInput(""),
		"secret":   widget.NewPasswordInput(""),
		"remember": widget.NewCheckBox(""),
		"saveTo":   widget.NewWin10Label(""),
		"hint":     widget.NewWin10Label(""),
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
	for _, key := range []string{"resource", "username", "secret", "remember", "saveTo", "hint", "ok", "cancel"} {
		named := fullWidgetSet()
		delete(named, key)
		v := &View{}
		if err := v.bind(named); err == nil {
			t.Fatalf("missing %q: expected error", key)
		}
	}
	for _, key := range []string{"username", "secret", "remember", "ok", "cancel"} {
		named := fullWidgetSet()
		named[key] = widget.NewWin10Label("wrong-type")
		v := &View{}
		if err := v.bind(named); err == nil {
			t.Fatalf("mistyped %q: expected error", key)
		}
	}
	for _, key := range []string{"resource", "saveTo", "hint"} {
		named := fullWidgetSet()
		named[key] = widget.NewButton("wrong-type")
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

func TestNewViewSetsInitialState(t *testing.T) {
	v := newTestView(t, Request{Resource: "https://example.com/repo.git", Username: "alice"})

	if v.Dialog().Title != i18n.T("Dialog.Credentials.Title") {
		t.Fatalf("title = %q", v.Dialog().Title)
	}
	if v.resourceText.Text() != "https://example.com/repo.git" {
		t.Fatalf("resource = %q", v.resourceText.Text())
	}
	if v.usernameInput.GetText() != "alice" {
		t.Fatalf("username = %q", v.usernameInput.GetText())
	}
	if v.hintLabel.IsVisible() {
		t.Fatal("hint must be hidden when Retry is false")
	}
	if v.hintLabel.Text() != i18n.T("Dialog.Credentials.Error.Empty") {
		t.Fatalf("hint text = %q", v.hintLabel.Text())
	}
}

func TestNewViewShowsWhereRememberWillSave(t *testing.T) {
	const target = "the Go.Git store"
	v := newTestView(t, Request{RememberTarget: target})
	if !v.saveToLabel.IsVisible() {
		t.Fatal("saveTo must be visible when RememberTarget is set")
	}
	want := i18n.Tf("Dialog.Credentials.SaveTo", target)
	if v.saveToLabel.Text() != want {
		t.Fatalf("saveTo text = %q, want %q", v.saveToLabel.Text(), want)
	}
}

func TestNewViewWithoutARememberTargetHidesSaveTo(t *testing.T) {
	v := newTestView(t, Request{})
	if v.saveToLabel.IsVisible() {
		t.Fatal("saveTo must stay hidden without a RememberTarget")
	}
}

func TestSetErrorColorChangesTheHintColor(t *testing.T) {
	v := newTestView(t, Request{})
	want := color.RGBA{R: 220, G: 80, B: 80, A: 255}
	v.SetErrorColor(want)
	if v.hintLabel.TextColor != want {
		t.Fatalf("hint color = %+v, want %+v", v.hintLabel.TextColor, want)
	}
}

func TestNewViewWithRetryShowsHint(t *testing.T) {
	v := newTestView(t, Request{Retry: true})
	if !v.hintLabel.IsVisible() {
		t.Fatal("hint must be visible when Retry is true")
	}
}

func TestTypingNonEmptySecretHidesHint(t *testing.T) {
	v := newTestView(t, Request{Retry: true})
	if !v.hintLabel.IsVisible() {
		t.Fatal("hint must start visible on retry")
	}
	changeText(v.secretInput, "s3cr3t")
	if v.hintLabel.IsVisible() {
		t.Fatal("hint must hide once a non-empty secret is typed")
	}
}

func TestTypingEmptySecretKeepsHintHidden(t *testing.T) {
	v := newTestView(t, Request{})
	changeText(v.secretInput, "")
	if v.hintLabel.IsVisible() {
		t.Fatal("hint must stay hidden while untouched and empty")
	}
}

func TestConfirmWithEmptySecretShowsHintAndDoesNotCallOnOK(t *testing.T) {
	v := newTestView(t, Request{})
	called := 0
	v.OnOK = func(Result) { called++ }

	clickButton(v.okBtn)

	if called != 0 {
		t.Fatalf("OnOK called %d times, want 0", called)
	}
	if !v.hintLabel.IsVisible() {
		t.Fatal("hint must become visible after confirming with an empty secret")
	}
}

func TestConfirmWithSecretCallsOnOKAndZeroesInternalCopy(t *testing.T) {
	v := newTestView(t, Request{Username: "bob"})
	v.usernameInput.SetText("bob")
	v.secretInput.SetText("hunter2")
	v.rememberCheck.SetChecked(true)

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
	if got.Username != "bob" {
		t.Fatalf("username = %q", got.Username)
	}
	if string(got.Secret) != "hunter2" {
		t.Fatalf("secret = %q", got.Secret)
	}
	if !got.Remember {
		t.Fatal("remember must be true")
	}
	if v.secret != nil {
		t.Fatalf("internal secret copy must be nil after OnOK, got %v", v.secret)
	}
	if v.secretInput.GetText() != "" {
		t.Fatal("the secret input must be cleared after confirming")
	}
}

func TestConfirmToleratesNilOnOKAndStillZeroes(t *testing.T) {
	v := newTestView(t, Request{})
	v.secretInput.SetText("hunter2")

	clickButton(v.okBtn)

	if v.secret != nil {
		t.Fatalf("internal secret copy must be nil, got %v", v.secret)
	}
}

func TestCancelClearsSecretInputAndInvokesCallback(t *testing.T) {
	v := newTestView(t, Request{})
	v.secretInput.SetText("hunter2")
	called := 0
	v.OnCancel = func() { called++ }

	clickButton(v.cancelBtn)

	if called != 1 {
		t.Fatalf("OnCancel called %d times, want 1", called)
	}
	if v.secretInput.GetText() != "" {
		t.Fatal("the secret input must be cleared on cancel")
	}
}

func TestCancelToleratesNilCallback(t *testing.T) {
	v := newTestView(t, Request{})
	clickButton(v.cancelBtn)
}

func TestDialogDefaultAndCancelActionsAreWired(t *testing.T) {
	v := newTestView(t, Request{})
	v.secretInput.SetText("hunter2")
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

func TestTheDialogWearsTheColoursOfTheTheme(t *testing.T) {
	theme := widget.Win11DarkTheme()
	p := style.Of(theme)
	v := newTestView(t, Request{})

	v.Restyle(theme)

	if v.usernameInput.Background != p.Field || v.usernameInput.BorderColor != p.Border || v.usernameInput.PaddingX != style.FieldPaddingX {
		t.Fatalf("field = %v on %v, want the shared field style", v.usernameInput.BorderColor, v.usernameInput.Background)
	}
	if v.secretInput.Background != p.Field || v.secretInput.BorderColor != p.Border || v.secretInput.PaddingX != style.FieldPaddingX {
		t.Fatalf("field = %v on %v, want the shared field style", v.secretInput.BorderColor, v.secretInput.Background)
	}
	if v.okBtn.Background != p.Accent || v.okBtn.TextColor != p.OnAccent {
		t.Fatalf("main button = %v on %v, want the accent", v.okBtn.TextColor, v.okBtn.Background)
	}
	if v.cancelBtn.Background != p.Field || v.cancelBtn.BorderColor != p.Border {
		t.Fatalf("quiet button = %v, want the field fill", v.cancelBtn.Background)
	}
}
