package stash

import (
	"errors"
	"testing"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/i18n"
)

func newTestView(t *testing.T, mode Mode) *View {
	t.Helper()
	widget.ClearStrings()
	t.Cleanup(widget.ClearStrings)
	if _, err := i18n.Install(""); err != nil {
		t.Fatal(err)
	}
	i18n.Apply("en")
	v, err := NewView(mode)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func toggle(box *widget.CheckBox) {
	box.OnMouseButton(widget.MouseEvent{Button: widget.MouseLeft, Pressed: true})
	box.OnMouseButton(widget.MouseEvent{Button: widget.MouseLeft, Pressed: false})
}

func TestNewViewPropagatesLoadDialogError(t *testing.T) {
	widget.ClearStrings()
	t.Cleanup(widget.ClearStrings)
	wantErr := errors.New("boom")
	prev := loadDialog
	loadDialog = func(string, string) (*widget.Dialog, map[string]widget.Widget, error) {
		return nil, nil, wantErr
	}
	t.Cleanup(func() { loadDialog = prev })

	if _, err := NewView(ModeApply); !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
}

func TestNewViewReportsEveryMissingWidget(t *testing.T) {
	widget.ClearStrings()
	t.Cleanup(widget.ClearStrings)
	prev := loadDialog
	t.Cleanup(func() { loadDialog = prev })
	full := func() map[string]widget.Widget {
		return map[string]widget.Widget{
			"entryLabel":     widget.NewLabel("", widget.CurrentTheme().LabelText),
			"entries":        widget.NewDropdown(),
			"dropAfterApply": widget.NewCheckBox(""),
			"restoreIndex":   widget.NewCheckBox(""),
			"ok":             widget.NewButton(""),
			"cancel":         widget.NewButton(""),
		}
	}
	for name := range full() {
		named := full()
		delete(named, name)
		loadDialog = func(string, string) (*widget.Dialog, map[string]widget.Widget, error) {
			return widget.NewDialog("", 100, 100), named, nil
		}
		if _, err := NewView(ModeDrop); !errors.Is(err, ErrWidgetMissing) {
			t.Fatalf("without %s: err = %v", name, err)
		}
	}
}

func TestTheModeNamesTheActionAndShowsTheApplyOptions(t *testing.T) {
	apply := newTestView(t, ModeApply)
	if apply.okBtn.Text != i18n.T("Dialog.ApplyStash.OK") || !apply.dropBox.IsVisible() || !apply.indexBox.IsVisible() {
		t.Fatalf("apply mode: ok = %q, drop visible = %v, index visible = %v", apply.okBtn.Text, apply.dropBox.IsVisible(), apply.indexBox.IsVisible())
	}
	drop := newTestView(t, ModeDrop)
	if drop.okBtn.Text != i18n.T("Dialog.DropStash.OK") || drop.dropBox.IsVisible() || drop.indexBox.IsVisible() {
		t.Fatalf("drop mode: ok = %q, drop visible = %v, index visible = %v", drop.okBtn.Text, drop.dropBox.IsVisible(), drop.indexBox.IsVisible())
	}
}

func TestTheApplyRequestCarriesTheRestoreIndexChoice(t *testing.T) {
	v := newTestView(t, ModeApply)
	v.SetEntries([]string{"stash@{0}: a"}, 0)
	toggle(v.indexBox)
	if got := v.Request(); got != (Request{Index: 0, RestoreIndex: true}) {
		t.Fatalf("request = %+v", got)
	}
	drop := newTestView(t, ModeDrop)
	drop.SetEntries([]string{"stash@{0}: a"}, 0)
	drop.indexBox.SetChecked(true)
	if got := drop.Request(); got != (Request{Index: 0, Drop: true}) {
		t.Fatalf("drop request = %+v", got)
	}
}

func TestTheRequestFollowsTheSelectedEntry(t *testing.T) {
	v := newTestView(t, ModeApply)
	v.SetEntries([]string{"stash@{0}: a", "stash@{1}: b", "stash@{2}: c"}, 1)
	if got := v.Request(); got != (Request{Index: 1}) {
		t.Fatalf("request = %+v", got)
	}
	toggle(v.dropBox)
	v.SetEntries([]string{"stash@{0}: a", "stash@{1}: b"}, 7)
	if got := v.Request(); got != (Request{Index: 1, Drop: true}) {
		t.Fatalf("request = %+v", got)
	}
	drop := newTestView(t, ModeDrop)
	drop.SetEntries([]string{"stash@{0}: a"}, -3)
	if got := drop.Request(); got != (Request{Index: 0, Drop: true}) {
		t.Fatalf("drop request = %+v", got)
	}
}

func TestConfirmNeedsAnEntryAndReachesTheCallbacks(t *testing.T) {
	v := newTestView(t, ModeApply)
	var requests []Request
	cancelled := 0
	v.OnOK = func(r Request) { requests = append(requests, r) }
	v.OnCancel = func() { cancelled++ }

	v.Dialog().DefaultAction()
	if len(requests) != 0 || v.okBtn.IsEnabled() {
		t.Fatal("an empty list confirmed")
	}
	v.SetEntries([]string{"stash@{0}: a"}, 0)
	v.Dialog().DefaultAction()
	v.Dialog().CancelAction()
	if len(requests) != 1 || cancelled != 1 {
		t.Fatalf("requests = %+v, cancelled = %d", requests, cancelled)
	}
}

func TestCallbacksAreOptional(t *testing.T) {
	v := newTestView(t, ModeApply)
	v.SetEntries([]string{"stash@{0}: a"}, 0)
	v.Dialog().DefaultAction()
	v.Dialog().CancelAction()
}

func TestRestyleAcceptsBothThemes(t *testing.T) {
	v := newTestView(t, ModeApply)
	v.Restyle(widget.Win11DarkTheme())
	v.Restyle(widget.Win11LightTheme())
}
