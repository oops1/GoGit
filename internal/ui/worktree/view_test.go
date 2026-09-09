package worktree

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/oops1/headless-gui/v3/engine"
	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/style"
)

func newTestView(t *testing.T) *View {
	t.Helper()
	widget.ClearStrings()
	t.Cleanup(widget.ClearStrings)
	if _, err := i18n.Install(""); err != nil {
		t.Fatal(err)
	}
	i18n.Apply("en")
	v, err := NewView(engine.New(800, 600, 30))
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func clickRadio(rb *widget.RadioButton) {
	rb.OnMouseButton(widget.MouseEvent{Button: widget.MouseLeft, Pressed: true})
	rb.OnMouseButton(widget.MouseEvent{Button: widget.MouseLeft, Pressed: false})
}

func changeText(in *widget.TextInput, text string) {
	in.SetText(text)
	in.OnChange(text)
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

	if _, err := NewView(engine.New(800, 600, 30)); !errors.Is(err, wantErr) {
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
			"path":         widget.NewTextInput(""),
			"browse":       widget.NewButton(""),
			"modeNew":      widget.NewRadioButton("", "g"),
			"modeExisting": widget.NewRadioButton("", "g"),
			"modeDetached": widget.NewRadioButton("", "g"),
			"branchLabel":  widget.NewLabel("", widget.CurrentTheme().LabelText),
			"branchName":   widget.NewTextInput(""),
			"branchList":   widget.NewDropdown(),
			"startLabel":   widget.NewLabel("", widget.CurrentTheme().LabelText),
			"startPoint":   widget.NewTextInput(""),
			"noCheckout":   widget.NewCheckBox(""),
			"hint":         widget.NewLabel("", widget.CurrentTheme().LabelText),
			"ok":           widget.NewButton(""),
			"cancel":       widget.NewButton(""),
		}
	}
	for name := range full() {
		named := full()
		delete(named, name)
		loadDialog = func(title string, _ string) (*widget.Dialog, map[string]widget.Widget, error) {
			return widget.NewDialog(title, 10, 10), named, nil
		}
		if _, err := NewView(engine.New(800, 600, 30)); !errors.Is(err, ErrWidgetMissing) {
			t.Fatalf("without %q: err = %v, want ErrWidgetMissing", name, err)
		}
	}
}

func TestTheNewBranchModeShowsTheNameAndTheStartPoint(t *testing.T) {
	v := newTestView(t)

	if !v.branchInput.IsVisible() || v.branchList.IsVisible() || !v.startInput.IsVisible() {
		t.Fatal("a new branch is typed by name and started from a commit")
	}
}

func TestTheExistingBranchModeShowsTheListAlone(t *testing.T) {
	v := newTestView(t)

	clickRadio(v.modeExisting)

	if v.branchInput.IsVisible() || !v.branchList.IsVisible() || v.startInput.IsVisible() {
		t.Fatal("an existing branch is picked from the list and starts where it is")
	}
}

func TestTheDetachedModeHidesTheBranchAltogether(t *testing.T) {
	v := newTestView(t)

	clickRadio(v.modeDetached)

	if v.branchInput.IsVisible() || v.branchList.IsVisible() || v.branchLabel.IsVisible() {
		t.Fatal("a detached worktree has no branch")
	}
	if !v.startInput.IsVisible() {
		t.Fatal("a detached worktree still needs a start point")
	}

	clickRadio(v.modeNew)

	if !v.branchInput.IsVisible() || !v.branchLabel.IsVisible() {
		t.Fatal("going back to a new branch brings its name field back")
	}
}

func TestTheDirectoryFollowsTheBranchUntilItIsTypedIn(t *testing.T) {
	v := newTestView(t)
	v.SetParentDirectory(`C:\repos`)

	changeText(v.branchInput, "feature/login")

	if got, want := v.pathInput.GetText(), filepath.Join(`C:\repos`, "feature-login"); got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}

	changeText(v.pathInput, `C:\elsewhere`)
	changeText(v.branchInput, "other")

	if v.pathInput.GetText() != `C:\elsewhere` {
		t.Fatalf("path = %q, want the one that was typed in", v.pathInput.GetText())
	}
}

func TestAnEmptiedPathGoesBackToFollowingTheBranch(t *testing.T) {
	v := newTestView(t)
	v.SetParentDirectory(`C:\repos`)
	changeText(v.pathInput, `C:\elsewhere`)

	changeText(v.pathInput, "")
	changeText(v.branchInput, "feature")

	if got, want := v.pathInput.GetText(), filepath.Join(`C:\repos`, "feature"); got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
}

func TestThePickedBranchNamesTheDirectory(t *testing.T) {
	v := newTestView(t)
	v.SetKnown(Known{Branches: []string{"main", "feature"}})
	v.SetParentDirectory(`C:\repos`)

	if len(v.Known().Branches) != 2 || v.ParentDirectory() != `C:\repos` {
		t.Fatalf("known = %+v, parent = %q", v.Known(), v.ParentDirectory())
	}

	clickRadio(v.modeExisting)
	v.branchList.SetSelected(1)
	v.branchList.OnChange(1, "feature")

	if got, want := v.pathInput.GetText(), filepath.Join(`C:\repos`, "feature"); got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
}

func TestTheStartPointNamesTheDirectoryOfADetachedWorktree(t *testing.T) {
	v := newTestView(t)
	v.SetParentDirectory(`C:\repos`)

	clickRadio(v.modeDetached)
	changeText(v.startInput, "v1.2.0")

	if got, want := v.pathInput.GetText(), filepath.Join(`C:\repos`, "v1.2.0"); got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
}

func TestTheRequestCarriesTheModeAndTheFields(t *testing.T) {
	v := newTestView(t)
	v.SetKnown(Known{Branches: []string{"main"}})
	changeText(v.pathInput, t.TempDir())
	changeText(v.branchInput, "feature")
	changeText(v.startInput, "main")
	v.noCheckoutBox.SetChecked(true)

	req := v.Request()

	if req.Mode != ModeNewBranch || req.Branch != "feature" || req.StartPoint != "main" || !req.NoCheckout {
		t.Fatalf("request = %+v", req)
	}

	clickRadio(v.modeExisting)
	v.branchList.SetSelected(0)

	if req := v.Request(); req.Mode != ModeExistingBranch || req.Branch != "main" {
		t.Fatalf("request = %+v, want the picked branch", req)
	}
}

func TestConfirmingIsRefusedUntilTheRequestIsValid(t *testing.T) {
	v := newTestView(t)
	called := false
	v.OnOK = func(Request) { called = true }

	v.confirm()

	if called || v.okBtn.IsEnabled() {
		t.Fatal("an incomplete request must not be confirmed")
	}

	changeText(v.pathInput, filepath.Join(t.TempDir(), "wt"))
	changeText(v.branchInput, "feature")
	v.confirm()

	if !called || !v.okBtn.IsEnabled() {
		t.Fatal("a complete request must be confirmed")
	}
}

func TestCancellingReportsBack(t *testing.T) {
	v := newTestView(t)
	called := false
	v.OnCancel = func() { called = true }

	v.cancel()

	if !called {
		t.Fatal("cancelling must be reported")
	}
}

func TestTheDialogWorksWithoutCallbacks(t *testing.T) {
	v := newTestView(t)
	changeText(v.pathInput, filepath.Join(t.TempDir(), "wt"))
	changeText(v.branchInput, "feature")

	v.confirm()
	v.cancel()
}

func TestThePickedFolderReplacesTheTypedPath(t *testing.T) {
	v := newTestView(t)

	v.onFolderPicked("", false)
	if v.pathInput.GetText() != "" {
		t.Fatal("a cancelled picker must leave the path alone")
	}

	v.onFolderPicked(`C:\picked`, true)
	if v.pathInput.GetText() != `C:\picked` {
		t.Fatalf("path = %q, want the picked folder", v.pathInput.GetText())
	}
}

func TestTheHintIsPaintedAsMutedText(t *testing.T) {
	v := newTestView(t)
	theme := widget.Win11DarkTheme()

	v.hintLabel.ApplyTheme(theme)

	if !v.hintLabel.Muted || v.hintLabel.TextColor != theme.InputPlaceholder {
		t.Fatalf("hint colour = %v, want the muted text", v.hintLabel.TextColor)
	}
}

func TestBrowsingOpensThePicker(t *testing.T) {
	v := newTestView(t)

	v.browse()
}

func TestEveryControlIsWiredToTheDialog(t *testing.T) {
	v := newTestView(t)
	v.SetKnown(Known{Branches: []string{"main"}})
	confirmed, cancelled := 0, 0
	v.OnOK = func(Request) { confirmed++ }
	v.OnCancel = func() { cancelled++ }
	changeText(v.pathInput, filepath.Join(t.TempDir(), "wt"))
	changeText(v.branchInput, "feature")

	v.noCheckoutBox.OnChange(true)
	v.branchList.OnChange(0, "main")
	v.okBtn.OnClick()
	v.dlg.DefaultAction()
	v.cancelBtn.OnClick()
	v.dlg.CancelAction()

	if confirmed != 2 || cancelled != 2 {
		t.Fatalf("confirmed = %d, cancelled = %d, want two of each", confirmed, cancelled)
	}
	if v.Dialog() != v.dlg {
		t.Fatal("the dialog must be the one that was loaded")
	}
}

func TestTheDialogWearsTheColoursOfTheTheme(t *testing.T) {
	theme := widget.Win11DarkTheme()
	p := style.Of(theme)
	v := newTestView(t)

	v.Restyle(theme)

	if v.pathInput.Background != p.Field || v.pathInput.BorderColor != p.Border || v.pathInput.PaddingX != style.FieldPaddingX {
		t.Fatalf("field = %v on %v, want the shared field style", v.pathInput.BorderColor, v.pathInput.Background)
	}
	if v.branchInput.Background != p.Field || v.branchInput.BorderColor != p.Border || v.branchInput.PaddingX != style.FieldPaddingX {
		t.Fatalf("field = %v on %v, want the shared field style", v.branchInput.BorderColor, v.branchInput.Background)
	}
	if v.branchList.Background != p.Field || v.branchList.PaddingX != style.FieldPaddingX {
		t.Fatalf("list = %v, want the same style as the fields", v.branchList.Background)
	}
	if v.okBtn.Background != p.Accent || v.okBtn.TextColor != p.OnAccent {
		t.Fatalf("main button = %v on %v, want the accent", v.okBtn.TextColor, v.okBtn.Background)
	}
	if v.cancelBtn.Background != p.Field || v.cancelBtn.BorderColor != p.Border {
		t.Fatalf("quiet button = %v, want the field fill", v.cancelBtn.Background)
	}
}
