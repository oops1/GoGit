package settings

import (
	"fmt"
	"image/color"
	"strings"

	"github.com/oops1/headless-gui/v3/widget"
	"github.com/oops1/headless-gui/v3/widget/datagrid"

	"github.com/oops1/gogit/internal/i18n"
)

type SecretEntry struct {
	Resource string
	Username string
}

type KeyEntry struct {
	Host string
	Path string
}

const (
	columnHeaderResource   = "Dialog.Settings.Credentials.Resource"
	columnHeaderUsername   = "Dialog.Settings.Credentials.User"
	columnHeaderSSHHost    = "Dialog.Settings.SSH.Host"
	columnHeaderSSHKeyFile = "Dialog.Settings.SSH.KeyFile"
)

func (v *View) bindSecrets(named map[string]widget.Widget) error {
	var ok bool
	if v.credentialsTable, ok = named["credentialsTable"].(*widget.DataGridWidget); !ok {
		return fmt.Errorf("%w: credentialsTable", ErrWidgetMissing)
	}
	if v.credentialResource, ok = named["credentialResource"].(*widget.TextInput); !ok {
		return fmt.Errorf("%w: credentialResource", ErrWidgetMissing)
	}
	if v.credentialUsername, ok = named["credentialUsername"].(*widget.TextInput); !ok {
		return fmt.Errorf("%w: credentialUsername", ErrWidgetMissing)
	}
	if v.credentialSecret, ok = named["credentialSecret"].(*widget.TextInput); !ok {
		return fmt.Errorf("%w: credentialSecret", ErrWidgetMissing)
	}
	if v.credentialAddBtn, ok = named["credentialAdd"].(*widget.Button); !ok {
		return fmt.Errorf("%w: credentialAdd", ErrWidgetMissing)
	}
	if v.credentialRemoveBtn, ok = named["credentialRemove"].(*widget.Button); !ok {
		return fmt.Errorf("%w: credentialRemove", ErrWidgetMissing)
	}
	if v.masterPasswordBtn, ok = named["credentialMasterPassword"].(*widget.Button); !ok {
		return fmt.Errorf("%w: credentialMasterPassword", ErrWidgetMissing)
	}
	if v.credentialsStatus, ok = named["credentialsStatus"].(*widget.Label); !ok {
		return fmt.Errorf("%w: credentialsStatus", ErrWidgetMissing)
	}
	if v.credentialsUnlockBtn, ok = named["credentialsUnlock"].(*widget.Button); !ok {
		return fmt.Errorf("%w: credentialsUnlock", ErrWidgetMissing)
	}
	if v.sshTable, ok = named["sshTable"].(*widget.DataGridWidget); !ok {
		return fmt.Errorf("%w: sshTable", ErrWidgetMissing)
	}
	if v.sshHostInput, ok = named["sshHost"].(*widget.TextInput); !ok {
		return fmt.Errorf("%w: sshHost", ErrWidgetMissing)
	}
	if v.sshPathInput, ok = named["sshPath"].(*widget.TextInput); !ok {
		return fmt.Errorf("%w: sshPath", ErrWidgetMissing)
	}
	if v.sshBrowseBtn, ok = named["sshBrowse"].(*widget.Button); !ok {
		return fmt.Errorf("%w: sshBrowse", ErrWidgetMissing)
	}
	if v.sshPassphraseInput, ok = named["sshPassphrase"].(*widget.TextInput); !ok {
		return fmt.Errorf("%w: sshPassphrase", ErrWidgetMissing)
	}
	if v.sshAddBtn, ok = named["sshAdd"].(*widget.Button); !ok {
		return fmt.Errorf("%w: sshAdd", ErrWidgetMissing)
	}
	if v.sshRemoveBtn, ok = named["sshRemove"].(*widget.Button); !ok {
		return fmt.Errorf("%w: sshRemove", ErrWidgetMissing)
	}
	if v.sshStatus, ok = named["sshStatus"].(*widget.Label); !ok {
		return fmt.Errorf("%w: sshStatus", ErrWidgetMissing)
	}
	if v.sshUnlockBtn, ok = named["sshUnlock"].(*widget.Button); !ok {
		return fmt.Errorf("%w: sshUnlock", ErrWidgetMissing)
	}
	return nil
}

func (v *View) buildSecretsColumns() {
	resourceCol := datagrid.NewTextColumn(i18n.T(columnHeaderResource), "Resource")
	resourceCol.SetWidth(datagrid.StarWidth(1))
	userCol := datagrid.NewTextColumn(i18n.T(columnHeaderUsername), "Username")
	userCol.SetWidth(datagrid.StarWidth(1))
	v.credentialsTable.Grid.SetColumns([]datagrid.Column{resourceCol, userCol})

	hostCol := datagrid.NewTextColumn(i18n.T(columnHeaderSSHHost), "Host")
	hostCol.SetWidth(datagrid.StarWidth(1))
	pathCol := datagrid.NewTextColumn(i18n.T(columnHeaderSSHKeyFile), "Path")
	pathCol.SetWidth(datagrid.StarWidth(2))
	v.sshTable.Grid.SetColumns([]datagrid.Column{hostCol, pathCol})
}

func (v *View) wireSecrets() {
	v.credentialsTable.Grid.OnSelectionChanged = v.onCredentialSelectionChanged
	v.credentialAddBtn.OnClick = v.onAddCredentialClicked
	v.credentialRemoveBtn.OnClick = v.onRemoveCredentialClicked
	v.masterPasswordBtn.OnClick = v.onSetMasterPasswordClicked
	v.credentialsUnlockBtn.OnClick = v.onUnlockSecretsClicked

	v.sshTable.Grid.OnSelectionChanged = v.onKeySelectionChanged
	v.sshAddBtn.OnClick = v.onAddKeyClicked
	v.sshRemoveBtn.OnClick = v.onRemoveKeyClicked
	v.sshBrowseBtn.OnClick = v.onBrowseKeyFileClicked
	v.sshUnlockBtn.OnClick = v.onUnlockSecretsClicked

	v.updateCredentialSelection("", false)
	v.updateKeySelection("", false)
}

func (v *View) SetCredentials(entries []SecretEntry) {
	v.credentials = append([]SecretEntry(nil), entries...)
	items := make([]interface{}, len(v.credentials))
	for i, e := range v.credentials {
		items[i] = e
	}
	v.credentialsTable.Grid.SetItemsSource(datagrid.NewObservableCollectionFrom(items))
	v.updateCredentialSelection("", false)
}

func (v *View) SetKeys(entries []KeyEntry) {
	v.keys = append([]KeyEntry(nil), entries...)
	items := make([]interface{}, len(v.keys))
	for i, e := range v.keys {
		items[i] = e
	}
	v.sshTable.Grid.SetItemsSource(datagrid.NewObservableCollectionFrom(items))
	v.updateKeySelection("", false)
}

func (v *View) SetSecretsStatus(text string, textColor color.RGBA) {
	v.credentialsStatus.SetText(text)
	v.credentialsStatus.TextColor = textColor
	v.sshStatus.SetText(text)
	v.sshStatus.TextColor = textColor
}

func (v *View) SetSecretsLocked(locked bool) {
	v.secretsLocked = locked
	enabled := !locked

	v.credentialsTable.SetEnabled(enabled)
	v.credentialResource.SetEnabled(enabled)
	v.credentialUsername.SetEnabled(enabled)
	v.credentialSecret.SetEnabled(enabled)
	v.credentialAddBtn.SetEnabled(enabled)
	v.masterPasswordBtn.SetEnabled(enabled)
	v.credentialRemoveBtn.SetEnabled(enabled && v.hasCredSelection)

	v.sshTable.SetEnabled(enabled)
	v.sshHostInput.SetEnabled(enabled)
	v.sshPathInput.SetEnabled(enabled)
	v.sshPassphraseInput.SetEnabled(enabled)
	v.sshAddBtn.SetEnabled(enabled)
	v.sshBrowseBtn.SetEnabled(enabled)
	v.sshRemoveBtn.SetEnabled(enabled && v.hasKeySelection)
}

func (v *View) SetKeyPath(path string) {
	v.sshPathInput.SetText(path)
}

func (v *View) onCredentialSelectionChanged(ev datagrid.SelectionChangedEvent) {
	entry, ok := ev.SelectedItem.(SecretEntry)
	if !ok {
		v.updateCredentialSelection("", false)
		return
	}
	v.credentialResource.SetText(entry.Resource)
	v.credentialUsername.SetText(entry.Username)
	v.updateCredentialSelection(entry.Resource, true)
}

func (v *View) updateCredentialSelection(resource string, has bool) {
	v.selectedResource = resource
	v.hasCredSelection = has
	v.credentialRemoveBtn.SetEnabled(has && !v.secretsLocked)
}

func (v *View) onKeySelectionChanged(ev datagrid.SelectionChangedEvent) {
	entry, ok := ev.SelectedItem.(KeyEntry)
	if !ok {
		v.updateKeySelection("", false)
		return
	}
	v.sshHostInput.SetText(entry.Host)
	v.sshPathInput.SetText(entry.Path)
	v.updateKeySelection(entry.Host, true)
}

func (v *View) updateKeySelection(host string, has bool) {
	v.selectedHost = host
	v.hasKeySelection = has
	v.sshRemoveBtn.SetEnabled(has && !v.secretsLocked)
}

func (v *View) onAddCredentialClicked() {
	resource := strings.TrimSpace(v.credentialResource.GetText())
	username := strings.TrimSpace(v.credentialUsername.GetText())
	text := v.credentialSecret.GetText()
	if resource == "" || text == "" {
		return
	}
	secret := []byte(text)
	v.credentialSecret.SetText("")
	if v.OnAddCredential != nil {
		v.OnAddCredential(resource, username, secret)
	}
	clear(secret)
}

func (v *View) onRemoveCredentialClicked() {
	if !v.hasCredSelection {
		return
	}
	if v.OnRemoveCredential != nil {
		v.OnRemoveCredential(v.selectedResource)
	}
}

func (v *View) onSetMasterPasswordClicked() {
	if v.OnSetMasterPassword != nil {
		v.OnSetMasterPassword()
	}
}

func (v *View) onUnlockSecretsClicked() {
	if v.OnUnlockSecrets != nil {
		v.OnUnlockSecrets()
	}
}

func (v *View) onAddKeyClicked() {
	host := strings.TrimSpace(v.sshHostInput.GetText())
	path := strings.TrimSpace(v.sshPathInput.GetText())
	text := v.sshPassphraseInput.GetText()
	if host == "" || path == "" {
		return
	}
	passphrase := []byte(text)
	v.sshPassphraseInput.SetText("")
	if v.OnAddKey != nil {
		v.OnAddKey(host, path, passphrase)
	}
	clear(passphrase)
}

func (v *View) onRemoveKeyClicked() {
	if !v.hasKeySelection {
		return
	}
	if v.OnRemoveKey != nil {
		v.OnRemoveKey(v.selectedHost)
	}
}

func (v *View) onBrowseKeyFileClicked() {
	if v.OnBrowseKeyFile != nil {
		v.OnBrowseKeyFile()
	}
}
