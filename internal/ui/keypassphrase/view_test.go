package keypassphrase

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

func fullWidgetSet() map[string]widget.Widget {
	return map[string]widget.Widget{
		"key":        widget.NewWin10Label(""),
		"host":       widget.NewWin10Label(""),
		"passphrase": widget.NewPasswordInput(""),
		"remember":   widget.NewCheckBox(""),
		"hint":       widget.NewWin10Label(""),
		"ok":         widget.NewButton(""),
		"cancel":     widget.NewButton(""),
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

	if _, err := NewView(engine.New(800, 600, 30), Request{}); !errors.Is(err, wantErr) {
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

	if _, err := NewView(engine.New(800, 600, 30), Request{}); !errors.Is(err, ErrWidgetMissing) {
		t.Fatalf("err = %v, want %v", err, ErrWidgetMissing)
	}
}

func TestBindReturnsErrorForEachMissingOrMistypedWidget(t *testing.T) {
	for key := range fullWidgetSet() {
		named := fullWidgetSet()
		delete(named, key)
		if err := (&View{}).bind(named); !errors.Is(err, ErrWidgetMissing) {
			t.Fatalf("missing %q: err = %v", key, err)
		}
		named = fullWidgetSet()
		named[key] = widget.NewPanel(color.RGBA{})
		if err := (&View{}).bind(named); !errors.Is(err, ErrWidgetMissing) {
			t.Fatalf("mistyped %q: err = %v", key, err)
		}
	}
	if err := (&View{}).bind(fullWidgetSet()); err != nil {
		t.Fatal(err)
	}
}

func TestNewViewShowsTheKeyAndTheHost(t *testing.T) {
	v := newTestView(t, Request{Host: "github.com", Path: "/home/me/.ssh/id_ed25519"})

	if v.Dialog().Title != i18n.T("Dialog.KeyPassphrase.Title") {
		t.Fatalf("title = %q", v.Dialog().Title)
	}
	if v.keyText.Text() != "/home/me/.ssh/id_ed25519" {
		t.Fatalf("key = %q", v.keyText.Text())
	}
	if v.hostText.Text() != i18n.Tf("Dialog.KeyPassphrase.Host", "github.com") {
		t.Fatalf("host = %q", v.hostText.Text())
	}
	if v.hintLabel.IsVisible() {
		t.Fatal("the wrong passphrase hint must be hidden on the first question")
	}
}

func TestRetryShowsTheHintUntilTheUserTypes(t *testing.T) {
	v := newTestView(t, Request{Retry: true})
	if !v.hintLabel.IsVisible() || v.hintLabel.Text() != i18n.T("Dialog.KeyPassphrase.Wrong") {
		t.Fatal("the wrong passphrase hint must be visible on a retry")
	}
	v.passphraseInput.SetText("x")
	v.passphraseInput.OnChange("x")
	if v.hintLabel.IsVisible() {
		t.Fatal("the hint must hide once the user edits the passphrase")
	}
}

func TestConfirmHandsOverThePassphraseAndRememberChoice(t *testing.T) {
	v := newTestView(t, Request{})
	v.passphraseInput.SetText("open sesame")
	v.rememberCheck.SetChecked(true)
	var got Result
	v.OnOK = func(r Result) { got = r }

	clickButton(v.okBtn)

	if string(got.Passphrase) != "open sesame" || !got.Remember {
		t.Fatalf("result = %q remember=%v", got.Passphrase, got.Remember)
	}
	if v.passphrase != nil || v.passphraseInput.GetText() != "" {
		t.Fatal("the passphrase must not stay in the dialog after confirming")
	}
}

func TestConfirmAndCancelTolerateMissingCallbacks(t *testing.T) {
	v := newTestView(t, Request{})
	v.passphraseInput.SetText("open sesame")
	v.Dialog().DefaultAction()
	v.passphraseInput.SetText("again")
	v.Dialog().CancelAction()
	if v.passphraseInput.GetText() != "" {
		t.Fatal("cancel must wipe the typed passphrase")
	}
}

func TestCancelInvokesTheCallback(t *testing.T) {
	v := newTestView(t, Request{})
	called := 0
	v.OnCancel = func() { called++ }
	clickButton(v.cancelBtn)
	if called != 1 {
		t.Fatalf("OnCancel called %d times", called)
	}
}

func TestTheDialogWearsTheColoursOfTheTheme(t *testing.T) {
	theme := widget.Win11DarkTheme()
	p := style.Of(theme)
	v := newTestView(t, Request{})

	v.Restyle(theme)
	v.SetErrorColor(color.RGBA{R: 1, A: 255})

	if v.passphraseInput.Background != p.Field || v.okBtn.Background != p.Accent || v.cancelBtn.Background != p.Field {
		t.Fatal("the dialog does not use the shared styles")
	}
	if v.hintLabel.TextColor != (color.RGBA{R: 1, A: 255}) {
		t.Fatalf("hint colour = %v", v.hintLabel.TextColor)
	}
}
