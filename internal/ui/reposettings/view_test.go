package reposettings

import (
	"errors"
	"testing"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/i18n"
)

func newTestView(t *testing.T) *View {
	t.Helper()
	widget.ClearStrings()
	t.Cleanup(widget.ClearStrings)
	if _, err := i18n.Install(""); err != nil {
		t.Fatal(err)
	}
	i18n.Apply("en")
	v, err := NewView()
	if err != nil {
		t.Fatal(err)
	}
	return v
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

	if _, err := NewView(); !errors.Is(err, wantErr) {
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
			"name":          widget.NewTextInput(""),
			"path":          widget.NewLabel("", widget.CurrentTheme().LabelText),
			"userName":      widget.NewTextInput(""),
			"userEmail":     widget.NewTextInput(""),
			"defaultRemote": widget.NewDropdown(),
			"pullStrategy":  widget.NewDropdown(),
			"autoFetch":     widget.NewDropdown(),
			"hint":          widget.NewLabel("", widget.CurrentTheme().LabelText),
			"ok":            widget.NewButton(""),
			"cancel":        widget.NewButton(""),
		}
	}
	for name := range full() {
		named := full()
		delete(named, name)
		loadDialog = func(title string, _ string) (*widget.Dialog, map[string]widget.Widget, error) {
			return widget.NewDialog(title, 10, 10), named, nil
		}
		if _, err := NewView(); !errors.Is(err, ErrWidgetMissing) {
			t.Fatalf("without %q: err = %v, want ErrWidgetMissing", name, err)
		}
	}
}

func TestTheDialogShowsWhatTheRepositoryHasNow(t *testing.T) {
	v := newTestView(t)
	v.SetRemotes([]string{"origin", "backup"})

	v.Apply(Settings{
		Name:          "Main",
		Path:          `C:\repos\main`,
		UserName:      "Ann",
		UserEmail:     "ann@example.com",
		DefaultRemote: "backup",
		PullStrategy:  PullRebase,
		AutoFetch:     AutoFetchOff,
	})

	got := v.Settings()
	if got.Name != "Main" || got.UserName != "Ann" || got.UserEmail != "ann@example.com" {
		t.Fatalf("settings = %+v", got)
	}
	if got.DefaultRemote != "backup" || got.PullStrategy != PullRebase || got.AutoFetch != AutoFetchOff {
		t.Fatalf("settings = %+v, want the choices that were applied", got)
	}
	if v.pathLabel.Text() != `C:\repos\main` {
		t.Fatalf("path = %q", v.pathLabel.Text())
	}
}

func TestAnEmptyChoiceMeansInherited(t *testing.T) {
	v := newTestView(t)
	v.SetRemotes([]string{"origin"})

	v.Apply(Settings{Name: "Main"})

	got := v.Settings()
	if got.DefaultRemote != "" || got.PullStrategy != PullInherit || got.AutoFetch != AutoFetchInherit {
		t.Fatalf("settings = %+v, want everything inherited", got)
	}
	if v.remoteDrop.Selected() != 0 {
		t.Fatalf("remote = %d, want the inherited row", v.remoteDrop.Selected())
	}
}

func TestARemoteThatIsGoneFallsBackToInherited(t *testing.T) {
	v := newTestView(t)
	v.SetRemotes([]string{"origin"})

	v.Apply(Settings{Name: "Main", DefaultRemote: "vanished"})

	if got := v.Settings().DefaultRemote; got != "" {
		t.Fatalf("remote = %q, want the inherited row", got)
	}
}

func TestTheInheritedValuesAreShownAsPlaceholders(t *testing.T) {
	v := newTestView(t)
	v.SetRemotes([]string{"origin"})

	v.SetInherited(Inherited{UserName: "Global Ann", UserEmail: "global@example.com", DefaultRemote: "origin"})

	if v.userNameInput.Placeholder != "Global Ann" || v.emailInput.Placeholder != "global@example.com" {
		t.Fatal("the inherited identity must show as a placeholder")
	}
	if v.remoteDrop.Items()[0] != i18n.Tf("Dialog.RepoSettings.Remote.InheritNamed", "origin") {
		t.Fatalf("first row = %q, want the inherited remote named", v.remoteDrop.Items()[0])
	}
	if len(v.remoteDrop.Items()) != 2 {
		t.Fatalf("rows = %v, want the inherited one and origin", v.remoteDrop.Items())
	}
}

func TestWithoutAnInheritedRemoteTheRowIsPlain(t *testing.T) {
	v := newTestView(t)

	if v.remoteDrop.Items()[0] != i18n.T("Dialog.RepoSettings.Remote.Inherit") {
		t.Fatalf("first row = %q, want the plain inherited row", v.remoteDrop.Items()[0])
	}
}

func TestSavingIsRefusedUntilTheSettingsAreValid(t *testing.T) {
	v := newTestView(t)
	saved := 0
	v.OnOK = func(Settings) { saved++ }

	v.Apply(Settings{Name: ""})
	v.confirm()

	if saved != 0 || v.okBtn.IsEnabled() {
		t.Fatal("a repository without a name cannot be saved")
	}

	changeText(v.nameInput, "Main")
	v.confirm()

	if saved != 1 || !v.okBtn.IsEnabled() {
		t.Fatal("a named repository must be saved")
	}
}

func TestTheIdentityIsCheckedAsItIsTyped(t *testing.T) {
	v := newTestView(t)
	v.Apply(Settings{Name: "Main"})

	changeText(v.userNameInput, "Ann")

	if v.okBtn.IsEnabled() {
		t.Fatal("half an identity cannot be saved")
	}

	changeText(v.emailInput, "ann@example.com")

	if !v.okBtn.IsEnabled() {
		t.Fatal("a whole identity must be saved")
	}
}

func TestCancellingReportsBack(t *testing.T) {
	v := newTestView(t)
	cancelled := 0
	v.OnCancel = func() { cancelled++ }

	v.cancelBtn.OnClick()
	v.dlg.CancelAction()

	if cancelled != 2 {
		t.Fatalf("cancelled = %d, want both ways to report", cancelled)
	}
}

func TestTheDialogWorksWithoutCallbacks(t *testing.T) {
	v := newTestView(t)
	v.Apply(Settings{Name: "Main"})

	v.okBtn.OnClick()
	v.dlg.DefaultAction()
	v.cancel()

	if v.Dialog() != v.dlg {
		t.Fatal("the dialog must be the one that was loaded")
	}
}

func TestEveryChoiceHasARowOfItsOwn(t *testing.T) {
	v := newTestView(t)

	if len(v.pullDrop.Items()) != len(pullOrder) {
		t.Fatalf("pull rows = %v, want one per strategy", v.pullDrop.Items())
	}
	if len(v.autoFetchDrop.Items()) != len(autoFetchOrder) {
		t.Fatalf("fetch rows = %v, want one per choice", v.autoFetchDrop.Items())
	}
}

func TestAChoiceOutsideTheListIsIgnored(t *testing.T) {
	v := newTestView(t)
	v.pullDrop.SetSelected(99)
	v.autoFetchDrop.SetSelected(-3)

	got := v.Settings()

	if got.PullStrategy != PullInherit || got.AutoFetch != AutoFetchInherit {
		t.Fatalf("settings = %+v, want the inherited choices", got)
	}
}

func TestAValueThatIsNotOnTheListSelectsTheFirstRow(t *testing.T) {
	v := newTestView(t)

	v.Apply(Settings{Name: "Main", PullStrategy: "no-such-strategy", AutoFetch: "maybe"})

	if v.pullDrop.Selected() != 0 || v.autoFetchDrop.Selected() != 0 {
		t.Fatalf("pull = %d, fetch = %d, want the inherited rows", v.pullDrop.Selected(), v.autoFetchDrop.Selected())
	}
}
