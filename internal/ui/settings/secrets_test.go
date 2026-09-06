package settings

import (
	"testing"

	"github.com/oops1/headless-gui/v3/widget/datagrid"
)

func TestBuildSecretsColumnsCreatesExpectedColumnCounts(t *testing.T) {
	v := newTestView(t, nil, Model{})
	if got := len(v.credentialsTable.Grid.Columns()); got != 2 {
		t.Fatalf("credentials columns = %d, want 2", got)
	}
	if got := len(v.sshTable.Grid.Columns()); got != 2 {
		t.Fatalf("ssh columns = %d, want 2", got)
	}
}

func TestSetCredentialsPopulatesTableAndClearsSelection(t *testing.T) {
	v := newTestView(t, nil, Model{})
	v.updateCredentialSelection("example.com", true)

	entries := []SecretEntry{{Resource: "example.com", Username: "alice"}, {Resource: "example.org", Username: "bob"}}
	v.SetCredentials(entries)

	if len(v.credentials) != 2 {
		t.Fatalf("credentials = %d, want 2", len(v.credentials))
	}
	if v.hasCredSelection {
		t.Fatal("selection must be cleared after SetCredentials")
	}
	if v.credentialRemoveBtn.IsEnabled() {
		t.Fatal("remove button must be disabled without a selection")
	}
}

func TestSetKeysPopulatesTableAndClearsSelection(t *testing.T) {
	v := newTestView(t, nil, Model{})
	v.updateKeySelection("example.com", true)

	v.SetKeys([]KeyEntry{{Host: "example.com", Path: "/home/user/.ssh/id_ed25519"}})

	if len(v.keys) != 1 {
		t.Fatalf("keys = %d, want 1", len(v.keys))
	}
	if v.hasKeySelection {
		t.Fatal("selection must be cleared after SetKeys")
	}
	if v.sshRemoveBtn.IsEnabled() {
		t.Fatal("remove button must be disabled without a selection")
	}
}

func TestSetSecretsStatusUpdatesBothStatusLabels(t *testing.T) {
	v := newTestView(t, nil, Model{})
	v.SetSecretsStatus("locked")
	if v.credentialsStatus.Text() != "locked" {
		t.Fatalf("credentialsStatus = %q", v.credentialsStatus.Text())
	}
	if v.sshStatus.Text() != "locked" {
		t.Fatalf("sshStatus = %q", v.sshStatus.Text())
	}
}

func TestSetSecretsLockedDisablesEverythingExceptUnlockButtons(t *testing.T) {
	v := newTestView(t, nil, Model{})
	v.SetSecretsLocked(true)

	for name, w := range map[string]interface{ IsEnabled() bool }{
		"credentialsTable":    v.credentialsTable,
		"credentialResource":  v.credentialResource,
		"credentialUsername":  v.credentialUsername,
		"credentialSecret":    v.credentialSecret,
		"credentialAddBtn":    v.credentialAddBtn,
		"masterPasswordBtn":   v.masterPasswordBtn,
		"credentialRemoveBtn": v.credentialRemoveBtn,
		"sshTable":            v.sshTable,
		"sshHostInput":        v.sshHostInput,
		"sshPathInput":        v.sshPathInput,
		"sshPassphraseInput":  v.sshPassphraseInput,
		"sshAddBtn":           v.sshAddBtn,
		"sshBrowseBtn":        v.sshBrowseBtn,
		"sshRemoveBtn":        v.sshRemoveBtn,
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
}

func TestCredentialSelectionChangedFillsFieldsAndEnablesRemove(t *testing.T) {
	v := newTestView(t, nil, Model{})
	v.SetCredentials([]SecretEntry{{Resource: "example.com", Username: "alice"}})

	v.onCredentialSelectionChanged(datagrid.SelectionChangedEvent{SelectedIndex: 0, SelectedItem: v.credentials[0]})

	if v.credentialResource.GetText() != "example.com" {
		t.Fatalf("resource = %q", v.credentialResource.GetText())
	}
	if v.credentialUsername.GetText() != "alice" {
		t.Fatalf("username = %q", v.credentialUsername.GetText())
	}
	if !v.credentialRemoveBtn.IsEnabled() {
		t.Fatal("remove button must be enabled after selecting a row")
	}

	v.onCredentialSelectionChanged(datagrid.SelectionChangedEvent{SelectedIndex: -1, SelectedItem: nil})
	if v.hasCredSelection || v.credentialRemoveBtn.IsEnabled() {
		t.Fatal("clearing the selection must disable remove")
	}
}

func TestKeySelectionChangedFillsFieldsAndEnablesRemove(t *testing.T) {
	v := newTestView(t, nil, Model{})
	v.SetKeys([]KeyEntry{{Host: "example.com", Path: "/keys/id_ed25519"}})

	v.onKeySelectionChanged(datagrid.SelectionChangedEvent{SelectedIndex: 0, SelectedItem: v.keys[0]})

	if v.sshHostInput.GetText() != "example.com" {
		t.Fatalf("host = %q", v.sshHostInput.GetText())
	}
	if v.sshPathInput.GetText() != "/keys/id_ed25519" {
		t.Fatalf("path = %q", v.sshPathInput.GetText())
	}
	if !v.sshRemoveBtn.IsEnabled() {
		t.Fatal("remove button must be enabled after selecting a row")
	}

	v.onKeySelectionChanged(datagrid.SelectionChangedEvent{SelectedIndex: -1, SelectedItem: nil})
	if v.hasKeySelection || v.sshRemoveBtn.IsEnabled() {
		t.Fatal("clearing the selection must disable remove")
	}
}

func TestCredentialSelectionChangedWhileLockedKeepsRemoveDisabled(t *testing.T) {
	v := newTestView(t, nil, Model{})
	v.SetCredentials([]SecretEntry{{Resource: "example.com", Username: "alice"}})
	v.SetSecretsLocked(true)

	v.onCredentialSelectionChanged(datagrid.SelectionChangedEvent{SelectedIndex: 0, SelectedItem: v.credentials[0]})

	if v.credentialRemoveBtn.IsEnabled() {
		t.Fatal("remove button must stay disabled while the store is locked, even after selecting a row")
	}
}

func TestKeySelectionChangedWhileLockedKeepsRemoveDisabled(t *testing.T) {
	v := newTestView(t, nil, Model{})
	v.SetKeys([]KeyEntry{{Host: "example.com", Path: "/keys/id_ed25519"}})
	v.SetSecretsLocked(true)

	v.onKeySelectionChanged(datagrid.SelectionChangedEvent{SelectedIndex: 0, SelectedItem: v.keys[0]})

	if v.sshRemoveBtn.IsEnabled() {
		t.Fatal("remove button must stay disabled while the store is locked, even after selecting a row")
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
