package app

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/oops1/headless-gui/v3/widget"
	"golang.org/x/crypto/ssh"

	"github.com/oops1/gogit/internal/gitcore/transport"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/hostkey"
	"github.com/oops1/gogit/internal/ui/keypassphrase"
	"github.com/oops1/gogit/internal/ui/unlock"
	"github.com/oops1/gogit/internal/vault"
)

type passphraseQuestions struct {
	mu       sync.Mutex
	requests []keypassphrase.Request
}

func (q *passphraseQuestions) all() []keypassphrase.Request {
	q.mu.Lock()
	defer q.mu.Unlock()
	return slices.Clone(q.requests)
}

func stubKeyPassphraseDialog(t *testing.T, a *App, respond func(req keypassphrase.Request) (keypassphrase.Result, bool)) *passphraseQuestions {
	t.Helper()
	questions := &passphraseQuestions{}
	prev := newKeyPassphraseView
	newKeyPassphraseView = func(eng widget.ModalShower, req keypassphrase.Request) (*keypassphrase.View, error) {
		view, err := prev(eng, req)
		if err != nil {
			return nil, err
		}
		questions.mu.Lock()
		questions.requests = append(questions.requests, req)
		questions.mu.Unlock()
		result, ok := respond(req)
		a.Post(func() {
			if ok {
				view.OnOK(result)
			} else {
				view.OnCancel()
			}
		})
		return view, nil
	}
	t.Cleanup(func() { newKeyPassphraseView = prev })
	return questions
}

func stubSSHUserHome(t *testing.T, home string, err error) {
	t.Helper()
	prev := sshUserHomeDir
	sshUserHomeDir = func() (string, error) { return home, err }
	t.Cleanup(func() { sshUserHomeDir = prev })
}

func appWithVault(t *testing.T) (*App, *vault.Vault) {
	t.Helper()
	a := newTestApp(t)
	v := createTestVault(t, a.paths.VaultFile())
	a.vaultInst = v
	return a, v
}

func setVaultSSHKey(t *testing.T, v *vault.Vault, key vault.SSHKey) {
	t.Helper()
	if err := v.SetSSHKey(key); err != nil {
		t.Fatal(err)
	}
}

func TestVaultSSHKeysOfferTheStoredKeyForTheHost(t *testing.T) {
	a, v := appWithVault(t)
	keyFile := filepath.Join(t.TempDir(), "id_work")
	if err := os.WriteFile(keyFile, []byte("PRIVATE"), 0o600); err != nil {
		t.Fatal(err)
	}
	setVaultSSHKey(t, v, vault.SSHKey{Host: "github-work", Path: keyFile, Passphrase: []byte("stored")})
	setVaultSSHKey(t, v, vault.SSHKey{Host: "real.example.com", Path: "inline", Private: []byte("INLINE")})
	source := vaultSSHKeys{app: a}

	keys, err := source.Keys(t.Context(), "github-work")
	if err != nil || len(keys) != 1 || string(keys[0].Private) != "PRIVATE" || string(keys[0].Passphrase) != "stored" || keys[0].Path != keyFile {
		t.Fatalf("Keys = %+v, %v", keys, err)
	}
	keys, err = source.SSHKeys(t.Context(), transport.SSHTarget{Host: "alias", HostName: "real.example.com"})
	if err != nil || len(keys) != 1 || string(keys[0].Private) != "INLINE" {
		t.Fatalf("SSHKeys by host name = %+v, %v", keys, err)
	}
	keys, err = source.SSHKeys(t.Context(), transport.SSHTarget{Host: "unknown"})
	if err != nil || keys != nil {
		t.Fatalf("SSHKeys for an unknown host = %+v, %v", keys, err)
	}
}

func TestVaultSSHKeysReportAMissingKeyFile(t *testing.T) {
	a, v := appWithVault(t)
	setVaultSSHKey(t, v, vault.SSHKey{Host: "github.com", Path: filepath.Join(t.TempDir(), "missing")})
	if _, err := (vaultSSHKeys{app: a}).Keys(t.Context(), "github.com"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Keys returned %v, want a missing file error", err)
	}
}

func TestVaultSSHKeysAreEmptyWithoutAStore(t *testing.T) {
	a := newTestApp(t)
	keys, err := (vaultSSHKeys{app: a}).Keys(t.Context(), "github.com")
	if err != nil || keys != nil {
		t.Fatalf("Keys = %+v, %v; want nothing without a secret store", keys, err)
	}
}

func TestExpandKeyPath(t *testing.T) {
	home := t.TempDir()
	stubSSHUserHome(t, home, nil)
	backslashed := `~\.ssh\id`
	if os.IsPathSeparator('\\') {
		backslashed = filepath.Join(home, ".ssh", "id")
	}
	for input, want := range map[string]string{
		"~":           home,
		"~/.ssh/id":   filepath.Join(home, ".ssh", "id"),
		`~\.ssh\id`:   backslashed,
		"~other/key":  "~other/key",
		"/keys/work":  "/keys/work",
		"relative/id": "relative/id",
	} {
		if got := expandKeyPath(input); got != want {
			t.Fatalf("expandKeyPath(%q) = %q, want %q", input, got, want)
		}
	}
	stubSSHUserHome(t, "", errors.New("no home"))
	if got := expandKeyPath("~/.ssh/id"); got != "~/.ssh/id" {
		t.Fatalf("expandKeyPath without a home = %q", got)
	}
}

func TestSSHKeySourceReadsTheDefaultIdentityFiles(t *testing.T) {
	a := newTestApp(t)
	t.Setenv("SSH_AUTH_SOCK", "")
	home := t.TempDir()
	identity := filepath.Join(home, ".ssh", "id_ed25519")
	if err := writeFile(filepath.Dir(identity), "id_ed25519", "KEY"); err != nil {
		t.Fatal(err)
	}
	hasIdentity := func() bool {
		keys, _ := a.sshKeySource().Keys(t.Context(), "github.com")
		return slices.ContainsFunc(keys, func(k transport.Key) bool { return k.Path == identity })
	}

	stubSSHUserHome(t, home, nil)
	if !hasIdentity() {
		t.Fatalf("the default identity %s was not offered", identity)
	}
	stubSSHUserHome(t, "", errors.New("no home"))
	if hasIdentity() {
		t.Fatalf("an identity was offered without a home directory")
	}
}

func TestKeyPassphraseSourceAsksAndRemembersTheWorkingPassphrase(t *testing.T) {
	a, v := appWithVault(t)
	questions := stubKeyPassphraseDialog(t, a, func(keypassphrase.Request) (keypassphrase.Result, bool) {
		return keypassphrase.Result{Passphrase: []byte("open sesame"), Remember: true}, true
	})
	source := a.keyPassphraseSource()
	req := transport.PassphraseRequest{Host: "github.com", Path: "/keys/work", Retry: true}

	got, err := source.Passphrase(t.Context(), req)
	if err != nil || string(got) != "open sesame" {
		t.Fatalf("Passphrase = %q, %v", got, err)
	}
	if asked := questions.all(); len(asked) != 1 || asked[0] != (keypassphrase.Request{Host: "github.com", Path: "/keys/work", Retry: true}) {
		t.Fatalf("questions = %+v", asked)
	}
	source.ApprovePassphrase(t.Context(), req, got)

	stored, ok := v.SSHKey("github.com")
	if !ok || stored.Path != "/keys/work" || string(stored.Passphrase) != "open sesame" {
		t.Fatalf("stored = %+v, %v", stored, ok)
	}
}

func TestKeyPassphraseSourceDoesNotStoreWithoutConsent(t *testing.T) {
	a, v := appWithVault(t)
	stubKeyPassphraseDialog(t, a, func(keypassphrase.Request) (keypassphrase.Result, bool) {
		return keypassphrase.Result{Passphrase: []byte("open sesame")}, true
	})
	source := a.keyPassphraseSource()
	req := transport.PassphraseRequest{Host: "github.com", Path: "/keys/work"}
	got, err := source.Passphrase(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	source.ApprovePassphrase(t.Context(), req, got)
	if _, ok := v.SSHKey("github.com"); ok {
		t.Fatal("the passphrase was stored although the user did not ask to remember it")
	}
}

func TestKeyPassphraseSourceReportsADismissedOrBrokenQuestion(t *testing.T) {
	a := newTestApp(t)
	stubKeyPassphraseDialog(t, a, func(keypassphrase.Request) (keypassphrase.Result, bool) {
		return keypassphrase.Result{}, false
	})
	if _, err := a.keyPassphraseSource().Passphrase(t.Context(), transport.PassphraseRequest{}); !errors.Is(err, transport.ErrNoCredentials) {
		t.Fatalf("cancelled question returned %v, want ErrNoCredentials", err)
	}

	prev := newKeyPassphraseView
	newKeyPassphraseView = func(widget.ModalShower, keypassphrase.Request) (*keypassphrase.View, error) {
		return nil, errors.New("boom")
	}
	defer func() { newKeyPassphraseView = prev }()
	if _, err := a.keyPassphraseSource().Passphrase(t.Context(), transport.PassphraseRequest{}); !errors.Is(err, transport.ErrNoCredentials) {
		t.Fatalf("broken dialog returned %v, want ErrNoCredentials", err)
	}
}

func TestKeyPassphraseQuestionEndsWithTheOperation(t *testing.T) {
	a := newTestApp(t)
	prev := newKeyPassphraseView
	opened := make(chan struct{})
	newKeyPassphraseView = func(eng widget.ModalShower, req keypassphrase.Request) (*keypassphrase.View, error) {
		view, err := prev(eng, req)
		close(opened)
		return view, err
	}
	t.Cleanup(func() { newKeyPassphraseView = prev })

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := a.keyPassphraseSource().Passphrase(ctx, transport.PassphraseRequest{Host: "github.com"})
		done <- err
	}()
	<-opened
	cancel()
	if err := <-done; !errors.Is(err, transport.ErrNoCredentials) {
		t.Fatalf("err = %v, want ErrNoCredentials", err)
	}
}

func rememberWork(a *App) {
	a.rememberKeyPassphrase(context.Background(), transport.PassphraseRequest{Host: "github.com", Path: "/keys/work"}, []byte("pp-secret"))
}

func TestRememberKeyPassphraseNeedsASecretStore(t *testing.T) {
	a := newTestApp(t)
	log := captureLog(a)
	rememberWork(a)
	if !strings.Contains(log.String(), "secret store is not set up") || strings.Contains(log.String(), "pp-secret") {
		t.Fatalf("log = %q", log.String())
	}
}

func TestRememberKeyPassphraseUnlocksTheStoreFirst(t *testing.T) {
	a, v := appWithVault(t)
	v.Lock()
	stubUnlockDialog(t, a, []unlock.Result{{Password: []byte(testVaultPassword)}})
	rememberWork(a)
	if stored, ok := v.SSHKey("github.com"); !ok || string(stored.Passphrase) != "pp-secret" {
		t.Fatalf("stored = %+v, %v", stored, ok)
	}
}

func TestRememberKeyPassphraseGivesUpWhenUnlockIsCancelled(t *testing.T) {
	a, v := appWithVault(t)
	v.Lock()
	stubUnlockDialog(t, a, nil)
	rememberWork(a)
	if !v.Locked() {
		t.Fatal("the store was unlocked although the user cancelled")
	}
}

func TestRememberKeyPassphraseLogsAFailedUnlock(t *testing.T) {
	a, v := appWithVault(t)
	if err := v.Close(); err != nil {
		t.Fatal(err)
	}
	log := captureLog(a)
	stubUnlockDialog(t, a, []unlock.Result{{Password: []byte(testVaultPassword)}})
	rememberWork(a)
	if !strings.Contains(log.String(), "unlock secret store for key passphrase failed") {
		t.Fatalf("log = %q", log.String())
	}
}

func TestRememberKeyPassphraseKeepsAnotherKeyOfTheHost(t *testing.T) {
	a, v := appWithVault(t)
	setVaultSSHKey(t, v, vault.SSHKey{Host: "github.com", Path: "/keys/other"})
	log := captureLog(a)
	rememberWork(a)
	stored, _ := v.SSHKey("github.com")
	if stored.Path != "/keys/other" || len(stored.Passphrase) != 0 {
		t.Fatalf("stored = %+v, want the other key untouched", stored)
	}
	if !strings.Contains(log.String(), "save key passphrase failed") || strings.Contains(log.String(), "pp-secret") {
		t.Fatalf("log = %q", log.String())
	}
}

func TestRememberKeyPassphraseKeepsTheStoredPrivateKey(t *testing.T) {
	a, v := appWithVault(t)
	setVaultSSHKey(t, v, vault.SSHKey{Host: "*", Path: "/keys/any"})
	setVaultSSHKey(t, v, vault.SSHKey{Host: "github.com", Path: "/keys/work", Private: []byte("PRIVATE")})
	rememberWork(a)
	stored, _ := v.SSHKey("github.com")
	if string(stored.Private) != "PRIVATE" || string(stored.Passphrase) != "pp-secret" {
		t.Fatalf("stored = %+v", stored)
	}
}

func TestRememberKeyPassphraseLogsAFailedSave(t *testing.T) {
	a := newTestApp(t)
	dir := filepath.Join(t.TempDir(), "store")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	a.vaultInst = createTestVault(t, filepath.Join(dir, "vault.bin"))
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	log := captureLog(a)
	rememberWork(a)
	if !strings.Contains(log.String(), "save key passphrase failed") {
		t.Fatalf("log = %q", log.String())
	}
}

func TestSSHTransportOptionsWithoutACommand(t *testing.T) {
	a := newTestApp(t)
	t.Setenv("GIT_SSH_COMMAND", "")
	opts := a.sshTransportOptions()
	if !slices.Equal(opts.ConfigFiles, transport.DefaultSSHConfigFiles()) || opts.Overrides != nil || opts.Passphrases == nil {
		t.Fatalf("options = %+v", opts)
	}
}

func TestSSHTransportOptionsApplyGitSSHCommand(t *testing.T) {
	a := newTestApp(t)
	home := t.TempDir()
	stubSSHUserHome(t, home, nil)
	t.Setenv("GIT_SSH_COMMAND", "ssh -F ~/work_config -i work_key -v")
	log := captureLog(a)

	opts := a.sshTransportOptions()
	if !slices.Equal(opts.ConfigFiles, []string{filepath.Join(home, "work_config")}) || !slices.Equal(opts.Overrides, []string{"IdentityFile work_key"}) {
		t.Fatalf("options = %+v", opts)
	}
	if !strings.Contains(log.String(), "-v") {
		t.Fatalf("the ignored argument was not logged: %q", log.String())
	}

	t.Setenv("GIT_SSH_COMMAND", `ssh -i "open`)
	if _, ignored := a.sshCommand(); len(ignored) != 1 || !strings.Contains(ignored[0], "unterminated") {
		t.Fatalf("ignored = %q, want the parse error", ignored)
	}
}

func TestSSHTransportOptionsReadCoreSSHCommandOfTheRepository(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "plain")
	initTestRepoWithBranch(t, dir, "main")
	if err := writeFile(filepath.Join(dir, ".git"), "config", "[core]\n\trepositoryformatversion = 0\n\tbare = false\n\tsshCommand = ssh -p 2222\n"); err != nil {
		t.Fatal(err)
	}
	a := newRemoteTestApp(t)
	node, err := a.registry.AddRepository("plain", dir, "")
	if err != nil {
		t.Fatal(err)
	}
	a.ActivateRepository(node.ID)
	t.Setenv("GIT_SSH_COMMAND", "")

	if opts := a.sshTransportOptions(); !slices.Equal(opts.Overrides, []string{"Port 2222"}) {
		t.Fatalf("overrides = %q, want the core.sshCommand port", opts.Overrides)
	}
}

func TestOperationLogExplainsIgnoredSSHArgumentsAndProxyCommand(t *testing.T) {
	a := newTestApp(t)
	views := captureOperationViews(t)
	t.Setenv("GIT_SSH_COMMAND", "ssh -v")

	a.RunOperation("Fetch", func(_ context.Context, reporter OperationReporter) error {
		a.reportIgnoredSSHArguments(reporter)
		reportTransportError(reporter, fmt.Errorf("dial: %w", transport.ErrProxyCommandUnsupported))
		reportTransportError(reporter, errors.New("other"))
		return nil
	})
	view := lastOperationView(t, views)
	waitForFinishedOperation(t, a, view)

	lines := readOnDispatcher(t, a, view.Lines)
	want := []string{i18n.Tf("Operation.Log.SSHCommandIgnored", "-v"), i18n.T("Operation.Log.ProxyCommandUnsupported")}
	if !slices.Equal(lines[:len(want)], want) {
		t.Fatalf("lines = %q, want %q", lines, want)
	}
}

func TestOperationLogStaysQuietWithoutIgnoredSSHArguments(t *testing.T) {
	a := newTestApp(t)
	views := captureOperationViews(t)
	t.Setenv("GIT_SSH_COMMAND", "ssh -p 22")

	a.RunOperation("Fetch", func(_ context.Context, reporter OperationReporter) error {
		a.reportIgnoredSSHArguments(reporter)
		return nil
	})
	view := lastOperationView(t, views)
	waitForFinishedOperation(t, a, view)
	for _, line := range readOnDispatcher(t, a, view.Lines) {
		if strings.Contains(line, "-p") {
			t.Fatalf("a supported argument was reported: %q", line)
		}
	}
}

func TestTransportErrorText(t *testing.T) {
	if got := transportErrorText(fmt.Errorf("x: %w", transport.ErrProxyCommandUnsupported)); got != i18n.T("Operation.Log.ProxyCommandUnsupported") {
		t.Fatalf("text = %q", got)
	}
	if got := transportErrorText(errors.New("boom")); got != "boom" {
		t.Fatalf("text = %q", got)
	}
}

func startAdvertisingSSHServer(t *testing.T, allowed ssh.PublicKey) string {
	t.Helper()
	_, hostPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	hostSigner, err := ssh.NewSignerFromKey(hostPriv)
	if err != nil {
		t.Fatal(err)
	}
	config := &ssh.ServerConfig{
		PublicKeyCallback: func(_ ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if bytes.Equal(key.Marshal(), allowed.Marshal()) {
				return nil, nil
			}
			return nil, errors.New("unknown key")
		},
	}
	config.AddHostKey(hostSigner)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	wg.Go(func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			wg.Go(func() { serveOneAdvertisement(conn, config) })
		}
	})
	t.Cleanup(func() {
		_ = ln.Close()
		wg.Wait()
	})
	return ln.Addr().String()
}

func serveOneAdvertisement(conn net.Conn, config *ssh.ServerConfig) {
	defer func() { _ = conn.Close() }()
	sConn, chans, reqs, err := ssh.NewServerConn(conn, config)
	if err != nil {
		return
	}
	defer func() { _ = sConn.Close() }()
	go ssh.DiscardRequests(reqs)
	for newCh := range chans {
		ch, requests, err := newCh.Accept()
		if err != nil {
			return
		}
		for req := range requests {
			_ = req.Reply(req.Type == "exec" || req.Type == "env", nil)
			if req.Type != "exec" {
				continue
			}
			line := strings.Repeat("1", 40) + " refs/heads/main\x00agent=test\n"
			_, _ = fmt.Fprintf(ch, "%04x%s0000", len(line)+4, line)
			_, _ = ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
			_ = ch.Close()
		}
	}
}

func TestSSHConnectionAsksForTheVaultKeyPassphraseAndRemembersIt(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("ProgramData", t.TempDir())
	t.Setenv("SSH_AUTH_SOCK", "")
	t.Setenv("GIT_SSH_COMMAND", "")
	stubSSHUserHome(t, home, nil)

	a, v := appWithVault(t)
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKeyWithPassphrase(priv, "", []byte("open sesame"))
	if err != nil {
		t.Fatal(err)
	}
	keyFile := filepath.Join(home, "work_key")
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}
	setVaultSSHKey(t, v, vault.SSHKey{Host: "127.0.0.1", Path: keyFile})
	addr := startAdvertisingSSHServer(t, signer.PublicKey())
	stubHostKeyDialog(t, a, func(hostkey.Request) bool { return true })
	questions := stubKeyPassphraseDialog(t, a, func(keypassphrase.Request) (keypassphrase.Result, bool) {
		return keypassphrase.Result{Passphrase: []byte("open sesame"), Remember: true}, true
	})

	session, err := transport.Dial(t.Context(), "ssh://git@"+addr+"/repo.git", transport.UploadPack, a.transportOptions(nil))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = session.Close() }()
	adv, err := session.Advertise(t.Context())
	if err != nil {
		t.Fatalf("Advertise returned error %v", err)
	}
	if len(adv.Refs) != 1 {
		t.Fatalf("refs = %+v", adv.Refs)
	}
	if asked := questions.all(); len(asked) != 1 || asked[0].Host != "127.0.0.1" || asked[0].Path != keyFile || asked[0].Retry {
		t.Fatalf("questions = %+v", asked)
	}
	if stored, ok := v.SSHKey("127.0.0.1"); !ok || string(stored.Passphrase) != "open sesame" || stored.Path != keyFile {
		t.Fatalf("stored = %+v, %v", stored, ok)
	}
}
