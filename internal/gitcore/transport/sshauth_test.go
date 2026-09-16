package transport

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

func collectSigners(ctx context.Context, source KeySource) ([]ssh.Signer, error) {
	auth, err := newSSHAuth(ctx, SSHTarget{Host: "host"}, source, nil)
	if err != nil {
		return nil, err
	}
	defer auth.release()
	return auth.ready, nil
}

type trackedConn struct {
	net.Conn
	closed atomic.Bool
}

func (c *trackedConn) Close() error {
	c.closed.Store(true)
	return c.Conn.Close()
}

func serveKeyringAgent(t *testing.T, keys ...any) *[]*trackedConn {
	t.Helper()
	keyring := agent.NewKeyring()
	for _, key := range keys {
		if err := keyring.Add(agent.AddedKey{PrivateKey: key}); err != nil {
			t.Fatalf("Add returned error %v", err)
		}
	}
	var mu sync.Mutex
	conns := &[]*trackedConn{}
	restore := agentDialer
	agentDialer = func() (io.ReadWriteCloser, error) {
		serverEnd, clientEnd := net.Pipe()
		go func() {
			_ = agent.ServeAgent(keyring, serverEnd)
			_ = serverEnd.Close()
		}()
		tracked := &trackedConn{Conn: clientEnd}
		mu.Lock()
		*conns = append(*conns, tracked)
		mu.Unlock()
		return tracked, nil
	}
	t.Cleanup(func() { agentDialer = restore })
	return conns
}

func advertiseOnlyServer(t *testing.T, allowed ...ssh.PublicKey) (string, ssh.Signer) {
	t.Helper()
	hostSigner := generateSSHSigner(t)
	adv := gitV1AdvertisementBytes(testHeadCaps, [][2]string{{idOf(1).String(), "refs/heads/main"}})
	addr := startFakeSSHServer(t, sshServerConfig{
		hostKey:     hostSigner,
		allowedKeys: allowed,
		handle:      sshAdvertiseOnlyHandler(sshExecCommand(UploadPack, "/repo.git"), adv),
	})
	return addr, hostSigner
}

func advertiseSSH(t *testing.T, ctx context.Context, addr string, hostKey ssh.PublicKey, opts Options) error {
	t.Helper()
	opts.HostKeys = NewKnownHosts(writeKnownHostsFile(t, addr, hostKey), neverConfirm(t))
	session, err := Dial(t.Context(), "ssh://"+addr+"/repo.git", UploadPack, opts)
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()
	_, err = session.Advertise(ctx)
	return err
}

func TestSSHAdvertiseAuthenticatesWithAKeyHeldByTheAgent(t *testing.T) {
	priv, signer := generateEd25519(t)
	conns := serveKeyringAgent(t, priv)
	addr, hostSigner := advertiseOnlyServer(t, signer.PublicKey())

	if err := advertiseSSH(t, t.Context(), addr, hostSigner.PublicKey(), Options{Keys: NewAgentKeys()}); err != nil {
		t.Fatalf("Advertise returned error %v, want the agent key to sign the handshake", err)
	}
	if len(*conns) != 1 {
		t.Fatalf("agent was dialled %d times, want 1", len(*conns))
	}
	if !(*conns)[0].closed.Load() {
		t.Fatalf("the agent connection stayed open after the handshake")
	}
}

type scriptedPassphrases struct {
	mu        sync.Mutex
	answers   [][]byte
	requests  []PassphraseRequest
	approved  []string
	onRequest func()
}

func (s *scriptedPassphrases) Passphrase(_ context.Context, req PassphraseRequest) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests = append(s.requests, req)
	if s.onRequest != nil {
		s.onRequest()
	}
	if len(s.answers) == 0 {
		return nil, ErrNoCredentials
	}
	answer := s.answers[0]
	s.answers = s.answers[1:]
	return append([]byte(nil), answer...), nil
}

func (s *scriptedPassphrases) ApprovePassphrase(_ context.Context, req PassphraseRequest, passphrase []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.approved = append(s.approved, req.Path+"="+string(passphrase))
}

func encryptedKeySource(t *testing.T, passphrase string, stored []byte) (KeySource, ssh.PublicKey) {
	t.Helper()
	priv, signer := generateEd25519(t)
	encoded := encodePrivateKey(t, priv, []byte(passphrase))
	return fixedKeySource{keys: []Key{{Path: "locked-key", Private: encoded, Passphrase: stored}}}, signer.PublicKey()
}

func TestSSHAuthPromptsForAnEncryptedKeyOnlyAfterTheReadyKeysFail(t *testing.T) {
	locked, lockedPub := encryptedKeySource(t, "open sesame", nil)
	addr, hostSigner := advertiseOnlyServer(t, lockedPub)
	prompt := &scriptedPassphrases{answers: [][]byte{[]byte("open sesame")}}

	opts := Options{
		Keys: MultiKeys(locked, testSignerKeySource{generateSSHSigner(t)}),
		SSH:  SSHOptions{Passphrases: prompt},
	}
	if err := advertiseSSH(t, t.Context(), addr, hostSigner.PublicKey(), opts); err != nil {
		t.Fatalf("Advertise returned error %v", err)
	}
	if len(prompt.requests) != 1 || prompt.requests[0].Retry || prompt.requests[0].Path != "locked-key" {
		t.Fatalf("requests = %+v, want one first-time request for locked-key", prompt.requests)
	}
	if len(prompt.approved) != 1 || prompt.approved[0] != "locked-key=open sesame" {
		t.Fatalf("approved = %v, want the working passphrase", prompt.approved)
	}
}

func TestSSHAuthDoesNotPromptWhenAReadyKeyIsAccepted(t *testing.T) {
	locked, _ := encryptedKeySource(t, "open sesame", nil)
	ready := generateSSHSigner(t)
	addr, hostSigner := advertiseOnlyServer(t, ready.PublicKey())
	prompt := &scriptedPassphrases{}

	opts := Options{Keys: MultiKeys(locked, testSignerKeySource{ready}), SSH: SSHOptions{Passphrases: prompt}}
	if err := advertiseSSH(t, t.Context(), addr, hostSigner.PublicKey(), opts); err != nil {
		t.Fatalf("Advertise returned error %v", err)
	}
	if len(prompt.requests) != 0 {
		t.Fatalf("the passphrase was requested %d times although a ready key worked", len(prompt.requests))
	}
}

func TestSSHAuthAsksAgainAfterAWrongPassphrase(t *testing.T) {
	locked, lockedPub := encryptedKeySource(t, "right", []byte("stale"))
	addr, hostSigner := advertiseOnlyServer(t, lockedPub)
	prompt := &scriptedPassphrases{answers: [][]byte{[]byte("wrong"), []byte("right")}}

	opts := Options{Keys: locked, SSH: SSHOptions{Passphrases: prompt}}
	if err := advertiseSSH(t, t.Context(), addr, hostSigner.PublicKey(), opts); err != nil {
		t.Fatalf("Advertise returned error %v", err)
	}
	if len(prompt.requests) != 2 || !prompt.requests[0].Retry || !prompt.requests[1].Retry {
		t.Fatalf("requests = %+v, want two retries after the stale stored passphrase", prompt.requests)
	}
}

func TestSSHAuthGivesUpAfterThreeWrongPassphrases(t *testing.T) {
	locked, lockedPub := encryptedKeySource(t, "right", nil)
	addr, hostSigner := advertiseOnlyServer(t, lockedPub)
	prompt := &scriptedPassphrases{answers: [][]byte{[]byte("a"), []byte("b"), []byte("c"), []byte("right")}}

	opts := Options{Keys: locked, SSH: SSHOptions{Passphrases: prompt}}
	err := advertiseSSH(t, t.Context(), addr, hostSigner.PublicKey(), opts)
	if !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("Advertise returned %v, want ErrAccessDenied", err)
	}
	if len(prompt.requests) != maxPassphraseAttempts {
		t.Fatalf("asked %d times, want %d", len(prompt.requests), maxPassphraseAttempts)
	}
}

func TestSSHAuthSkipsAKeyWhoseQuestionWasDismissed(t *testing.T) {
	locked, lockedPub := encryptedKeySource(t, "right", nil)
	addr, hostSigner := advertiseOnlyServer(t, lockedPub)
	prompt := &scriptedPassphrases{}

	opts := Options{Keys: locked, SSH: SSHOptions{Passphrases: prompt}}
	err := advertiseSSH(t, t.Context(), addr, hostSigner.PublicKey(), opts)
	if !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("Advertise returned %v, want ErrAccessDenied", err)
	}
}

func TestSSHAuthStopsWhenTheOperationIsCancelledDuringTheQuestion(t *testing.T) {
	locked, lockedPub := encryptedKeySource(t, "right", nil)
	addr, hostSigner := advertiseOnlyServer(t, lockedPub)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	prompt := &scriptedPassphrases{onRequest: cancel}

	opts := Options{Keys: locked, SSH: SSHOptions{Passphrases: prompt}}
	err := advertiseSSH(t, ctx, addr, hostSigner.PublicKey(), opts)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Advertise returned %v, want context.Canceled", err)
	}
}

func TestNewSSHAuthReturnsTheContextErrorOnceCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := newSSHAuth(ctx, SSHTarget{}, testSignerKeySource{generateSSHSigner(t)}, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("newSSHAuth returned %v, want context.Canceled", err)
	}
}

func TestNewSSHAuthKeepsLockedKeysOnlyWhenItCanAsk(t *testing.T) {
	locked, _ := encryptedKeySource(t, "right", nil)
	if _, err := newSSHAuth(t.Context(), SSHTarget{}, locked, nil); !errors.Is(err, errKeyLocked) {
		t.Fatalf("newSSHAuth without a prompt returned %v, want errKeyLocked", err)
	}
	auth, err := newSSHAuth(t.Context(), SSHTarget{}, locked, &scriptedPassphrases{})
	if err != nil {
		t.Fatalf("newSSHAuth with a prompt returned error %v", err)
	}
	if len(auth.locked) != 1 {
		t.Fatalf("locked = %d, want 1", len(auth.locked))
	}
}

func TestCloserSetClosesInReverseOrderOnce(t *testing.T) {
	var order []int
	var set closerSet
	for i := range 3 {
		set.add(closerFunc(func() error {
			order = append(order, i)
			return nil
		}))
	}
	set.closeAll()
	set.closeAll()
	if len(order) != 3 || order[0] != 2 || order[2] != 0 {
		t.Fatalf("close order = %v, want [2 1 0]", order)
	}
}

type targetRecordingKeySource struct {
	got SSHTarget
}

func (s *targetRecordingKeySource) Keys(context.Context, string) ([]Key, error) {
	return nil, errors.New("Keys must not be used when SSHKeys is available")
}

func (s *targetRecordingKeySource) SSHKeys(_ context.Context, target SSHTarget) ([]Key, error) {
	s.got = target
	return nil, nil
}

func TestKeySourcesReceiveTheResolvedTarget(t *testing.T) {
	source := &targetRecordingKeySource{}
	target := SSHTarget{Host: "alias", HostName: "real.example.com", User: "git"}
	_, err := newSSHAuth(t.Context(), target, MultiKeys(source), nil)
	if !errors.Is(err, ErrNoKeys) {
		t.Fatalf("newSSHAuth returned %v, want ErrNoKeys", err)
	}
	if source.got.HostName != "real.example.com" || source.got.User != "git" {
		t.Fatalf("target = %+v", source.got)
	}
}

func TestDirKeysReadTheConfiguredIdentityFilesInsteadOfTheDefaults(t *testing.T) {
	dir := t.TempDir()
	priv, _ := generateEd25519(t)
	writeKeyFile(t, dir, "id_ed25519", priv, nil)
	custom := writeKeyFile(t, t.TempDir(), "work", priv, nil)

	keys, err := NewDirKeys(dir).(SSHKeySource).SSHKeys(t.Context(), SSHTarget{IdentityFiles: []string{custom, filepath.Join(dir, "missing")}})
	if err != nil {
		t.Fatalf("SSHKeys returned error %v", err)
	}
	if len(keys) != 1 || keys[0].Path != custom {
		t.Fatalf("keys = %+v, want only %s", keys, custom)
	}
}

func TestAgentOffersOnlyTheIdentityFilesWhenIdentitiesOnlyIsSet(t *testing.T) {
	dir := t.TempDir()
	pubPriv, pubSigner := generateEd25519(t)
	filePriv, fileSigner := generateEd25519(t)
	strayPriv, _ := generateEd25519(t)
	conns := serveKeyringAgent(t, pubPriv, filePriv, strayPriv)

	withPub := filepath.Join(dir, "with_pub")
	if err := os.WriteFile(withPub+".pub", ssh.MarshalAuthorizedKey(pubSigner.PublicKey()), 0o600); err != nil {
		t.Fatal(err)
	}
	brokenPub := writeKeyFile(t, dir, "broken_pub", filePriv, nil)
	if err := os.WriteFile(brokenPub+".pub", []byte("garbage"), 0o600); err != nil {
		t.Fatal(err)
	}
	unreadable := writeKeyFile(t, dir, "encrypted", strayPriv, []byte("secret"))
	target := SSHTarget{
		IdentitiesOnly: true,
		IdentityFiles:  []string{withPub, brokenPub, unreadable, filepath.Join(dir, "missing")},
	}

	held := &closerSet{}
	signers, err := agentSigners(t.Context(), target, held)
	if err != nil {
		t.Fatalf("agentSigners returned error %v", err)
	}
	defer held.closeAll()
	if len(signers) != 2 {
		t.Fatalf("len(signers) = %d, want 2", len(signers))
	}
	got := map[string]bool{}
	for _, s := range signers {
		got[string(s.PublicKey().Marshal())] = true
	}
	if !got[string(pubSigner.PublicKey().Marshal())] || !got[string(fileSigner.PublicKey().Marshal())] {
		t.Fatalf("the agent offered keys that are not identity files")
	}

	_, err = agentSigners(t.Context(), SSHTarget{IdentitiesOnly: true}, held)
	if !errors.Is(err, ErrNoKeys) {
		t.Fatalf("agentSigners returned %v, want ErrNoKeys when no identity matches", err)
	}
	if !(*conns)[1].closed.Load() {
		t.Fatalf("the agent connection was left open after no identity matched")
	}
}
