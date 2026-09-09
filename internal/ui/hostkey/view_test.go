package hostkey

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

func fullWidgetSet() map[string]widget.Widget {
	return map[string]widget.Widget{
		"host":        widget.NewWin10Label(""),
		"algorithm":   widget.NewWin10Label(""),
		"fingerprint": widget.NewWin10Label(""),
		"warning":     widget.NewWin10Label(""),
		"accept":      widget.NewButton(""),
		"reject":      widget.NewButton(""),
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
	for _, key := range []string{"host", "algorithm", "fingerprint", "warning", "accept", "reject"} {
		named := fullWidgetSet()
		delete(named, key)
		v := &View{}
		if err := v.bind(named); err == nil {
			t.Fatalf("missing %q: expected error", key)
		}
	}
	for _, key := range []string{"host", "algorithm", "fingerprint", "warning"} {
		named := fullWidgetSet()
		named[key] = widget.NewButton("wrong-type")
		v := &View{}
		if err := v.bind(named); err == nil {
			t.Fatalf("mistyped %q: expected error", key)
		}
	}
	for _, key := range []string{"accept", "reject"} {
		named := fullWidgetSet()
		named[key] = widget.NewWin10Label("wrong-type")
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

func TestNewViewSetsInitialStateWithoutChange(t *testing.T) {
	v := newTestView(t, Request{
		Host:        "example.com",
		Algorithm:   "ssh-ed25519",
		Fingerprint: "SHA256:abc123",
	})

	if v.Dialog().Title != i18n.T("Dialog.HostKey.Title") {
		t.Fatalf("title = %q", v.Dialog().Title)
	}
	if v.hostText.Text() != "example.com" {
		t.Fatalf("host = %q", v.hostText.Text())
	}
	if v.algorithmText.Text() != "ssh-ed25519" {
		t.Fatalf("algorithm = %q", v.algorithmText.Text())
	}
	if v.fingerprintText.Text() != "SHA256:abc123" {
		t.Fatalf("fingerprint = %q", v.fingerprintText.Text())
	}
	if v.warningLabel.IsVisible() {
		t.Fatal("warning must be hidden when the key has not changed")
	}
	if !v.acceptBtn.IsEnabled() {
		t.Fatal("accept must be enabled when the key has not changed")
	}
}

func TestNewViewWithChangedKeyDisablesAcceptAndShowsWarning(t *testing.T) {
	v := newTestView(t, Request{Changed: true})

	if !v.warningLabel.IsVisible() {
		t.Fatal("warning must be visible when the key has changed")
	}
	if v.warningLabel.Text() != i18n.T("Dialog.HostKey.Changed") {
		t.Fatalf("warning text = %q", v.warningLabel.Text())
	}
	if v.acceptBtn.IsEnabled() {
		t.Fatal("accept must be disabled when the key has changed")
	}
}

func TestAcceptClickInvokesCallbackWhenNotChanged(t *testing.T) {
	v := newTestView(t, Request{})
	called := 0
	v.OnAccept = func() { called++ }

	clickButton(v.acceptBtn)

	if called != 1 {
		t.Fatalf("OnAccept called %d times, want 1", called)
	}
}

func TestAcceptClickDoesNothingWhenChanged(t *testing.T) {
	v := newTestView(t, Request{Changed: true})
	called := 0
	v.OnAccept = func() { called++ }

	clickButton(v.acceptBtn)

	if called != 0 {
		t.Fatalf("OnAccept called %d times, want 0", called)
	}
}

func TestAcceptToleratesNilCallback(t *testing.T) {
	v := newTestView(t, Request{})
	clickButton(v.acceptBtn)
}

func TestRejectClickInvokesCallbackRegardlessOfChanged(t *testing.T) {
	for _, changed := range []bool{false, true} {
		v := newTestView(t, Request{Changed: changed})
		called := 0
		v.OnReject = func() { called++ }

		clickButton(v.rejectBtn)

		if called != 1 {
			t.Fatalf("changed=%v: OnReject called %d times, want 1", changed, called)
		}
	}
}

func TestRejectToleratesNilCallback(t *testing.T) {
	v := newTestView(t, Request{})
	clickButton(v.rejectBtn)
}

func TestDialogDefaultActionRespectsAcceptEnabledState(t *testing.T) {
	v := newTestView(t, Request{Changed: true})
	called := 0
	v.OnAccept = func() { called++ }

	v.Dialog().DefaultAction()

	if called != 0 {
		t.Fatalf("DefaultAction must not accept a changed key, called %d times", called)
	}
}

func TestDialogCancelActionInvokesReject(t *testing.T) {
	v := newTestView(t, Request{})
	called := 0
	v.OnReject = func() { called++ }

	v.Dialog().CancelAction()

	if called != 1 {
		t.Fatalf("CancelAction: OnReject called %d times, want 1", called)
	}
}

func TestTheDialogWearsTheColoursOfTheTheme(t *testing.T) {
	theme := widget.Win11DarkTheme()
	p := style.Of(theme)
	v := newTestView(t, Request{})

	v.Restyle(theme)

	if v.acceptBtn.Background != p.Field || v.acceptBtn.BorderColor != p.Border {
		t.Fatalf("quiet button = %v, want the field fill", v.acceptBtn.Background)
	}
}
