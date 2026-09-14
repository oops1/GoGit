package app

import (
	"testing"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/config"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/dialogs"
)

type dialogOwnerView struct {
	owned []*widget.Dialog
}

func (dialogOwnerView) Restyle(*widget.Theme) {}

func (v dialogOwnerView) OwnedDialogs() []*widget.Dialog { return v.owned }

func loadedAddRepoDialog(t *testing.T) (*widget.Dialog, *widget.Button) {
	t.Helper()
	dlg, named, err := dialogs.Load("add_repo", "Title")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { dialogs.Release(dlg) })
	return dlg, named["browse"].(*widget.Button)
}

func restoreLanguageAfter(t *testing.T) {
	t.Helper()
	t.Cleanup(func() { i18n.Apply(config.Default().Language) })
}

func TestAnOpenDialogFollowsTheLanguage(t *testing.T) {
	a := newTestApp(t)
	restoreLanguageAfter(t)
	dlg, button := loadedAddRepoDialog(t)
	runOnDispatcher(t, a, func() { a.showModal(dlg, dialogOwnerView{}) })
	english := readOnDispatcher(t, a, button.GetText)

	runOnDispatcher(t, a, func() { a.SetLanguage("ru") })

	if got := readOnDispatcher(t, a, button.GetText); got == english {
		t.Fatalf("button = %q, want the open dialog translated", got)
	}
	runOnDispatcher(t, a, func() { a.eng.CloseModal(dlg) })
}

func TestAClosedDialogNoLongerFollowsTheLanguage(t *testing.T) {
	a := newTestApp(t)
	restoreLanguageAfter(t)
	dlg, button := loadedAddRepoDialog(t)
	editor, editorButton := loadedAddRepoDialog(t)
	runOnDispatcher(t, a, func() { a.showModal(dlg, dialogOwnerView{owned: []*widget.Dialog{editor}}) })
	english := readOnDispatcher(t, a, button.GetText)

	runOnDispatcher(t, a, func() { a.eng.CloseModal(dlg) })
	runOnDispatcher(t, a, func() { a.SetLanguage("ru") })

	if got := readOnDispatcher(t, a, button.GetText); got != english {
		t.Fatalf("closed dialog button = %q, want it left at %q", got, english)
	}
	if got := readOnDispatcher(t, a, editorButton.GetText); got != english {
		t.Fatalf("owned dialog button = %q, want it released with its owner", got)
	}
}

func TestADialogClosedOutsideTheEngineIsReleasedBeforeTheLanguageChanges(t *testing.T) {
	a := newTestApp(t)
	restoreLanguageAfter(t)
	dlg, button := loadedAddRepoDialog(t)
	runOnDispatcher(t, a, func() { a.showModal(dlg, dialogOwnerView{}) })
	english := readOnDispatcher(t, a, button.GetText)

	runOnDispatcher(t, a, func() { dlg.SetModal(false) })
	runOnDispatcher(t, a, func() { a.SetLanguage("ru") })

	if got := readOnDispatcher(t, a, button.GetText); got != english {
		t.Fatalf("button = %q, want it left at %q", got, english)
	}
	runOnDispatcher(t, a, func() { a.eng.CloseModal(dlg) })
}

func TestClosingTheAppReleasesTheDialogsStillOpen(t *testing.T) {
	a := newTestApp(t)
	restoreLanguageAfter(t)
	dlg, button := loadedAddRepoDialog(t)
	runOnDispatcher(t, a, func() { a.showModal(dlg, dialogOwnerView{}) })
	english := readOnDispatcher(t, a, button.GetText)

	a.Close()
	i18n.Apply("ru")

	if got := button.GetText(); got != english {
		t.Fatalf("button = %q, want it left at %q", got, english)
	}
	if len(a.shownDialogs) != 0 {
		t.Fatal("the app still tracks dialogs after closing")
	}
}
