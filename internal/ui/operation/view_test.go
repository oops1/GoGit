package operation

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/oops1/headless-gui/v3/engine"
	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/i18n"
)

func newTestView(t *testing.T, title string) *View {
	t.Helper()
	widget.ClearStrings()
	t.Cleanup(widget.ClearStrings)
	if _, err := i18n.Install(""); err != nil {
		t.Fatal(err)
	}
	i18n.Apply("en")
	eng := engine.New(800, 600, 30)
	v, err := NewView(eng, title)
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
		"status":   widget.NewWin10Label(""),
		"progress": widget.NewProgressBar(),
		"log":      widget.NewListView(),
		"cancel":   widget.NewButton(""),
		"close":    widget.NewButton(""),
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
	if _, err := NewView(eng, "Title"); !errors.Is(err, wantErr) {
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
	if _, err := NewView(eng, "Title"); !errors.Is(err, ErrWidgetMissing) {
		t.Fatalf("err = %v, want %v", err, ErrWidgetMissing)
	}
}

func TestBindReturnsErrorForEachMissingOrMistypedWidget(t *testing.T) {
	for _, key := range []string{"status", "progress", "log", "cancel", "close"} {
		named := fullWidgetSet()
		delete(named, key)
		v := &View{}
		if err := v.bind(named); err == nil {
			t.Fatalf("missing %q: expected error", key)
		}
	}
	for _, key := range []string{"progress", "log", "cancel", "close"} {
		named := fullWidgetSet()
		named[key] = widget.NewWin10Label("wrong-type")
		v := &View{}
		if err := v.bind(named); err == nil {
			t.Fatalf("mistyped %q: expected error", key)
		}
	}
	named := fullWidgetSet()
	named["status"] = widget.NewButton("wrong-type")
	v := &View{}
	if err := v.bind(named); err == nil {
		t.Fatal("mistyped \"status\": expected error")
	}
}

func TestBindSucceedsWithAllWidgetsPresent(t *testing.T) {
	v := &View{}
	if err := v.bind(fullWidgetSet()); err != nil {
		t.Fatal(err)
	}
}

func TestNewViewSetsInitialState(t *testing.T) {
	v := newTestView(t, "Pull origin")

	if v.Dialog().Title != "Pull origin" {
		t.Fatalf("title = %q", v.Dialog().Title)
	}
	if v.statusLabel.Text() != i18n.T("Operation.Status.Running") {
		t.Fatalf("status = %q", v.statusLabel.Text())
	}
	if v.cancelBtn.GetText() != i18n.T("Operation.Cancel") {
		t.Fatalf("cancel text = %q", v.cancelBtn.GetText())
	}
	if v.closeBtn.GetText() != i18n.T("Operation.Close") {
		t.Fatalf("close text = %q", v.closeBtn.GetText())
	}
	if !v.progressBar.IsIndeterminate() {
		t.Fatal("progress bar must start indeterminate")
	}
	if !v.cancelBtn.IsEnabled() {
		t.Fatal("cancel must start enabled")
	}
	if v.closeBtn.IsEnabled() {
		t.Fatal("close must start disabled")
	}
	if !v.Running() {
		t.Fatal("Running() must be true right after NewView")
	}
	if len(v.Lines()) != 0 {
		t.Fatalf("Lines() = %v, want empty", v.Lines())
	}
}

func TestAppendAddsLineWithoutHighlightingIt(t *testing.T) {
	v := newTestView(t, "Title")
	v.Append("first")
	v.Append("second")

	lines := v.Lines()
	if len(lines) != 2 || lines[0] != "first" || lines[1] != "second" {
		t.Fatalf("lines = %v", lines)
	}
	if got := v.logList.Items(); len(got) != 2 || got[1] != "second" {
		t.Fatalf("log items = %v", got)
	}
	if v.logList.Selected() >= 0 {
		t.Fatalf("the log must not highlight a line, selected = %d", v.logList.Selected())
	}
}

func TestAppendTrimsToMaxLogLines(t *testing.T) {
	v := newTestView(t, "Title")
	for i := 0; i < maxLogLines+10; i++ {
		v.Append(fmt.Sprintf("line-%d", i))
	}

	lines := v.Lines()
	if len(lines) != maxLogLines {
		t.Fatalf("len(lines) = %d, want %d", len(lines), maxLogLines)
	}
	if lines[0] != "line-10" {
		t.Fatalf("lines[0] = %q, want line-10", lines[0])
	}
	if lines[len(lines)-1] != fmt.Sprintf("line-%d", maxLogLines+9) {
		t.Fatalf("last line = %q", lines[len(lines)-1])
	}
	if v.logList.ScrollY() <= 0 {
		t.Fatalf("the log did not scroll to the last line, scroll = %d", v.logList.ScrollY())
	}
}

func TestLinesReturnsIndependentCopy(t *testing.T) {
	v := newTestView(t, "Title")
	v.Append("one")

	lines := v.Lines()
	lines[0] = "mutated"

	if v.Lines()[0] != "one" {
		t.Fatalf("internal state must not be affected by mutating Lines() result")
	}
}

func TestSetStatusUpdatesLabel(t *testing.T) {
	v := newTestView(t, "Title")
	v.SetStatus("custom status")
	if v.statusLabel.Text() != "custom status" {
		t.Fatalf("status = %q", v.statusLabel.Text())
	}
}

func TestSetIndeterminateTogglesProgressBar(t *testing.T) {
	v := newTestView(t, "Title")

	v.SetIndeterminate(false)
	if v.progressBar.IsIndeterminate() {
		t.Fatal("progress bar must not be indeterminate")
	}

	v.SetIndeterminate(true)
	if !v.progressBar.IsIndeterminate() {
		t.Fatal("progress bar must be indeterminate")
	}
}

func TestSetProgressClampsValueAndDisablesIndeterminate(t *testing.T) {
	tests := []struct {
		name string
		in   float64
		want float64
	}{
		{"below zero clamps to zero", -0.5, 0},
		{"within range is kept", 0.42, 0.42},
		{"above one clamps to one", 1.5, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := newTestView(t, "Title")
			v.SetProgress(tt.in)
			if v.progressBar.IsIndeterminate() {
				t.Fatal("SetProgress must turn off indeterminate mode")
			}
			if got := v.progressBar.Value(); got < tt.want-1e-6 || got > tt.want+1e-6 {
				t.Fatalf("value = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFinishWithNilErrorMarksDoneAndTogglesButtons(t *testing.T) {
	v := newTestView(t, "Title")
	v.Finish(nil)

	if v.Running() {
		t.Fatal("Running() must be false after Finish")
	}
	if v.progressBar.IsIndeterminate() {
		t.Fatal("progress bar must not be indeterminate after Finish")
	}
	if v.progressBar.Value() != 1 {
		t.Fatalf("progress value = %v, want 1", v.progressBar.Value())
	}
	if v.statusLabel.Text() != i18n.T("Operation.Status.Done") {
		t.Fatalf("status = %q", v.statusLabel.Text())
	}
	if v.cancelBtn.IsEnabled() {
		t.Fatal("cancel must be disabled after Finish")
	}
	if !v.closeBtn.IsEnabled() {
		t.Fatal("close must be enabled after Finish")
	}
	if len(v.Lines()) != 0 {
		t.Fatalf("lines = %v, want empty on success", v.Lines())
	}
}

func TestFinishWithCanceledErrorShowsCanceledStatus(t *testing.T) {
	v := newTestView(t, "Title")
	wrapped := fmt.Errorf("stopping: %w", context.Canceled)
	v.Finish(wrapped)

	if v.statusLabel.Text() != i18n.T("Operation.Status.Canceled") {
		t.Fatalf("status = %q", v.statusLabel.Text())
	}
	lines := v.Lines()
	if len(lines) != 1 || lines[0] != wrapped.Error() {
		t.Fatalf("lines = %v", lines)
	}
}

func TestFinishWithErrorShowsFailedStatus(t *testing.T) {
	v := newTestView(t, "Title")
	wantErr := errors.New("network unreachable")
	v.Finish(wantErr)

	if v.statusLabel.Text() != i18n.T("Operation.Status.Failed") {
		t.Fatalf("status = %q", v.statusLabel.Text())
	}
	lines := v.Lines()
	if len(lines) != 1 || lines[0] != wantErr.Error() {
		t.Fatalf("lines = %v", lines)
	}
	if v.cancelBtn.IsEnabled() {
		t.Fatal("cancel must be disabled after Finish")
	}
	if !v.closeBtn.IsEnabled() {
		t.Fatal("close must be enabled after Finish")
	}
}

func TestFinishIsIdempotent(t *testing.T) {
	v := newTestView(t, "Title")
	v.Finish(errors.New("first"))
	v.Finish(errors.New("second"))

	lines := v.Lines()
	if len(lines) != 1 || lines[0] != "first" {
		t.Fatalf("lines = %v, second Finish must be a no-op", lines)
	}
	if v.statusLabel.Text() != i18n.T("Operation.Status.Failed") {
		t.Fatalf("status = %q", v.statusLabel.Text())
	}
}

func TestCancelClickAppendsLogDisablesCancelAndInvokesCallback(t *testing.T) {
	v := newTestView(t, "Title")
	called := 0
	v.OnCancel = func() { called++ }

	clickButton(v.cancelBtn)

	if called != 1 {
		t.Fatalf("OnCancel called %d times, want 1", called)
	}
	if v.cancelBtn.IsEnabled() {
		t.Fatal("cancel must be disabled after click")
	}
	lines := v.Lines()
	if len(lines) != 1 || lines[0] != i18n.T("Operation.Log.Canceling") {
		t.Fatalf("lines = %v", lines)
	}
}

func TestCancelClickToleratesNilCallback(t *testing.T) {
	v := newTestView(t, "Title")
	clickButton(v.cancelBtn)
}

func TestCloseClickInvokesCallback(t *testing.T) {
	v := newTestView(t, "Title")
	v.Finish(nil)
	called := 0
	v.OnClose = func() { called++ }

	clickButton(v.closeBtn)

	if called != 1 {
		t.Fatalf("OnClose called %d times, want 1", called)
	}
}

func TestCloseClickToleratesNilCallback(t *testing.T) {
	v := newTestView(t, "Title")
	v.Finish(nil)
	clickButton(v.closeBtn)
}

func TestDialogTitleUsesProvidedTitle(t *testing.T) {
	v := newTestView(t, "Fetch all remotes")
	if v.Dialog().Title != "Fetch all remotes" {
		t.Fatalf("title = %q", v.Dialog().Title)
	}
}
