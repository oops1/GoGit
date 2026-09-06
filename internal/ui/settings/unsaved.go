package settings

import (
	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/i18n"
)

func unsavedChangesAliases() map[string]string {
	return map[string]string{
		"dlg.yes":    "Dialog.Settings.Unsaved.Save",
		"dlg.no":     "Dialog.Settings.Unsaved.Discard",
		"dlg.cancel": "Dialog.Settings.Unsaved.Cancel",
	}
}

func clearUnsavedChangesAliases() {
	widget.AliasStrings(map[string]string{
		"dlg.yes":    "",
		"dlg.no":     "",
		"dlg.cancel": "",
	})
}

func (v *View) confirmUnsavedChanges() {
	widget.AliasStrings(unsavedChangesAliases())
	dlg := widget.NewMessageBox(v.eng).ShowYesNoCancel(
		i18n.T("Dialog.Settings.Unsaved.Title"),
		i18n.T("Dialog.Settings.Unsaved.Message"),
		v.resolveUnsavedChanges,
	)
	dlg.CancelAction = v.abortUnsavedChanges
	v.unsavedDialog = dlg
}

func (v *View) resolveUnsavedChanges(result widget.MessageBoxResult) {
	clearUnsavedChangesAliases()
	switch result {
	case widget.MBResultYes:
		v.confirm()
	case widget.MBResultNo:
		v.doCancel()
	case widget.MBResultCancel:
		v.reopenAfterAbortedClose()
	}
}

func (v *View) abortUnsavedChanges() {
	clearUnsavedChangesAliases()
	v.reopenAfterAbortedClose()
}

func (v *View) reopenAfterAbortedClose() {
	if !v.dlg.IsModal() {
		v.eng.ShowModal(v.dlg)
	}
}
