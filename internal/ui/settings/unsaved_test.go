package settings

import (
	"testing"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/i18n"
)

func TestCancelWithUnsavedChangesAsksBeforeDiscarding(t *testing.T) {
	v := newTestView(t, []string{"en"}, Model{})
	t.Cleanup(widget.ClearStringAliases)
	v.logMaxCount.SetValue(v.logMaxCount.Value() + 50)

	cancelCalled := 0
	v.OnCancel = func() { cancelCalled++ }
	v.cancel()

	if cancelCalled != 0 {
		t.Fatal("cancel with unsaved changes must not discard before the user answers")
	}
	if v.unsavedDialog == nil {
		t.Fatal("cancel with unsaved changes must show the confirmation dialog")
	}
}

func TestConfirmUnsavedChangesAliasesTheGenericButtonsToItsOwnWording(t *testing.T) {
	v := newTestView(t, []string{"en"}, Model{})
	t.Cleanup(widget.ClearStringAliases)

	v.confirmUnsavedChanges()

	if got, want := widget.Tr("dlg.yes"), i18n.T("Dialog.Settings.Unsaved.Save"); got != want {
		t.Fatalf("dlg.yes = %q, want %q", got, want)
	}
	if got, want := widget.Tr("dlg.no"), i18n.T("Dialog.Settings.Unsaved.Discard"); got != want {
		t.Fatalf("dlg.no = %q, want %q", got, want)
	}
	if got, want := widget.Tr("dlg.cancel"), i18n.T("Dialog.Settings.Unsaved.Cancel"); got != want {
		t.Fatalf("dlg.cancel = %q, want %q", got, want)
	}

	v.resolveUnsavedChanges(widget.MBResultCancel)

	if got, unwanted := widget.Tr("dlg.yes"), i18n.T("Dialog.Settings.Unsaved.Save"); got == unwanted {
		t.Fatalf("dlg.yes after resolving = %q, want the alias removed", got)
	}
}

func TestResolveUnsavedChangesSaveCallsOnOKWithTheCurrentModel(t *testing.T) {
	v := newTestView(t, []string{"en"}, Model{})
	t.Cleanup(widget.ClearStringAliases)
	v.logMaxCount.SetValue(600)

	var got Model
	okCalled := 0
	v.OnOK = func(m Model) { got = m; okCalled++ }
	cancelCalled := 0
	v.OnCancel = func() { cancelCalled++ }

	v.resolveUnsavedChanges(widget.MBResultYes)

	if okCalled != 1 {
		t.Fatalf("OnOK called = %d, want 1", okCalled)
	}
	if cancelCalled != 0 {
		t.Fatal("OnCancel must not run when the user chose to save")
	}
	if got.LogMaxCount != 600 {
		t.Fatalf("model = %+v", got)
	}
}

func TestResolveUnsavedChangesDiscardCallsOnCancel(t *testing.T) {
	v := newTestView(t, []string{"en"}, Model{})
	t.Cleanup(widget.ClearStringAliases)

	okCalled := 0
	v.OnOK = func(Model) { okCalled++ }
	cancelCalled := 0
	v.OnCancel = func() { cancelCalled++ }

	v.resolveUnsavedChanges(widget.MBResultNo)

	if cancelCalled != 1 {
		t.Fatalf("OnCancel called = %d, want 1", cancelCalled)
	}
	if okCalled != 0 {
		t.Fatal("OnOK must not run when the user chose to discard")
	}
}

func TestResolveUnsavedChangesCancelCallsNeitherCallback(t *testing.T) {
	v := newTestView(t, []string{"en"}, Model{})
	t.Cleanup(widget.ClearStringAliases)

	okCalled, cancelCalled := 0, 0
	v.OnOK = func(Model) { okCalled++ }
	v.OnCancel = func() { cancelCalled++ }

	v.resolveUnsavedChanges(widget.MBResultCancel)

	if okCalled != 0 || cancelCalled != 0 {
		t.Fatalf("aborting must call neither callback, got OnOK=%d OnCancel=%d", okCalled, cancelCalled)
	}
}

func TestAbortingUnsavedChangesReopensTheDialogTheEngineForceClosed(t *testing.T) {
	v := newTestView(t, []string{"en"}, Model{})
	t.Cleanup(widget.ClearStringAliases)

	v.eng.ShowModal(v.dlg)
	v.eng.CloseModal(v.dlg)
	if v.dlg.IsModal() {
		t.Fatal("test setup: dialog must be closed before aborting")
	}

	v.abortUnsavedChanges()

	if !v.dlg.IsModal() {
		t.Fatal("aborting must reopen a dialog the engine force-closed on Escape or the close button")
	}
}

func TestAbortingUnsavedChangesLeavesAnAlreadyOpenDialogAlone(t *testing.T) {
	v := newTestView(t, []string{"en"}, Model{})
	t.Cleanup(widget.ClearStringAliases)

	v.eng.ShowModal(v.dlg)

	v.abortUnsavedChanges()

	if !v.dlg.IsModal() {
		t.Fatal("dialog must remain open when it was never closed (Cancel button path)")
	}
}

func TestEscapeOnTheSettingsDialogWithUnsavedChangesEndsUpAskingAndCanBeAborted(t *testing.T) {
	v := newTestView(t, []string{"en"}, Model{})
	t.Cleanup(widget.ClearStringAliases)
	v.logMaxCount.SetValue(v.logMaxCount.Value() + 50)

	okCalled, cancelCalled := 0, 0
	v.OnOK = func(Model) { okCalled++ }
	v.OnCancel = func() { cancelCalled++ }

	v.eng.ShowModal(v.dlg)

	v.dlg.OnCancel()
	v.eng.CloseModal(v.dlg)

	if okCalled != 0 || cancelCalled != 0 {
		t.Fatal("Escape must not decide anything on its own while changes are unsaved")
	}
	if v.unsavedDialog == nil {
		t.Fatal("Escape with unsaved changes must show the confirmation dialog")
	}

	v.unsavedDialog.CancelAction()

	if !v.dlg.IsModal() {
		t.Fatal("aborting the confirmation must bring the settings dialog back")
	}
}

func TestClickingSaveInsideTheConfirmationDialogClosesItAndSaves(t *testing.T) {
	v := newTestView(t, []string{"en"}, Model{})
	t.Cleanup(widget.ClearStringAliases)
	v.logMaxCount.SetValue(v.logMaxCount.Value() + 50)

	okCalled := 0
	v.OnOK = func(Model) { okCalled++ }

	v.confirmUnsavedChanges()
	btn := findButtonByText(t, v.unsavedDialog, i18n.T("Dialog.Settings.Unsaved.Save"))
	btn.OnClick()

	if okCalled != 1 {
		t.Fatalf("OnOK called = %d, want 1", okCalled)
	}
}

func findButtonByText(t *testing.T, root widget.Widget, text string) *widget.Button {
	t.Helper()
	var found *widget.Button
	var walk func(w widget.Widget)
	walk = func(w widget.Widget) {
		if found != nil || w == nil {
			return
		}
		if btn, ok := w.(*widget.Button); ok && btn.Text == text {
			found = btn
			return
		}
		if c, ok := w.(interface{ Children() []widget.Widget }); ok {
			for _, child := range c.Children() {
				walk(child)
				if found != nil {
					return
				}
			}
		}
	}
	walk(root)
	if found == nil {
		t.Fatalf("no button with text %q found", text)
	}
	return found
}
