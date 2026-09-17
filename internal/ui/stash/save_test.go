package stash

import (
	"errors"
	"testing"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/i18n"
)

func newTestSaveView(t *testing.T, selectedFiles int) *SaveView {
	t.Helper()
	widget.ClearStrings()
	t.Cleanup(widget.ClearStrings)
	if _, err := i18n.Install(""); err != nil {
		t.Fatal(err)
	}
	i18n.Apply("en")
	v, err := NewSaveView(selectedFiles)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestNewSaveViewPropagatesLoadDialogError(t *testing.T) {
	widget.ClearStrings()
	t.Cleanup(widget.ClearStrings)
	wantErr := errors.New("boom")
	prev := loadDialog
	loadDialog = func(string, string) (*widget.Dialog, map[string]widget.Widget, error) {
		return nil, nil, wantErr
	}
	t.Cleanup(func() { loadDialog = prev })

	if _, err := NewSaveView(0); !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
}

func TestNewSaveViewReportsEveryMissingWidget(t *testing.T) {
	widget.ClearStrings()
	t.Cleanup(widget.ClearStrings)
	prev := loadDialog
	t.Cleanup(func() { loadDialog = prev })
	full := func() map[string]widget.Widget {
		return map[string]widget.Widget{
			"scopeLabel":       widget.NewLabel("", widget.CurrentTheme().LabelText),
			"messageLabel":     widget.NewLabel("", widget.CurrentTheme().LabelText),
			"message":          widget.NewTextInput(""),
			"includeUntracked": widget.NewCheckBox(""),
			"keepIndex":        widget.NewCheckBox(""),
			"ok":               widget.NewButton(""),
			"cancel":           widget.NewButton(""),
		}
	}
	for name := range full() {
		named := full()
		delete(named, name)
		loadDialog = func(string, string) (*widget.Dialog, map[string]widget.Widget, error) {
			return widget.NewDialog("", 100, 100), named, nil
		}
		if _, err := NewSaveView(0); !errors.Is(err, ErrWidgetMissing) {
			t.Fatalf("without %s: err = %v", name, err)
		}
	}
}

func TestTheSaveDialogNamesWhatItStashes(t *testing.T) {
	all := newTestSaveView(t, 0)
	if all.Dialog().Title != i18n.T("Dialog.SaveStash.Title") || all.scopeLabel.Text() != i18n.T("Dialog.SaveStash.Scope.All") || !all.untrackedBox.IsVisible() {
		t.Fatalf("whole tree: title = %q, scope = %q, untracked visible = %v", all.Dialog().Title, all.scopeLabel.Text(), all.untrackedBox.IsVisible())
	}
	selection := newTestSaveView(t, 3)
	if selection.Dialog().Title != i18n.T("Dialog.StashSelection.Title") || selection.scopeLabel.Text() != i18n.Tf("Dialog.SaveStash.Scope.Selection", 3) || selection.untrackedBox.IsVisible() {
		t.Fatalf("selection: title = %q, scope = %q, untracked visible = %v", selection.Dialog().Title, selection.scopeLabel.Text(), selection.untrackedBox.IsVisible())
	}
}

func TestTheSaveRequestCarriesTheMessageAndTheOptions(t *testing.T) {
	cases := []struct {
		name          string
		selectedFiles int
		message       string
		untracked     bool
		keepIndex     bool
		want          SaveRequest
	}{
		{"plain", 0, "", false, false, SaveRequest{}},
		{"trimmed message", 0, "  wip  ", false, false, SaveRequest{Message: "wip"}},
		{"untracked", 0, "", true, false, SaveRequest{IncludeUntracked: true}},
		{"keep index", 0, "m", false, true, SaveRequest{Message: "m", KeepIndex: true}},
		{"selection ignores the hidden untracked box", 2, "", true, true, SaveRequest{KeepIndex: true}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v := newTestSaveView(t, c.selectedFiles)
			v.messageBox.SetText(c.message)
			if c.untracked {
				v.untrackedBox.SetChecked(true)
			}
			if c.keepIndex {
				toggle(v.keepIndexBox)
			}
			if got := v.Request(); got != c.want {
				t.Fatalf("request = %+v, want %+v", got, c.want)
			}
		})
	}
}

func TestTheSaveDialogReachesTheCallbacks(t *testing.T) {
	v := newTestSaveView(t, 0)
	var requests []SaveRequest
	cancelled := 0
	v.OnOK = func(r SaveRequest) { requests = append(requests, r) }
	v.OnCancel = func() { cancelled++ }
	v.messageBox.SetText("keep")

	v.Dialog().DefaultAction()
	v.okBtn.OnClick()
	v.cancelBtn.OnClick()
	v.Dialog().CancelAction()

	if len(requests) != 2 || requests[0].Message != "keep" || cancelled != 2 {
		t.Fatalf("requests = %+v, cancelled = %d", requests, cancelled)
	}
}

func TestTheSaveDialogCallbacksAreOptional(t *testing.T) {
	v := newTestSaveView(t, 0)
	v.Dialog().DefaultAction()
	v.Dialog().CancelAction()
}

func TestTheSaveDialogRestylesInBothThemes(t *testing.T) {
	v := newTestSaveView(t, 1)
	v.Restyle(widget.Win11DarkTheme())
	v.Restyle(widget.Win11LightTheme())
}
