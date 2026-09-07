package settings

import (
	"strings"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/i18n"
)

const (
	credentialEditorName = "credential_edit"
	keyEditorName        = "ssh_key_edit"
)

func (v *View) openCredentialEditor(entry SecretEntry, existing bool) {
	v.credentialResource.SetText(entry.Resource)
	v.credentialResource.SetEnabled(!existing)
	v.credentialUsername.SetText(entry.Username)
	v.credentialType.SetSelected(credentialAuthTypeIndex(entry.Type))
	v.credentialSecret.SetText("")
	v.credentialEditStatus.SetText("")
	v.credentialEditor.Title = i18n.T(editorTitleKey(existing, "Dialog.Settings.Group.AddCredential", "Dialog.Settings.Group.EditCredential"))
	v.eng.ShowModal(v.credentialEditor)
}

func (v *View) openKeyEditor(entry KeyEntry, existing bool) {
	isDefault := entry.Host == defaultKeyHost
	v.sshUseDefaultCheckBox.SetChecked(isDefault)
	v.sshHostInput.SetText(hostForEditor(entry.Host))
	v.sshHostInput.SetEnabled(!isDefault)
	v.sshPathInput.SetText(entry.Path)
	v.sshPassphraseInput.SetText("")
	v.sshEditStatus.SetText("")
	v.keyEditor.Title = i18n.T(editorTitleKey(existing, "Dialog.Settings.Group.AddHost", "Dialog.Settings.Group.EditHost"))
	v.eng.ShowModal(v.keyEditor)
}

func hostForEditor(host string) string {
	if host == defaultKeyHost {
		return ""
	}
	return host
}

func editorTitleKey(existing bool, addKey, editKey string) string {
	if existing {
		return editKey
	}
	return addKey
}

func (v *View) wireEditors() {
	v.credentialEditOK.OnClick = v.saveCredentialFromEditor
	v.credentialEditCancel.OnClick = v.closeCredentialEditor
	v.credentialEditor.DefaultAction = v.saveCredentialFromEditor
	v.credentialEditor.CancelAction = v.closeCredentialEditor

	v.keyEditOK.OnClick = v.saveKeyFromEditor
	v.keyEditCancel.OnClick = v.closeKeyEditor
	v.keyEditor.DefaultAction = v.saveKeyFromEditor
	v.keyEditor.CancelAction = v.closeKeyEditor
}

func (v *View) closeCredentialEditor() { v.eng.CloseModal(v.credentialEditor) }

func (v *View) closeKeyEditor() { v.eng.CloseModal(v.keyEditor) }

func (v *View) saveCredentialFromEditor() {
	resource := strings.TrimSpace(v.credentialResource.GetText())
	username := strings.TrimSpace(v.credentialUsername.GetText())
	secret := v.credentialSecret.TakeSecret()
	defer clear(secret)
	if resource == "" {
		v.reportEditorProblem(v.credentialEditStatus, "Dialog.Settings.Secrets.Status.NeedResource")
		return
	}
	if len(secret) == 0 {
		v.reportEditorProblem(v.credentialEditStatus, "Dialog.Settings.Secrets.Status.NeedSecret")
		return
	}
	if v.OnAddCredential != nil {
		v.OnAddCredential(resource, username, secret)
	}
	v.closeCredentialEditor()
}

func (v *View) saveKeyFromEditor() {
	host := defaultKeyHost
	if !v.sshUseDefaultCheckBox.IsChecked() {
		host = strings.TrimSpace(v.sshHostInput.GetText())
	}
	path := strings.TrimSpace(v.sshPathInput.GetText())
	passphrase := v.sshPassphraseInput.TakeSecret()
	defer clear(passphrase)
	if host == "" {
		v.reportEditorProblem(v.sshEditStatus, "Dialog.Settings.Secrets.Status.NeedHost")
		return
	}
	if path == "" {
		v.reportEditorProblem(v.sshEditStatus, "Dialog.Settings.Secrets.Status.NeedKeyFile")
		return
	}
	if v.OnAddKey != nil {
		v.OnAddKey(host, path, passphrase)
	}
	v.closeKeyEditor()
}

func (v *View) reportEditorProblem(label *widget.Label, key string) {
	label.SetText(i18n.T(key))
	label.TextColor = StatusError.DotColor(widget.CurrentTheme())
}
