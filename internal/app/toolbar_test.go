package app

import (
	"strings"
	"testing"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/ui/settings"
)

type toolbarTestButton struct {
	btn   *widget.Button
	arrow int
}

func (b toolbarTestButton) captionWidth() int {
	return toolbarCaptionButtonWidth(b.btn.Text) + b.arrow
}

func allToolbarButtons(t *testing.T, a *App) map[string]toolbarTestButton {
	t.Helper()
	buttons := map[string]toolbarTestButton{}
	for _, name := range toolbarButtons {
		buttons[name] = toolbarTestButton{btn: a.Widget(name).(*widget.Button)}
	}
	for _, entry := range toolbarMenuButtons() {
		menu, ok := a.Widget(entry.Name).(*widget.MenuButton)
		if !ok {
			t.Fatalf("toolbar button %q is %T, want a menu button", entry.Name, a.Widget(entry.Name))
		}
		buttons[entry.Name] = toolbarTestButton{btn: menu.Button, arrow: toolbarMenuArrowWidth}
	}
	return buttons
}

func TestToolbarButtonWidthFitsCaptionInEveryLanguage(t *testing.T) {
	a := newTestApp(t)
	for _, lang := range []string{"en", "ru"} {
		a.SetLanguage(lang)
		for name, button := range allToolbarButtons(t, a) {
			btn := button.btn
			want := button.captionWidth()
			if got := btn.Bounds().Dx(); got < want {
				t.Fatalf("lang %q: button %q width = %d, want at least %d for caption %q", lang, name, got, want, btn.Text)
			}
		}
	}
}

func TestToolbarButtonsShareOneWidthAcrossAllCaptions(t *testing.T) {
	a := newTestApp(t)
	a.SetLanguage("ru")
	widths := map[int]bool{}
	for _, button := range allToolbarButtons(t, a) {
		widths[button.btn.Bounds().Dx()] = true
	}
	if len(widths) != 1 {
		t.Fatalf("toolbar buttons have %d distinct widths, want 1", len(widths))
	}
}

func TestToolbarButtonWidthIsClampedToTheMaximum(t *testing.T) {
	a := newTestApp(t)
	btn := a.Widget("btnCommit").(*widget.Button)
	btn.Text = strings.Repeat("Ж", 100)
	if got := a.toolbarCaptionsWidth(); got != toolbarButtonMaxWidth {
		t.Fatalf("width = %d, want the clamped maximum %d", got, toolbarButtonMaxWidth)
	}
}

func TestToolbarButtonWidthStaysCompactWithoutCaptions(t *testing.T) {
	a := newTestApp(t)
	a.cfg.UI.ToolbarCaptions = false
	a.SetLanguage("ru")
	for name, button := range allToolbarButtons(t, a) {
		if got := button.btn.Bounds().Dx(); got != toolbarCompactWidth {
			t.Fatalf("button %q width = %d, want %d", name, got, toolbarCompactWidth)
		}
	}
}

func TestApplySettingsRecomputesToolbarWidthWhenCaptionsAreToggled(t *testing.T) {
	a := newTestApp(t)

	m := settings.FromConfig(a.cfg)
	m.ToolbarCaptions = false
	a.applySettings(m, true)
	for name, button := range allToolbarButtons(t, a) {
		btn := button.btn
		if got := btn.Bounds().Dx(); got != toolbarCompactWidth {
			t.Fatalf("captions off: button %q width = %d, want %d", name, got, toolbarCompactWidth)
		}
		if btn.IconPos != widget.IconOnly {
			t.Fatalf("captions off: button %q icon position = %v, want IconOnly", name, btn.IconPos)
		}
	}

	m.ToolbarCaptions = true
	a.applySettings(m, true)
	widths := map[int]bool{}
	for name, button := range allToolbarButtons(t, a) {
		btn := button.btn
		want := button.captionWidth()
		if got := btn.Bounds().Dx(); got < want {
			t.Fatalf("captions on: button %q width = %d, want at least %d for caption %q", name, got, want, btn.Text)
		}
		if btn.IconPos != widget.IconTop {
			t.Fatalf("captions on: button %q icon position = %v, want IconTop", name, btn.IconPos)
		}
		widths[btn.Bounds().Dx()] = true
	}
	if len(widths) != 1 {
		t.Fatalf("toolbar row is uneven: buttons have %d distinct widths, want 1", len(widths))
	}
}
