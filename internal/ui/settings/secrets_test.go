package settings

import (
	"image/color"
	"testing"

	"github.com/oops1/headless-gui/v3/widget/datagrid"

	"github.com/oops1/gogit/internal/i18n"
)

func TestBuildSecretsColumnsCreatesExpectedColumnCounts(t *testing.T) {
	v := newTestView(t, nil, Model{})
	if got := len(v.credentialsTable.Grid.Columns()); got != 4 {
		t.Fatalf("credentials columns = %d, want 4", got)
	}
	if got := len(v.sshTable.Grid.Columns()); got != 4 {
		t.Fatalf("ssh columns = %d, want 4", got)
	}
}

func TestBuildSecretsColumnsSetsMinWidthOnTheStatusColumn(t *testing.T) {
	v := newTestView(t, nil, Model{})
	credCols := v.credentialsTable.Grid.Columns()
	if got := credCols[len(credCols)-1].MinWidth(); got != tableStatusColumnMinWidth {
		t.Fatalf("credentials status column MinWidth = %d, want %d", got, tableStatusColumnMinWidth)
	}
	sshCols := v.sshTable.Grid.Columns()
	if got := sshCols[len(sshCols)-1].MinWidth(); got != tableStatusColumnMinWidth {
		t.Fatalf("ssh status column MinWidth = %d, want %d", got, tableStatusColumnMinWidth)
	}
}

func TestBuildSecretsColumnsSetsLocalizedEmptyStateText(t *testing.T) {
	v := newTestView(t, nil, Model{})
	if got := v.credentialsTable.Grid.EmptyStateText; got != i18n.T("Dialog.Settings.Credentials.Table.Empty") {
		t.Fatalf("credentials EmptyStateText = %q", got)
	}
	if got := v.sshTable.Grid.EmptyStateText; got != i18n.T("Dialog.Settings.SSH.Table.Empty") {
		t.Fatalf("ssh EmptyStateText = %q", got)
	}
}

func TestBuildSecretNotesSetsIconsAndSecondaryTextColor(t *testing.T) {
	v := newTestView(t, nil, Model{})
	if v.credentialsLockIcon.Image() == nil {
		t.Fatal("credentialsLockIcon must have an image")
	}
	if v.sshLockIcon.Image() == nil {
		t.Fatal("sshLockIcon must have an image")
	}
	want := i18n.T("Dialog.Settings.Secrets.SecureStorage")
	if v.credentialsSecureNote.Text() != want {
		t.Fatalf("credentialsSecureNote text = %q, want %q", v.credentialsSecureNote.Text(), want)
	}
	if v.sshSecureNote.Text() != want {
		t.Fatalf("sshSecureNote text = %q, want %q", v.sshSecureNote.Text(), want)
	}
}

func TestSetCredentialsPopulatesTableAndClearsSelection(t *testing.T) {
	v := newTestView(t, nil, Model{})
	v.updateCredentialSelection("example.com", true)

	entries := []SecretEntry{
		{Resource: "example.com", Username: "alice", Type: "password", Status: StatusSaved},
		{Resource: "example.org", Username: "bob", Type: "token", Status: StatusError},
	}
	v.SetCredentials(entries)

	if len(v.credentials) != 2 {
		t.Fatalf("credentials = %d, want 2", len(v.credentials))
	}
	if v.hasCredSelection {
		t.Fatal("selection must be cleared after SetCredentials")
	}
	if v.credentialRemoveBtn.IsEnabled() || v.credentialEditBtn.IsEnabled() {
		t.Fatal("edit and remove buttons must be disabled without a selection")
	}
}

func TestSetKeysPopulatesTableAndClearsSelection(t *testing.T) {
	v := newTestView(t, nil, Model{})
	v.updateKeySelection("example.com", true)

	v.SetKeys([]KeyEntry{{Host: "example.com", Path: "/home/user/.ssh/id_ed25519", Type: "ed25519", Status: StatusSaved}})

	if len(v.keys) != 1 {
		t.Fatalf("keys = %d, want 1", len(v.keys))
	}
	if v.hasKeySelection {
		t.Fatal("selection must be cleared after SetKeys")
	}
	if v.sshRemoveBtn.IsEnabled() || v.sshEditBtn.IsEnabled() {
		t.Fatal("edit and remove buttons must be disabled without a selection")
	}
}

func TestSetSecretsStatusUpdatesBothStatusLabels(t *testing.T) {
	v := newTestView(t, nil, Model{})
	want := color.RGBA{R: 220, G: 80, B: 80, A: 255}
	v.SetSecretsStatus("locked", want)
	if v.credentialsStatus.Text() != "locked" {
		t.Fatalf("credentialsStatus = %q", v.credentialsStatus.Text())
	}
	if v.sshStatus.Text() != "locked" {
		t.Fatalf("sshStatus = %q", v.sshStatus.Text())
	}
	if v.credentialsStatus.TextColor != want {
		t.Fatalf("credentialsStatus color = %+v, want %+v", v.credentialsStatus.TextColor, want)
	}
	if v.sshStatus.TextColor != want {
		t.Fatalf("sshStatus color = %+v, want %+v", v.sshStatus.TextColor, want)
	}
}

func TestSetSecretsLockedDisablesEverythingExceptUnlockButtons(t *testing.T) {
	v := newTestView(t, nil, Model{})
	v.SetSecretsLocked(true)

	for name, w := range map[string]interface{ IsEnabled() bool }{
		"credentialsTable":      v.credentialsTable,
		"credentialResource":    v.credentialResource,
		"credentialUsername":    v.credentialUsername,
		"credentialType":        v.credentialType,
		"credentialSecret":      v.credentialSecret,
		"credentialAddBtn":      v.credentialAddBtn,
		"credentialTestBtn":     v.credentialTestBtn,
		"masterPasswordBtn":     v.masterPasswordBtn,
		"credentialRemoveBtn":   v.credentialRemoveBtn,
		"credentialEditBtn":     v.credentialEditBtn,
		"sshTable":              v.sshTable,
		"sshHostInput":          v.sshHostInput,
		"sshPathInput":          v.sshPathInput,
		"sshPassphraseInput":    v.sshPassphraseInput,
		"sshUseDefaultCheckBox": v.sshUseDefaultCheckBox,
		"sshAddBtn":             v.sshAddBtn,
		"sshEditBtn":            v.sshEditBtn,
		"sshTestBtn":            v.sshTestBtn,
		"sshBrowseBtn":          v.sshBrowseBtn,
		"sshRemoveBtn":          v.sshRemoveBtn,
	} {
		if w.IsEnabled() {
			t.Fatalf("%s must be disabled when locked", name)
		}
	}
	if !v.credentialsUnlockBtn.IsEnabled() || !v.sshUnlockBtn.IsEnabled() {
		t.Fatal("unlock buttons must stay enabled when locked")
	}

	v.SetSecretsLocked(false)
	if !v.credentialAddBtn.IsEnabled() || !v.sshAddBtn.IsEnabled() {
		t.Fatal("add buttons must be re-enabled when unlocked")
	}
	if v.credentialRemoveBtn.IsEnabled() || v.sshRemoveBtn.IsEnabled() {
		t.Fatal("remove buttons must stay disabled without a selection even when unlocked")
	}
	if v.credentialEditBtn.IsEnabled() || v.sshEditBtn.IsEnabled() {
		t.Fatal("edit buttons must stay disabled without a selection even when unlocked")
	}
}

func TestSetSecretsLockedKeepsTheSSHHostFieldDisabledWhenUsingTheDefaultKey(t *testing.T) {
	v := newTestView(t, nil, Model{})
	v.sshUseDefaultCheckBox.SetChecked(true)
	v.SetSecretsLocked(false)
	if v.sshHostInput.IsEnabled() {
		t.Fatal("host field must stay disabled while the default-key checkbox is checked")
	}
}

func TestCredentialSelectionChangedFillsFieldsAndEnablesEditAndRemove(t *testing.T) {
	v := newTestView(t, nil, Model{})
	v.SetCredentials([]SecretEntry{{Resource: "example.com", Username: "alice", Type: "token"}})

	v.onCredentialSelectionChanged(datagrid.SelectionChangedEvent{SelectedIndex: 0, SelectedItem: v.credentials[0]})

	if v.credentialResource.GetText() != "example.com" {
		t.Fatalf("resource = %q", v.credentialResource.GetText())
	}
	if v.credentialUsername.GetText() != "alice" {
		t.Fatalf("username = %q", v.credentialUsername.GetText())
	}
	if v.credentialType.Selected() != credentialAuthTypeIndex("token") {
		t.Fatalf("type selection = %d, want %d", v.credentialType.Selected(), credentialAuthTypeIndex("token"))
	}
	if !v.credentialRemoveBtn.IsEnabled() || !v.credentialEditBtn.IsEnabled() {
		t.Fatal("edit and remove buttons must be enabled after selecting a row")
	}

	v.onCredentialSelectionChanged(datagrid.SelectionChangedEvent{SelectedIndex: -1, SelectedItem: nil})
	if v.hasCredSelection || v.credentialRemoveBtn.IsEnabled() || v.credentialEditBtn.IsEnabled() {
		t.Fatal("clearing the selection must disable edit and remove")
	}
}

func TestKeySelectionChangedFillsFieldsAndEnablesEditAndRemove(t *testing.T) {
	v := newTestView(t, nil, Model{})
	v.SetKeys([]KeyEntry{{Host: "example.com", Path: "/keys/id_ed25519"}})

	v.onKeySelectionChanged(datagrid.SelectionChangedEvent{SelectedIndex: 0, SelectedItem: v.keys[0]})

	if v.sshHostInput.GetText() != "example.com" {
		t.Fatalf("host = %q", v.sshHostInput.GetText())
	}
	if v.sshPathInput.GetText() != "/keys/id_ed25519" {
		t.Fatalf("path = %q", v.sshPathInput.GetText())
	}
	if v.sshUseDefaultCheckBox.IsChecked() {
		t.Fatal("use-default checkbox must be unchecked for a non-default host")
	}
	if !v.sshHostInput.IsEnabled() {
		t.Fatal("host field must stay enabled for a non-default host")
	}
	if !v.sshRemoveBtn.IsEnabled() || !v.sshEditBtn.IsEnabled() {
		t.Fatal("edit and remove buttons must be enabled after selecting a row")
	}

	v.onKeySelectionChanged(datagrid.SelectionChangedEvent{SelectedIndex: -1, SelectedItem: nil})
	if v.hasKeySelection || v.sshRemoveBtn.IsEnabled() || v.sshEditBtn.IsEnabled() {
		t.Fatal("clearing the selection must disable edit and remove")
	}
}

func TestKeySelectionChangedOnADefaultHostChecksTheBoxAndDisablesHost(t *testing.T) {
	v := newTestView(t, nil, Model{})
	v.SetKeys([]KeyEntry{{Host: defaultKeyHost, Path: "/keys/id_ed25519"}})

	v.onKeySelectionChanged(datagrid.SelectionChangedEvent{SelectedIndex: 0, SelectedItem: v.keys[0]})

	if !v.sshUseDefaultCheckBox.IsChecked() {
		t.Fatal("use-default checkbox must be checked for the default host")
	}
	if v.sshHostInput.IsEnabled() {
		t.Fatal("host field must be disabled while showing the default host")
	}
}

func TestCredentialSelectionChangedWhileLockedKeepsEditAndRemoveDisabled(t *testing.T) {
	v := newTestView(t, nil, Model{})
	v.SetCredentials([]SecretEntry{{Resource: "example.com", Username: "alice"}})
	v.SetSecretsLocked(true)

	v.onCredentialSelectionChanged(datagrid.SelectionChangedEvent{SelectedIndex: 0, SelectedItem: v.credentials[0]})

	if v.credentialRemoveBtn.IsEnabled() || v.credentialEditBtn.IsEnabled() {
		t.Fatal("edit and remove buttons must stay disabled while the store is locked, even after selecting a row")
	}
}

func TestKeySelectionChangedWhileLockedKeepsEditAndRemoveDisabled(t *testing.T) {
	v := newTestView(t, nil, Model{})
	v.SetKeys([]KeyEntry{{Host: "example.com", Path: "/keys/id_ed25519"}})
	v.SetSecretsLocked(true)

	v.onKeySelectionChanged(datagrid.SelectionChangedEvent{SelectedIndex: 0, SelectedItem: v.keys[0]})

	if v.sshRemoveBtn.IsEnabled() || v.sshEditBtn.IsEnabled() {
		t.Fatal("edit and remove buttons must stay disabled while the store is locked, even after selecting a row")
	}
}

func TestAddCredentialClickedCallsCallbackAndWipesTheSecretAfterwards(t *testing.T) {
	v := newTestView(t, nil, Model{})
	v.credentialResource.SetText("example.com")
	v.credentialUsername.SetText("alice")
	v.credentialSecret.SetText("s3cr3t")

	var gotResource, gotUsername string
	var gotSecret []byte
	var secretDuringCall []byte
	v.OnAddCredential = func(resource, username string, secret []byte) {
		gotResource, gotUsername = resource, username
		gotSecret = secret
		secretDuringCall = append([]byte(nil), secret...)
	}

	v.onAddCredentialClicked()

	if gotResource != "example.com" || gotUsername != "alice" {
		t.Fatalf("resource/username = %q/%q", gotResource, gotUsername)
	}
	if string(secretDuringCall) != "s3cr3t" {
		t.Fatalf("secret seen during callback = %q, want %q", secretDuringCall, "s3cr3t")
	}
	if v.credentialSecret.GetText() != "" {
		t.Fatal("secret input must be cleared after Add")
	}
	for _, b := range gotSecret {
		if b != 0 {
			t.Fatal("secret bytes must be wiped after the callback returns")
		}
	}
}

func TestAddCredentialClickedDoesNothingWithoutResourceOrSecret(t *testing.T) {
	v := newTestView(t, nil, Model{})
	called := false
	v.OnAddCredential = func(string, string, []byte) { called = true }

	v.credentialResource.SetText("")
	v.credentialSecret.SetText("s3cr3t")
	v.onAddCredentialClicked()

	v.credentialResource.SetText("example.com")
	v.credentialSecret.SetText("")
	v.onAddCredentialClicked()

	if called {
		t.Fatal("OnAddCredential must not be called without both a resource and a secret")
	}
}

func TestEditCredentialButtonUsesTheSameUpsertAsAdd(t *testing.T) {
	v := newTestView(t, nil, Model{})
	v.SetCredentials([]SecretEntry{{Resource: "example.com", Username: "alice"}})
	v.onCredentialSelectionChanged(datagrid.SelectionChangedEvent{SelectedIndex: 0, SelectedItem: v.credentials[0]})
	v.credentialUsername.SetText("alice2")
	v.credentialSecret.SetText("newsecret")

	var gotUsername string
	v.OnAddCredential = func(resource, username string, secret []byte) { gotUsername = username }

	v.credentialEditBtn.OnClick()

	if gotUsername != "alice2" {
		t.Fatalf("username = %q, want alice2", gotUsername)
	}
}

func TestRemoveCredentialClickedRequiresASelection(t *testing.T) {
	v := newTestView(t, nil, Model{})
	called := false
	v.OnRemoveCredential = func(string) { called = true }

	v.onRemoveCredentialClicked()
	if called {
		t.Fatal("OnRemoveCredential must not fire without a selection")
	}

	v.SetCredentials([]SecretEntry{{Resource: "example.com", Username: "alice"}})
	v.onCredentialSelectionChanged(datagrid.SelectionChangedEvent{SelectedIndex: 0, SelectedItem: v.credentials[0]})

	var gotResource string
	v.OnRemoveCredential = func(resource string) { gotResource = resource }
	v.onRemoveCredentialClicked()
	if gotResource != "example.com" {
		t.Fatalf("removed resource = %q", gotResource)
	}
}

func TestAddKeyClickedCallsCallbackAndWipesThePassphraseAfterwards(t *testing.T) {
	v := newTestView(t, nil, Model{})
	v.sshHostInput.SetText("example.com")
	v.sshPathInput.SetText("/keys/id_ed25519")
	v.sshPassphraseInput.SetText("p4ss")

	var gotHost, gotPath string
	var gotPassphrase []byte
	var seenDuringCall string
	v.OnAddKey = func(host, path string, passphrase []byte) {
		gotHost, gotPath = host, path
		gotPassphrase = passphrase
		seenDuringCall = string(passphrase)
	}

	v.onAddKeyClicked()

	if gotHost != "example.com" || gotPath != "/keys/id_ed25519" {
		t.Fatalf("host/path = %q/%q", gotHost, gotPath)
	}
	if seenDuringCall != "p4ss" {
		t.Fatalf("passphrase seen during callback = %q, want %q", seenDuringCall, "p4ss")
	}
	if v.sshPassphraseInput.GetText() != "" {
		t.Fatal("passphrase input must be cleared after Add")
	}
	for _, b := range gotPassphrase {
		if b != 0 {
			t.Fatal("passphrase bytes must be wiped after the callback returns")
		}
	}
}

func TestAddKeyClickedUsesTheDefaultHostWhenTheCheckboxIsChecked(t *testing.T) {
	v := newTestView(t, nil, Model{})
	v.sshHostInput.SetText("ignored.example.com")
	v.sshUseDefaultCheckBox.SetChecked(true)
	v.sshPathInput.SetText("/keys/id_ed25519")

	var gotHost string
	v.OnAddKey = func(host, path string, passphrase []byte) { gotHost = host }
	v.onAddKeyClicked()

	if gotHost != defaultKeyHost {
		t.Fatalf("host = %q, want %q", gotHost, defaultKeyHost)
	}
}

func TestAddKeyClickedDoesNothingWithoutHostOrPath(t *testing.T) {
	v := newTestView(t, nil, Model{})
	called := false
	v.OnAddKey = func(string, string, []byte) { called = true }

	v.sshHostInput.SetText("")
	v.sshPathInput.SetText("/keys/id_ed25519")
	v.onAddKeyClicked()

	v.sshHostInput.SetText("example.com")
	v.sshPathInput.SetText("")
	v.onAddKeyClicked()

	if called {
		t.Fatal("OnAddKey must not be called without both a host and a path")
	}
}

func TestEditKeyButtonUsesTheSameUpsertAsAdd(t *testing.T) {
	v := newTestView(t, nil, Model{})
	v.SetKeys([]KeyEntry{{Host: "example.com", Path: "/keys/id_ed25519"}})
	v.onKeySelectionChanged(datagrid.SelectionChangedEvent{SelectedIndex: 0, SelectedItem: v.keys[0]})
	v.sshPathInput.SetText("/keys/id_ed25519_new")
	v.sshPassphraseInput.SetText("p4ss")

	var gotPath string
	v.OnAddKey = func(host, path string, passphrase []byte) { gotPath = path }
	v.sshEditBtn.OnClick()

	if gotPath != "/keys/id_ed25519_new" {
		t.Fatalf("path = %q, want /keys/id_ed25519_new", gotPath)
	}
}

func TestSSHUseDefaultCheckboxTogglesTheHostFieldEnabledState(t *testing.T) {
	v := newTestView(t, nil, Model{})
	v.onSSHUseDefaultChanged(true)
	if v.sshHostInput.IsEnabled() {
		t.Fatal("host field must be disabled once the checkbox is checked")
	}
	v.onSSHUseDefaultChanged(false)
	if !v.sshHostInput.IsEnabled() {
		t.Fatal("host field must be re-enabled once the checkbox is unchecked")
	}
}

func TestSSHUseDefaultCheckboxRespectsTheLockedState(t *testing.T) {
	v := newTestView(t, nil, Model{})
	v.SetSecretsLocked(true)
	v.onSSHUseDefaultChanged(false)
	if v.sshHostInput.IsEnabled() {
		t.Fatal("host field must stay disabled while the store is locked")
	}
}

func TestRemoveKeyClickedRequiresASelection(t *testing.T) {
	v := newTestView(t, nil, Model{})
	called := false
	v.OnRemoveKey = func(string) { called = true }

	v.onRemoveKeyClicked()
	if called {
		t.Fatal("OnRemoveKey must not fire without a selection")
	}

	v.SetKeys([]KeyEntry{{Host: "example.com", Path: "/keys/id_ed25519"}})
	v.onKeySelectionChanged(datagrid.SelectionChangedEvent{SelectedIndex: 0, SelectedItem: v.keys[0]})

	var gotHost string
	v.OnRemoveKey = func(host string) { gotHost = host }
	v.onRemoveKeyClicked()
	if gotHost != "example.com" {
		t.Fatalf("removed host = %q", gotHost)
	}
}

func TestSetMasterPasswordClickedInvokesCallback(t *testing.T) {
	v := newTestView(t, nil, Model{})
	called := false
	v.OnSetMasterPassword = func() { called = true }
	v.onSetMasterPasswordClicked()
	if !called {
		t.Fatal("OnSetMasterPassword must be called")
	}
}

func TestUnlockSecretsClickedInvokesCallback(t *testing.T) {
	v := newTestView(t, nil, Model{})
	called := false
	v.OnUnlockSecrets = func() { called = true }
	v.onUnlockSecretsClicked()
	if !called {
		t.Fatal("OnUnlockSecrets must be called")
	}
}

func TestBrowseKeyFileClickedInvokesCallback(t *testing.T) {
	v := newTestView(t, nil, Model{})
	called := false
	v.OnBrowseKeyFile = func() { called = true }
	v.onBrowseKeyFileClicked()
	if !called {
		t.Fatal("OnBrowseKeyFile must be called")
	}
}

func TestTestConnectionButtonsReportTheirSection(t *testing.T) {
	v := newTestView(t, nil, Model{})
	var gotSections []string
	v.OnTestConnection = func(section string) { gotSections = append(gotSections, section) }

	v.credentialTestBtn.OnClick()
	v.sshTestBtn.OnClick()

	if len(gotSections) != 2 || gotSections[0] != "credentials" || gotSections[1] != "ssh" {
		t.Fatalf("sections reported = %v, want [credentials ssh]", gotSections)
	}
}

func TestTestConnectionClickedToleratesNilCallback(t *testing.T) {
	v := newTestView(t, nil, Model{})
	v.credentialTestBtn.OnClick()
	v.sshTestBtn.OnClick()
}

func TestSetCredentialSourceInfoFillsTheInfoLabels(t *testing.T) {
	v := newTestView(t, nil, Model{})
	v.SetCredentialSourceInfo("C:\\Users\\alice\\.gogit\\vault.bin", "Master password", []CredentialHelperEntry{
		{Name: "manager-core", Supported: true},
		{Name: "!some-shell-script", Supported: false},
	})

	wantStorePath := i18n.T("Dialog.Settings.Secrets.StorePath") + " C:\\Users\\alice\\.gogit\\vault.bin"
	if v.credentialSourceStorePath.Text() != wantStorePath {
		t.Fatalf("storePath = %q, want %q", v.credentialSourceStorePath.Text(), wantStorePath)
	}
	wantKeyProtection := i18n.T("Dialog.Settings.Secrets.KeyProtection") + " Master password"
	if v.credentialSourceKeyProtection.Text() != wantKeyProtection {
		t.Fatalf("keyProtection = %q, want %q", v.credentialSourceKeyProtection.Text(), wantKeyProtection)
	}
	want := i18n.T("Dialog.Settings.Secrets.Helpers") + " manager-core, " +
		i18n.Tf("Dialog.Settings.Secrets.HelperUnsupported", "!some-shell-script")
	if v.credentialSourceHelpers.Text() != want {
		t.Fatalf("helpers = %q, want %q", v.credentialSourceHelpers.Text(), want)
	}
}

func TestSetCredentialSourceInfoWithNoHelpersShowsNone(t *testing.T) {
	v := newTestView(t, nil, Model{})
	v.SetCredentialSourceInfo("", "", nil)

	want := i18n.T("Dialog.Settings.Secrets.Helpers") + " " + i18n.T("Dialog.Settings.Secrets.HelpersNone")
	if v.credentialSourceHelpers.Text() != want {
		t.Fatalf("helpers = %q, want the empty-list placeholder %q", v.credentialSourceHelpers.Text(), want)
	}
}

func TestSetKeyPathFillsThePathInput(t *testing.T) {
	v := newTestView(t, nil, Model{})
	v.SetKeyPath("/keys/id_ed25519")
	if v.sshPathInput.GetText() != "/keys/id_ed25519" {
		t.Fatalf("path = %q", v.sshPathInput.GetText())
	}
}

func TestClickingAddAndRemoveButtonsCallTheHandlers(t *testing.T) {
	v := newTestView(t, nil, Model{})
	v.credentialResource.SetText("example.com")
	v.credentialSecret.SetText("s3cr3t")
	addCalled := false
	v.OnAddCredential = func(string, string, []byte) { addCalled = true }
	v.credentialAddBtn.OnClick()
	if !addCalled {
		t.Fatal("clicking Add must call OnAddCredential")
	}

	v.sshHostInput.SetText("example.com")
	v.sshPathInput.SetText("/keys/id_ed25519")
	sshAddCalled := false
	v.OnAddKey = func(string, string, []byte) { sshAddCalled = true }
	v.sshAddBtn.OnClick()
	if !sshAddCalled {
		t.Fatal("clicking Add must call OnAddKey")
	}

	masterCalled := false
	v.OnSetMasterPassword = func() { masterCalled = true }
	v.masterPasswordBtn.OnClick()
	if !masterCalled {
		t.Fatal("clicking Set Master Password must call OnSetMasterPassword")
	}

	unlockCalled := false
	v.OnUnlockSecrets = func() { unlockCalled = true }
	v.credentialsUnlockBtn.OnClick()
	v.sshUnlockBtn.OnClick()
	if !unlockCalled {
		t.Fatal("clicking Unlock must call OnUnlockSecrets")
	}

	browseCalled := false
	v.OnBrowseKeyFile = func() { browseCalled = true }
	v.sshBrowseBtn.OnClick()
	if !browseCalled {
		t.Fatal("clicking Browse must call OnBrowseKeyFile")
	}
}

func TestCredentialAuthTypeIndexAndAtRoundTripKnownValues(t *testing.T) {
	for i, value := range credentialAuthTypeOrder {
		if credentialAuthTypeIndex(value) != i {
			t.Fatalf("credentialAuthTypeIndex(%q) = %d, want %d", value, credentialAuthTypeIndex(value), i)
		}
		if credentialAuthTypeAt(i) != value {
			t.Fatalf("credentialAuthTypeAt(%d) = %q, want %q", i, credentialAuthTypeAt(i), value)
		}
	}
}

func TestCredentialAuthTypeIndexFallsBackToZeroForUnknownValue(t *testing.T) {
	if credentialAuthTypeIndex("bogus") != 0 {
		t.Fatal("unknown auth type must map to index 0")
	}
}

func TestCredentialAuthTypeAtFallsBackToFirstForOutOfRangeIndex(t *testing.T) {
	if credentialAuthTypeAt(-1) != credentialAuthTypeOrder[0] {
		t.Fatal("negative index must fall back to the first auth type")
	}
	if credentialAuthTypeAt(len(credentialAuthTypeOrder)) != credentialAuthTypeOrder[0] {
		t.Fatal("index past the end must fall back to the first auth type")
	}
}
