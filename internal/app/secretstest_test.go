package app

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/oops1/gogit/internal/config"
	"github.com/oops1/gogit/internal/gitcore/transport"
	"github.com/oops1/gogit/internal/i18n"
)

func stubLsRemote(t *testing.T, refs []transport.Ref, err error) *string {
	t.Helper()
	var seen string
	prev := lsRemoteRefs
	lsRemoteRefs = func(_ context.Context, url string, _ transport.Options) ([]transport.Ref, error) {
		seen = url
		return refs, err
	}
	t.Cleanup(func() { lsRemoteRefs = prev })
	return &seen
}

func TestTestConnectionRefusesToRunWithoutAResource(t *testing.T) {
	a := newTestApp(t)
	view := newSecretsTestView(t, a)

	a.testSecretsConnection(view, "credentials")
	waitForPostQueueDrain(t, a)

	if got := view.SecretsStatus(); got != i18n.T("Dialog.Settings.Secrets.Status.NeedResource") {
		t.Fatalf("status = %q, want the missing-resource message", got)
	}
}

func TestTestConnectionReportsTheNumberOfRemoteRefs(t *testing.T) {
	a := newTestApp(t)
	view := newSecretsTestView(t, a)
	seen := stubLsRemote(t, []transport.Ref{{Name: "refs/heads/main"}, {Name: "refs/heads/next"}}, nil)
	view.SetCredentialResource("https://example.com/repo.git")

	a.testSecretsConnection(view, "credentials")
	secretsWG.Wait()
	waitForPostQueueDrain(t, a)

	if *seen != "https://example.com/repo.git" {
		t.Fatalf("checked url = %q", *seen)
	}
	if got := view.SecretsStatus(); got != i18n.Tf("Dialog.Settings.Secrets.Status.ConnectionOk", 2) {
		t.Fatalf("status = %q, want the success message", got)
	}
}

func TestTestConnectionReportsTheTransportError(t *testing.T) {
	a := newTestApp(t)
	view := newSecretsTestView(t, a)
	stubLsRemote(t, nil, errors.New("no route to host"))
	view.SetCredentialResource("https://example.com/repo.git")

	a.testSecretsConnection(view, "credentials")
	secretsWG.Wait()
	waitForPostQueueDrain(t, a)

	if got := view.SecretsStatus(); !strings.Contains(got, "no route to host") {
		t.Fatalf("status = %q, want it to carry the transport error", got)
	}
}

func TestCheckKeyRefusesToRunWithoutAFile(t *testing.T) {
	a := newTestApp(t)
	view := newSecretsTestView(t, a)

	a.testSecretsConnection(view, "ssh")
	waitForPostQueueDrain(t, a)

	if got := view.SecretsStatus(); got != i18n.T("Dialog.Settings.Secrets.Status.NeedKeyFile") {
		t.Fatalf("status = %q, want the missing-file message", got)
	}
}

func TestCheckKeyAcceptsAPlainPrivateKey(t *testing.T) {
	a := newTestApp(t)
	view := newSecretsTestView(t, a)
	path := writeTestPrivateKey(t, "")
	view.SetKeyPath(path)

	a.testSecretsConnection(view, "ssh")
	secretsWG.Wait()
	waitForPostQueueDrain(t, a)

	if got := view.SecretsStatus(); got != i18n.T("Dialog.Settings.Secrets.Status.KeyOk") {
		t.Fatalf("status = %q, want the success message", got)
	}
}

func TestCheckKeyReportsAFileItCannotRead(t *testing.T) {
	a := newTestApp(t)
	view := newSecretsTestView(t, a)
	view.SetKeyPath(filepath.Join(t.TempDir(), "missing"))

	a.testSecretsConnection(view, "ssh")
	secretsWG.Wait()
	waitForPostQueueDrain(t, a)

	if got := view.SecretsStatus(); !strings.Contains(got, "missing") {
		t.Fatalf("status = %q, want it to name the unreadable file", got)
	}
}

func TestCheckPrivateKeyFileNeedsThePassphraseForAnEncryptedKey(t *testing.T) {
	path := writeTestPrivateKey(t, "p4ss")

	if err := checkPrivateKeyFile(path, nil); !errors.Is(err, ErrKeyPassphraseRequired) {
		t.Fatalf("err = %v, want ErrKeyPassphraseRequired", err)
	}
	if err := checkPrivateKeyFile(path, []byte("p4ss")); err != nil {
		t.Fatalf("err = %v, want the key to open with its passphrase", err)
	}
	if err := checkPrivateKeyFile(path, []byte("wrong")); err == nil {
		t.Fatal("a wrong passphrase must be reported")
	}
}

func TestCheckPrivateKeyFileRejectsAFileThatIsNotAKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notakey")
	if err := os.WriteFile(path, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := checkPrivateKeyFile(path, nil); err == nil {
		t.Fatal("a file that is not a private key must be reported")
	}
}

func TestCredentialSourceInfoFollowsTheChosenSource(t *testing.T) {
	a := newTestApp(t)
	view := newSecretsTestView(t, a)

	a.refreshCredentialSourceInfoFor(view, config.CredentialSourceVault)
	if view.CredentialHelpersLine() != "" {
		t.Fatalf("helpers line = %q, want it hidden for the vault-only source", view.CredentialHelpersLine())
	}
	if view.CredentialStoreLine() == "" {
		t.Fatal("the vault-only source must name the store")
	}

	a.refreshCredentialSourceInfoFor(view, config.CredentialSourceHelper)
	if view.CredentialStoreLine() != "" {
		t.Fatalf("store line = %q, want it hidden for the helper-only source", view.CredentialStoreLine())
	}
	if view.CredentialHelpersLine() == "" {
		t.Fatal("the helper-only source must name the helpers")
	}

	a.refreshCredentialSourceInfoFor(view, config.CredentialSourceVaultThenHelper)
	if view.CredentialStoreLine() == "" || view.CredentialHelpersLine() == "" {
		t.Fatal("the combined source must name both the store and the helpers")
	}
}

func TestWiringTheSecretsViewConnectsTheCheckAndSourceCallbacks(t *testing.T) {
	a := newTestApp(t)
	view := newSecretsTestView(t, a)

	a.wireSecretsView(view)
	secretsWG.Wait()

	if view.OnTestConnection == nil {
		t.Fatal("the check button must be connected")
	}
	if view.OnCredentialSource == nil {
		t.Fatal("choosing a credential source must be connected")
	}
	view.OnCredentialSource(config.CredentialSourceHelper)
	if view.CredentialStoreLine() != "" {
		t.Fatal("choosing the helper source must drop the store line right away")
	}
}

func writeTestPrivateKey(t *testing.T, passphrase string) string {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	var block *pem.Block
	if passphrase == "" {
		block, err = ssh.MarshalPrivateKey(priv, "")
	} else {
		block, err = ssh.MarshalPrivateKeyWithPassphrase(priv, "", []byte(passphrase))
	}
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "id_ed25519")
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
