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
	Type     string
	Status   SecretStatus
}

type KeyEntry struct {
	Host   string
	Path   string
	Type   string
	Status SecretStatus
}

type CredentialHelperEntry struct {
	Name      string
	Supported bool
}

const defaultKeyHost = "*"

const authTypeColumnMinWidth = 130

const userColumnMinWidth = 110

const (
	columnHeaderResource   = "Dialog.Settings.Credentials.Resource"
	columnHeaderUsername   = "Dialog.Settings.Credentials.User"
	columnHeaderAuthType   = "Dialog.Settings.Credentials.Type"
	columnHeaderAuthStatus = "Dialog.Settings.Credentials.Status"
	columnHeaderSSHHost    = "Dialog.Settings.SSH.Host"
	columnHeaderSSHKeyFile = "Dialog.Settings.SSH.KeyFile"
	columnHeaderSSHType    = "Dialog.Settings.SSH.Type"
	columnHeaderSSHStatus  = "Dialog.Settings.SSH.Status"

	columnHeaderStorePath     = "Dialog.Settings.Secrets.StorePath"
	columnHeaderKeyProtection = "Dialog.Settings.Secrets.KeyProtection"
	columnHeaderHelpers       = "Dialog.Settings.Secrets.Helpers"
)

var credentialAuthTypeOrder = []string{"password", "token"}

func credentialAuthTypeIndex(value string) int {
	for i, t := range credentialAuthTypeOrder {
		if t == value {
			return i
		}
	}
	return 0
}

func credentialAuthTypeAt(idx int) string {
	if idx < 0 || idx >= len(credentialAuthTypeOrder) {
		return credentialAuthTypeOrder[0]
	}
	return credentialAuthTypeOrder[idx]
}

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
	if v.credentialType, ok = named["credentialType"].(*widget.Dropdown); !ok {
		return fmt.Errorf("%w: credentialType", ErrWidgetMissing)
	}
	if v.credentialSecret, ok = named["credentialSecret"].(*widget.TextInput); !ok {
		return fmt.Errorf("%w: credentialSecret", ErrWidgetMissing)
	}
	if v.credentialAddBtn, ok = named["credentialAdd"].(*widget.Button); !ok {
		return fmt.Errorf("%w: credentialAdd", ErrWidgetMissing)
	}
	if v.credentialSaveBtn, ok = named["credentialSave"].(*widget.Button); !ok {
		return fmt.Errorf("%w: credentialSave", ErrWidgetMissing)
	}
	if v.credentialEditBtn, ok = named["credentialEdit"].(*widget.Button); !ok {
		return fmt.Errorf("%w: credentialEdit", ErrWidgetMissing)
	}
	if v.credentialRemoveBtn, ok = named["credentialRemove"].(*widget.Button); !ok {
		return fmt.Errorf("%w: credentialRemove", ErrWidgetMissing)
	}
	if v.credentialTestBtn, ok = named["credentialTestConnection"].(*widget.Button); !ok {
		return fmt.Errorf("%w: credentialTestConnection", ErrWidgetMissing)
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
	if v.credentialsLockIcon, ok = named["credentialsLockIcon"].(*widget.ImageWidget); !ok {
		return fmt.Errorf("%w: credentialsLockIcon", ErrWidgetMissing)
	}
	if v.credentialsSecureRow, ok = named["credentialsSecureRow"].(*widget.Grid); !ok {
		return fmt.Errorf("%w: credentialsSecureRow", ErrWidgetMissing)
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
	if v.sshUseDefaultCheckBox, ok = named["sshUseDefault"].(*widget.CheckBox); !ok {
		return fmt.Errorf("%w: sshUseDefault", ErrWidgetMissing)
	}
	if v.sshAddBtn, ok = named["sshAdd"].(*widget.Button); !ok {
		return fmt.Errorf("%w: sshAdd", ErrWidgetMissing)
	}
	if v.sshSaveBtn, ok = named["sshSave"].(*widget.Button); !ok {
		return fmt.Errorf("%w: sshSave", ErrWidgetMissing)
	}
	if v.sshEditBtn, ok = named["sshEdit"].(*widget.Button); !ok {
		return fmt.Errorf("%w: sshEdit", ErrWidgetMissing)
	}
	if v.sshRemoveBtn, ok = named["sshRemove"].(*widget.Button); !ok {
		return fmt.Errorf("%w: sshRemove", ErrWidgetMissing)
	}
	if v.sshTestBtn, ok = named["sshTestConnection"].(*widget.Button); !ok {
		return fmt.Errorf("%w: sshTestConnection", ErrWidgetMissing)
	}
	if v.sshStatus, ok = named["sshStatus"].(*widget.Label); !ok {
		return fmt.Errorf("%w: sshStatus", ErrWidgetMissing)
	}
	if v.sshUnlockBtn, ok = named["sshUnlock"].(*widget.Button); !ok {
		return fmt.Errorf("%w: sshUnlock", ErrWidgetMissing)
	}
	if v.sshLockIcon, ok = named["sshLockIcon"].(*widget.ImageWidget); !ok {
		return fmt.Errorf("%w: sshLockIcon", ErrWidgetMissing)
	}
	if v.sshSecureRow, ok = named["sshSecureRow"].(*widget.Grid); !ok {
		return fmt.Errorf("%w: sshSecureRow", ErrWidgetMissing)
	}
	return nil
}

func (v *View) buildSecretsColumns() {
	emptyColor := widget.CurrentTheme().SecondaryText

	resourceCol := datagrid.NewTextColumn(i18n.T(columnHeaderResource), "Resource")
	resourceCol.SetWidth(datagrid.StarWidth(2))
	userCol := datagrid.NewTextColumn(i18n.T(columnHeaderUsername), "Username")
	userCol.SetWidth(datagrid.StarWidth(1))
	userCol.SetMinWidth(userColumnMinWidth)
	credentialTypeCol := datagrid.NewTextColumn(i18n.T(columnHeaderAuthType), "Type")
	credentialTypeCol.SetWidth(datagrid.StarWidth(1))
	credentialTypeCol.SetMinWidth(authTypeColumnMinWidth)
	credentialStatusCol := datagrid.NewTemplateColumn(i18n.T(columnHeaderAuthStatus), drawSecretStatusCell)
	credentialStatusCol.SetWidth(datagrid.StarWidth(1))
	credentialStatusCol.SetMinWidth(tableStatusColumnMinWidth)
	v.credentialsTable.Grid.SetColumns([]datagrid.Column{resourceCol, userCol, credentialTypeCol, credentialStatusCol})
	v.credentialsTable.Grid.EmptyStateText = i18n.T("Dialog.Settings.Credentials.Table.Empty")
	v.credentialsTable.Grid.EmptyStateColor = emptyColor

	hostCol := datagrid.NewTextColumn(i18n.T(columnHeaderSSHHost), "Host")
	hostCol.SetWidth(datagrid.StarWidth(2))
	pathCol := datagrid.NewTextColumn(i18n.T(columnHeaderSSHKeyFile), "Path")
	pathCol.SetWidth(datagrid.StarWidth(2))
	sshTypeCol := datagrid.NewTextColumn(i18n.T(columnHeaderSSHType), "Type")
	sshTypeCol.SetWidth(datagrid.StarWidth(1))
	sshStatusCol := datagrid.NewTemplateColumn(i18n.T(columnHeaderSSHStatus), drawSecretStatusCell)
	sshStatusCol.SetWidth(datagrid.StarWidth(1))
	sshStatusCol.SetMinWidth(tableStatusColumnMinWidth)
	v.sshTable.Grid.SetColumns([]datagrid.Column{hostCol, pathCol, sshTypeCol, sshStatusCol})
	v.sshTable.Grid.EmptyStateText = i18n.T("Dialog.Settings.SSH.Table.Empty")
	v.sshTable.Grid.EmptyStateColor = emptyColor
}

func (v *View) buildSecretNotes() {
	v.credentialsSecureNote = newSecureNote(v.credentialsLockIcon)
	v.credentialsSecureNote.SetGridProps(0, 1, 1, 1)
	v.credentialsSecureRow.AddChild(v.credentialsSecureNote)
	v.credentialsSecureRow.SetBounds(v.credentialsSecureRow.Bounds())

	v.sshSecureNote = newSecureNote(v.sshLockIcon)
	v.sshSecureNote.SetGridProps(0, 1, 1, 1)
	v.sshSecureRow.AddChild(v.sshSecureNote)
	v.sshSecureRow.SetBounds(v.sshSecureRow.Bounds())
}

func (v *View) wireSecrets() {
	v.credentialsTable.Grid.OnSelectionChanged = v.onCredentialSelectionChanged
	v.credentialAddBtn.OnClick = v.onNewCredentialClicked
	v.credentialEditBtn.OnClick = v.onEditCredentialClicked
	v.credentialSaveBtn.OnClick = v.onSaveCredentialClicked
	v.credentialRemoveBtn.OnClick = v.onRemoveCredentialClicked
	v.credentialTestBtn.OnClick = v.onTestCredentialConnectionClicked
	v.masterPasswordBtn.OnClick = v.onSetMasterPasswordClicked
	v.credentialsUnlockBtn.OnClick = v.onUnlockSecretsClicked

	v.sshTable.Grid.OnSelectionChanged = v.onKeySelectionChanged
	v.sshAddBtn.OnClick = v.onNewKeyClicked
	v.sshEditBtn.OnClick = v.onEditKeyClicked
	v.sshSaveBtn.OnClick = v.onSaveKeyClicked
	v.sshRemoveBtn.OnClick = v.onRemoveKeyClicked
	v.sshTestBtn.OnClick = v.onTestKeyConnectionClicked
	v.sshBrowseBtn.OnClick = v.onBrowseKeyFileClicked
	v.sshUnlockBtn.OnClick = v.onUnlockSecretsClicked
	v.sshUseDefaultCheckBox.OnChange = v.onSSHUseDefaultChanged

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
	v.credentialType.SetEnabled(enabled)
	v.credentialSecret.SetEnabled(enabled)
	v.credentialAddBtn.SetEnabled(enabled)
	v.credentialSaveBtn.SetEnabled(enabled)
	v.credentialTestBtn.SetEnabled(enabled)
	v.masterPasswordBtn.SetEnabled(enabled)
	v.credentialRemoveBtn.SetEnabled(enabled && v.hasCredSelection)
	v.credentialEditBtn.SetEnabled(enabled && v.hasCredSelection)

	v.sshTable.SetEnabled(enabled)
	v.sshHostInput.SetEnabled(enabled && !v.sshUseDefaultCheckBox.IsChecked())
	v.sshPathInput.SetEnabled(enabled)
	v.sshPassphraseInput.SetEnabled(enabled)
	v.sshUseDefaultCheckBox.SetEnabled(enabled)
	v.sshAddBtn.SetEnabled(enabled)
	v.sshSaveBtn.SetEnabled(enabled)
	v.sshTestBtn.SetEnabled(enabled)
	v.sshBrowseBtn.SetEnabled(enabled)
	v.sshRemoveBtn.SetEnabled(enabled && v.hasKeySelection)
	v.sshEditBtn.SetEnabled(enabled && v.hasKeySelection)
}

func (v *View) CredentialResource() string {
	return strings.TrimSpace(v.credentialResource.GetText())
}

func (v *View) SetCredentialResource(resource string) {
	v.credentialResource.SetText(resource)
}

func (v *View) SecretsStatus() string {
	return v.credentialsStatus.Text()
}

func (v *View) CredentialStoreLine() string {
	return v.credentialSourceStorePath.Text()
}

func (v *View) CredentialHelpersLine() string {
	return v.credentialSourceHelpers.Text()
}

func (v *View) KeyPath() string {
	return strings.TrimSpace(v.sshPathInput.GetText())
}

func (v *View) TakeKeyPassphrase() []byte {
	return v.sshPassphraseInput.TakeSecret()
}

func (v *View) SetKeyPath(path string) {
	v.sshPathInput.SetText(path)
}

func (v *View) SetCredentialSourceInfo(storePath, keyProtection string, helpers []CredentialHelperEntry) {
	v.credentialSourceStorePath.SetText(labeledInfoLine(columnHeaderStorePath, storePath))
	v.credentialSourceKeyProtection.SetText(labeledInfoLine(columnHeaderKeyProtection, keyProtection))
	v.credentialSourceHelpers.SetText(labeledInfoLine(columnHeaderHelpers, helperSummary(helpers)))
}

func labeledInfoLine(labelKey, value string) string {
	if value == "" {
		return ""
	}
	return i18n.T(labelKey) + " " + value
}

func helperSummary(helpers []CredentialHelperEntry) string {
	if helpers == nil {
		return ""
	}
	return formatCredentialHelpers(helpers)
}

func formatCredentialHelpers(helpers []CredentialHelperEntry) string {
	if len(helpers) == 0 {
		return i18n.T("Dialog.Settings.Secrets.HelpersNone")
	}
	labels := make([]string, len(helpers))
	for i, h := range helpers {
		if h.Supported {
			labels[i] = h.Name
			continue
		}
		labels[i] = i18n.Tf("Dialog.Settings.Secrets.HelperUnsupported", h.Name)
	}
	return strings.Join(labels, ", ")
}

func (v *View) onCredentialSelectionChanged(ev datagrid.SelectionChangedEvent) {
	entry, ok := ev.SelectedItem.(SecretEntry)
	if !ok {
		v.updateCredentialSelection("", false)
		return
	}
	v.credentialResource.SetText(entry.Resource)
	v.credentialUsername.SetText(entry.Username)
	v.credentialType.SetSelected(credentialAuthTypeIndex(entry.Type))
	v.updateCredentialSelection(entry.Resource, true)
}

func (v *View) updateCredentialSelection(resource string, has bool) {
	v.selectedResource = resource
	v.hasCredSelection = has
	v.credentialRemoveBtn.SetEnabled(has && !v.secretsLocked)
	v.credentialEditBtn.SetEnabled(has && !v.secretsLocked)
}

func (v *View) onKeySelectionChanged(ev datagrid.SelectionChangedEvent) {
	entry, ok := ev.SelectedItem.(KeyEntry)
	if !ok {
		v.updateKeySelection("", false)
		return
	}
	isDefault := entry.Host == defaultKeyHost
	v.sshUseDefaultCheckBox.SetChecked(isDefault)
	v.sshHostInput.SetEnabled(!isDefault && !v.secretsLocked)
	v.sshHostInput.SetText(entry.Host)
	v.sshPathInput.SetText(entry.Path)
	v.updateKeySelection(entry.Host, true)
}

func (v *View) updateKeySelection(host string, has bool) {
	v.selectedHost = host
	v.hasKeySelection = has
	v.sshRemoveBtn.SetEnabled(has && !v.secretsLocked)
	v.sshEditBtn.SetEnabled(has && !v.secretsLocked)
}

func (v *View) onNewCredentialClicked() {
	v.credentialsTable.Grid.SetSelectedIndex(-1)
	v.credentialResource.SetText("")
	v.credentialUsername.SetText("")
	v.credentialSecret.SetText("")
	v.credentialType.SetSelected(0)
	v.updateCredentialSelection("", false)
	v.credentialForm.SetExpanded(true)
	v.reportSecretsHint("Dialog.Settings.Secrets.Status.NewCredential")
}

func (v *View) onEditCredentialClicked() {
	entry, ok := v.credentialByResource(v.selectedResource)
	if !ok {
		v.reportSecretsProblem("Dialog.Settings.Secrets.Status.PickCredential")
		return
	}
	v.credentialResource.SetText(entry.Resource)
	v.credentialUsername.SetText(entry.Username)
	v.credentialType.SetSelected(credentialAuthTypeIndex(entry.Type))
	v.credentialSecret.SetText("")
	v.credentialForm.SetExpanded(true)
	v.reportSecretsHint("Dialog.Settings.Secrets.Status.EditCredential")
}

func (v *View) credentialByResource(resource string) (SecretEntry, bool) {
	for _, e := range v.credentials {
		if e.Resource == resource {
			return e, true
		}
	}
	return SecretEntry{}, false
}

func (v *View) onSaveCredentialClicked() {
	resource := strings.TrimSpace(v.credentialResource.GetText())
	username := strings.TrimSpace(v.credentialUsername.GetText())
	secret := v.credentialSecret.TakeSecret()
	defer clear(secret)
	if resource == "" {
		v.reportSecretsProblem("Dialog.Settings.Secrets.Status.NeedResource")
		return
	}
	if len(secret) == 0 {
		v.reportSecretsProblem("Dialog.Settings.Secrets.Status.NeedSecret")
		return
	}
	if v.OnAddCredential != nil {
		v.OnAddCredential(resource, username, secret)
	}
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

func (v *View) onTestCredentialConnectionClicked() {
	if v.OnTestConnection != nil {
		v.OnTestConnection("credentials")
	}
}

func (v *View) onTestKeyConnectionClicked() {
	if v.OnTestConnection != nil {
		v.OnTestConnection("ssh")
	}
}

func (v *View) onSSHUseDefaultChanged(checked bool) {
	v.sshHostInput.SetEnabled(!checked && !v.secretsLocked)
}

func (v *View) onNewKeyClicked() {
	v.sshTable.Grid.SetSelectedIndex(-1)
	v.sshUseDefaultCheckBox.SetChecked(false)
	v.sshHostInput.SetEnabled(!v.secretsLocked)
	v.sshHostInput.SetText("")
	v.sshPathInput.SetText("")
	v.sshPassphraseInput.SetText("")
	v.updateKeySelection("", false)
	v.sshForm.SetExpanded(true)
	v.reportSecretsHint("Dialog.Settings.Secrets.Status.NewKey")
}

func (v *View) onEditKeyClicked() {
	entry, ok := v.keyByHost(v.selectedHost)
	if !ok {
		v.reportSecretsProblem("Dialog.Settings.Secrets.Status.PickKey")
		return
	}
	isDefault := entry.Host == defaultKeyHost
	v.sshUseDefaultCheckBox.SetChecked(isDefault)
	v.sshHostInput.SetEnabled(!isDefault && !v.secretsLocked)
	v.sshHostInput.SetText(entry.Host)
	v.sshPathInput.SetText(entry.Path)
	v.sshPassphraseInput.SetText("")
	v.sshForm.SetExpanded(true)
	v.reportSecretsHint("Dialog.Settings.Secrets.Status.EditKey")
}

func (v *View) keyByHost(host string) (KeyEntry, bool) {
	for _, e := range v.keys {
		if e.Host == host {
			return e, true
		}
	}
	return KeyEntry{}, false
}

func (v *View) onSaveKeyClicked() {
	host := defaultKeyHost
	if !v.sshUseDefaultCheckBox.IsChecked() {
		host = strings.TrimSpace(v.sshHostInput.GetText())
	}
	path := strings.TrimSpace(v.sshPathInput.GetText())
	passphrase := v.sshPassphraseInput.TakeSecret()
	defer clear(passphrase)
	if host == "" {
		v.reportSecretsProblem("Dialog.Settings.Secrets.Status.NeedHost")
		return
	}
	if path == "" {
		v.reportSecretsProblem("Dialog.Settings.Secrets.Status.NeedKeyFile")
		return
	}
	if v.OnAddKey != nil {
		v.OnAddKey(host, path, passphrase)
	}
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

func (v *View) reportSecretsProblem(key string) {
	v.SetSecretsStatus(i18n.T(key), StatusError.DotColor(widget.CurrentTheme()))
}

func (v *View) reportSecretsHint(key string) {
	v.SetSecretsStatus(i18n.T(key), widget.CurrentTheme().SecondaryText)
}
