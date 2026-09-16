package masterpassword

import (
	"errors"
	"testing"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/style"
)

func installEnglish(t *testing.T) {
	t.Helper()
	widget.ClearStrings()
	t.Cleanup(widget.ClearStrings)
	if _, err := i18n.Install(""); err != nil {
		t.Fatal(err)
	}
	i18n.Apply("en")
}

func newTestView(t *testing.T, req Request) *View {
	t.Helper()
	installEnglish(t)
	v, err := NewView(req)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func clickButton(btn *widget.Button) {
	btn.OnMouseButton(widget.MouseEvent{Button: widget.MouseLeft, Pressed: true})
	btn.OnMouseButton(widget.MouseEvent{Button: widget.MouseLeft, Pressed: false})
}

func typeInto(in *widget.TextInput, text string) {
	in.SetText(text)
	in.OnChange(text)
}

func fullWidgetSet() map[string]widget.Widget {
	return map[string]widget.Widget{
		"prompt":       widget.NewWin10Label(""),
		"currentLabel": widget.NewWin10Label(""),
		"strength":     widget.NewWin10Label(""),
		"hint":         widget.NewWin10Label(""),
		"current":      widget.NewPasswordInput(""),
		"password":     widget.NewPasswordInput(""),
		"confirm":      widget.NewPasswordInput(""),
		"ok":           widget.NewButton(""),
		"cancel":       widget.NewButton(""),
	}
}

func TestNewViewPropagatesLoadDialogError(t *testing.T) {
	installEnglish(t)
	prev := loadDialog
	wantErr := errors.New("boom")
	loadDialog = func(string, string) (*widget.Dialog, map[string]widget.Widget, error) {
		return nil, nil, wantErr
	}
	t.Cleanup(func() { loadDialog = prev })
	if _, err := NewView(Request{}); !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
}

func TestNewViewPropagatesBindError(t *testing.T) {
	installEnglish(t)
	prev := loadDialog
	loadDialog = func(_, title string) (*widget.Dialog, map[string]widget.Widget, error) {
		return widget.NewDialog(title, 10, 10), map[string]widget.Widget{}, nil
	}
	t.Cleanup(func() { loadDialog = prev })
	if _, err := NewView(Request{}); !errors.Is(err, ErrWidgetMissing) {
		t.Fatalf("err = %v, want %v", err, ErrWidgetMissing)
	}
}

func TestBindReportsEveryMissingOrMistypedWidget(t *testing.T) {
	for name := range fullWidgetSet() {
		named := fullWidgetSet()
		delete(named, name)
		if err := (&View{}).bind(named); !errors.Is(err, ErrWidgetMissing) {
			t.Fatalf("missing %q: err = %v", name, err)
		}
		named = fullWidgetSet()
		if _, isLabel := named[name].(*widget.Label); isLabel {
			named[name] = widget.NewButton("wrong-type")
		} else {
			named[name] = widget.NewWin10Label("wrong-type")
		}
		if err := (&View{}).bind(named); !errors.Is(err, ErrWidgetMissing) {
			t.Fatalf("mistyped %q: err = %v", name, err)
		}
	}
	if err := (&View{}).bind(fullWidgetSet()); err != nil {
		t.Fatal(err)
	}
}

func TestNewViewShowsTheCurrentPasswordOnlyWhenChanging(t *testing.T) {
	cases := []struct {
		req        Request
		prompt     string
		current    bool
		hintShown  bool
		hintString string
	}{
		{Request{}, "Dialog.MasterPassword.PromptSet", false, false, ""},
		{Request{Backup: true}, "Dialog.MasterPassword.PromptBackup", false, false, ""},
		{Request{Change: true}, "Dialog.MasterPassword.PromptChange", true, false, ""},
		{Request{Change: true, WrongCurrent: true}, "Dialog.MasterPassword.PromptChange", true, true, "Dialog.MasterPassword.WrongCurrent"},
	}
	for _, c := range cases {
		v := newTestView(t, c.req)
		if v.Dialog().Title != i18n.T("Dialog.MasterPassword.Title") {
			t.Fatalf("title = %q", v.Dialog().Title)
		}
		if v.promptLabel.Text() != i18n.T(c.prompt) {
			t.Fatalf("%+v: prompt = %q", c.req, v.promptLabel.Text())
		}
		if v.currentInput.IsVisible() != c.current || v.currentLabel.IsVisible() != c.current {
			t.Fatalf("%+v: current visible = %v", c.req, v.currentInput.IsVisible())
		}
		if v.hintLabel.IsVisible() != c.hintShown || (c.hintShown && v.hintLabel.Text() != i18n.T(c.hintString)) {
			t.Fatalf("%+v: hint = %v %q", c.req, v.hintLabel.IsVisible(), v.hintLabel.Text())
		}
	}
}

func TestTypingUpdatesStrengthAndHidesTheHint(t *testing.T) {
	v := newTestView(t, Request{Change: true, WrongCurrent: true})
	typeInto(v.currentInput, "x")
	if v.hintLabel.IsVisible() {
		t.Fatal("editing the current password must hide the hint")
	}
	v.showHint("again")
	typeInto(v.passwordInput, "Tr0ub4dor&3-horse")
	if v.hintLabel.IsVisible() || v.strengthLabel.Text() != i18n.T("Dialog.MasterPassword.Strength.Strong") {
		t.Fatalf("strength = %q", v.strengthLabel.Text())
	}
	v.showHint("again")
	typeInto(v.confirmInput, "x")
	if v.hintLabel.IsVisible() {
		t.Fatal("editing the confirmation must hide the hint")
	}
}

func TestStrengthText(t *testing.T) {
	installEnglish(t)
	cases := map[string]string{
		"":                  "",
		"Ab1!":              "Dialog.MasterPassword.Strength.Weak",
		"abcdefghij":        "Dialog.MasterPassword.Strength.Weak",
		"abcdefgh12":        "Dialog.MasterPassword.Strength.Weak",
		"abcdEFGH12":        "Dialog.MasterPassword.Strength.Fair",
		"abcdefghijkl12":    "Dialog.MasterPassword.Strength.Fair",
		"abcdEFGH12!?":      "Dialog.MasterPassword.Strength.Strong",
		"correcthorsebat12": "Dialog.MasterPassword.Strength.Fair",
	}
	for password, key := range cases {
		want := ""
		if key != "" {
			want = i18n.T(key)
		}
		if got := StrengthText(password); got != want {
			t.Fatalf("%q: got %q, want %q", password, got, want)
		}
	}
}

func TestValidate(t *testing.T) {
	installEnglish(t)
	cases := []struct {
		change                    bool
		current, password, repeat string
		want                      string
	}{
		{true, "", "longenough", "longenough", i18n.T("Dialog.MasterPassword.NeedCurrent")},
		{false, "", "short", "short", i18n.Tf("Dialog.MasterPassword.TooShort", MinPasswordLength)},
		{false, "", "longenough", "different1", i18n.T("Dialog.MasterPassword.Mismatch")},
		{false, "", "longenough", "longenough", ""},
		{true, "old", "longenough", "longenough", ""},
	}
	for _, c := range cases {
		if got := Validate(c.change, []byte(c.current), []byte(c.password), []byte(c.repeat)); got != c.want {
			t.Fatalf("%+v: got %q", c, got)
		}
	}
}

func TestConfirmWithInvalidInputShowsTheHintAndKeepsTheDialog(t *testing.T) {
	v := newTestView(t, Request{})
	called := false
	v.OnOK = func(Result) { called = true }
	typeInto(v.passwordInput, "longenough")
	typeInto(v.confirmInput, "different1")
	clickButton(v.okBtn)
	if called || !v.hintLabel.IsVisible() || v.hintLabel.Text() != i18n.T("Dialog.MasterPassword.Mismatch") {
		t.Fatalf("called = %v, hint = %q", called, v.hintLabel.Text())
	}
	if v.passwordInput.GetText() != "" || v.confirmInput.GetText() != "" {
		t.Fatal("both fields must be cleared after a failed attempt")
	}
}

func TestConfirmHandsOverCopiesOfThePasswords(t *testing.T) {
	v := newTestView(t, Request{Change: true})
	var got Result
	v.OnOK = func(r Result) { got = r }
	typeInto(v.currentInput, "old-password")
	typeInto(v.passwordInput, "new-password")
	typeInto(v.confirmInput, "new-password")
	clickButton(v.okBtn)
	if string(got.Current) != "old-password" || string(got.Password) != "new-password" {
		t.Fatalf("result = %q / %q", got.Current, got.Password)
	}
	got.Wipe()
	if got.Current != nil || got.Password != nil {
		t.Fatal("Wipe must drop both passwords")
	}
}

func TestConfirmToleratesMissingCallback(t *testing.T) {
	v := newTestView(t, Request{})
	typeInto(v.passwordInput, "new-password")
	typeInto(v.confirmInput, "new-password")
	v.Dialog().DefaultAction()
	if v.passwordInput.GetText() != "" {
		t.Fatal("the field must be cleared")
	}
}

func TestCancelWipesEveryFieldAndInvokesTheCallback(t *testing.T) {
	v := newTestView(t, Request{Change: true})
	called := 0
	v.OnCancel = func() { called++ }
	typeInto(v.currentInput, "a")
	typeInto(v.passwordInput, "b")
	typeInto(v.confirmInput, "c")
	clickButton(v.cancelBtn)
	if called != 1 || v.currentInput.GetText() != "" || v.passwordInput.GetText() != "" || v.confirmInput.GetText() != "" {
		t.Fatalf("called = %d", called)
	}
	v.OnCancel = nil
	v.Dialog().CancelAction()
}

func TestTheDialogWearsTheColoursOfTheTheme(t *testing.T) {
	theme := widget.Win11DarkTheme()
	p := style.Of(theme)
	v := newTestView(t, Request{})
	v.Restyle(theme)
	for _, in := range []*widget.TextInput{v.currentInput, v.passwordInput, v.confirmInput} {
		if in.Background != p.Field || in.BorderColor != p.Border {
			t.Fatalf("field = %v on %v, want the shared field style", in.BorderColor, in.Background)
		}
	}
	if v.okBtn.Background != p.Accent || v.cancelBtn.Background != p.Field {
		t.Fatalf("buttons = %v / %v", v.okBtn.Background, v.cancelBtn.Background)
	}
}
