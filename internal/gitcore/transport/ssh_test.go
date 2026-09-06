package transport

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

func generateSSHSigner(t *testing.T) ssh.Signer {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("ed25519.GenerateKey returned error %v", err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("ssh.NewSignerFromKey returned error %v", err)
	}
	return signer
}

func generateEd25519(t *testing.T) (ed25519.PrivateKey, ssh.Signer) {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("ed25519.GenerateKey returned error %v", err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("ssh.NewSignerFromKey returned error %v", err)
	}
	return priv, signer
}

func encodePrivateKey(t *testing.T, priv ed25519.PrivateKey, passphrase []byte) []byte {
	t.Helper()
	var block *pem.Block
	var err error
	if len(passphrase) == 0 {
		block, err = ssh.MarshalPrivateKey(priv, "")
	} else {
		block, err = ssh.MarshalPrivateKeyWithPassphrase(priv, "", passphrase)
	}
	if err != nil {
		t.Fatalf("marshal private key returned error %v", err)
	}
	return pem.EncodeToMemory(block)
}

func writeKeyFile(t *testing.T, dir, name string, priv ed25519.PrivateKey, passphrase []byte) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, encodePrivateKey(t, priv, passphrase), 0o600); err != nil {
		t.Fatalf("WriteFile returned error %v", err)
	}
	return path
}

func sshKnownHostsLine(addr string, key ssh.PublicKey) string {
	return knownhosts.Line([]string{addr}, key)
}

type sshServerConfig struct {
	hostKey       ssh.Signer
	allowedKeys   []ssh.PublicKey
	handle        func(t *testing.T, ch ssh.Channel, cmd string)
	rejectSession bool
	rejectExec    bool
}

func startFakeSSHServer(t *testing.T, cfg sshServerConfig) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen returned error %v", err)
	}

	config := &ssh.ServerConfig{
		PublicKeyCallback: func(_ ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			for _, k := range cfg.allowedKeys {
				if bytes.Equal(k.Marshal(), key.Marshal()) {
					return nil, nil
				}
			}
			return nil, fmt.Errorf("unknown public key")
		},
	}
	config.AddHostKey(cfg.hostKey)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go serveFakeSSHConn(t, conn, config, cfg)
		}
	}()
	t.Cleanup(func() {
		_ = ln.Close()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Errorf("fake ssh server did not finish within 5s")
		}
	})
	return ln.Addr().String()
}

func serveFakeSSHConn(t *testing.T, conn net.Conn, config *ssh.ServerConfig, cfg sshServerConfig) {
	defer func() { _ = conn.Close() }()
	sConn, chans, reqs, err := ssh.NewServerConn(conn, config)
	if err != nil {
		return
	}
	defer func() { _ = sConn.Close() }()
	go ssh.DiscardRequests(reqs)
	for newCh := range chans {
		if newCh.ChannelType() != "session" {
			_ = newCh.Reject(ssh.UnknownChannelType, "")
			continue
		}
		if cfg.rejectSession {
			_ = newCh.Reject(ssh.Prohibited, "session channels are disabled")
			continue
		}
		ch, requests, err := newCh.Accept()
		if err != nil {
			return
		}
		handleSSHSessionChannel(t, ch, requests, cfg)
	}
}

func handleSSHSessionChannel(t *testing.T, ch ssh.Channel, requests <-chan *ssh.Request, cfg sshServerConfig) {
	for req := range requests {
		switch req.Type {
		case "exec":
			var payload struct{ Command string }
			_ = ssh.Unmarshal(req.Payload, &payload)
			if cfg.rejectExec {
				if req.WantReply {
					_ = req.Reply(false, nil)
				}
				_ = ch.Close()
				continue
			}
			if req.WantReply {
				_ = req.Reply(true, nil)
			}
			if cfg.handle != nil {
				cfg.handle(t, ch, payload.Command)
			}
			_, _ = ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
			_ = ch.Close()
		case "env":
			if req.WantReply {
				_ = req.Reply(true, nil)
			}
		default:
			if req.WantReply {
				_ = req.Reply(false, nil)
			}
		}
	}
}

func sshUploadPackHandler(_ *testing.T, adv []byte, wantHaves int, pack []byte) func(*testing.T, ssh.Channel, string) {
	return func(t *testing.T, ch ssh.Channel, cmd string) {
		wantCmd := sshExecCommand(UploadPack, "/repo.git")
		if cmd != wantCmd {
			t.Errorf("cmd = %q, want %q", cmd, wantCmd)
			return
		}
		if _, err := ch.Write(adv); err != nil {
			t.Errorf("Write returned error %v", err)
			return
		}
		dec := NewDecoder(ch)
		haves, doneNegotiation := readHaveRound(t, dec)
		if haves != wantHaves || !doneNegotiation {
			t.Errorf("round = (haves=%d, done=%v), want (%d, true)", haves, doneNegotiation, wantHaves)
			return
		}
		resp := newPktBuilder().line("NAK\n").raw(sidebandFrame(SidebandPack, pack)).flush().bytes()
		if _, err := ch.Write(resp); err != nil {
			t.Errorf("Write returned error %v", err)
		}
	}
}

func sshAdvertiseOnlyHandler(wantCmd string, adv []byte) func(*testing.T, ssh.Channel, string) {
	return func(t *testing.T, ch ssh.Channel, cmd string) {
		if cmd != wantCmd {
			t.Errorf("cmd = %q, want %q", cmd, wantCmd)
			return
		}
		if _, err := ch.Write(adv); err != nil {
			t.Errorf("Write returned error %v", err)
		}
	}
}

func fixedConfirm(ok bool) func(context.Context, HostKey) (bool, error) {
	return func(context.Context, HostKey) (bool, error) { return ok, nil }
}

func neverConfirm(t *testing.T) func(context.Context, HostKey) (bool, error) {
	return func(context.Context, HostKey) (bool, error) {
		t.Fatalf("confirm was called but the host key should have been accepted or rejected without it")
		return false, nil
	}
}

func writeKnownHostsFile(t *testing.T, addr string, key ssh.PublicKey) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "known_hosts")
	if err := os.WriteFile(path, []byte(sshKnownHostsLine(addr, key)+"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error %v", err)
	}
	return path
}

type fixedKeySource struct{ keys []Key }

func (f fixedKeySource) Keys(context.Context, string) ([]Key, error) {
	return f.keys, nil
}

type testSignerKeySource struct{ signer ssh.Signer }

func (s testSignerKeySource) Keys(context.Context, string) ([]Key, error) {
	return []Key{{Path: "test-signer"}}, nil
}

func (s testSignerKeySource) listSigners(context.Context, string) ([]ssh.Signer, error) {
	return []ssh.Signer{s.signer}, nil
}

type erroringKeySource struct{ err error }

func (e erroringKeySource) Keys(context.Context, string) ([]Key, error) {
	return nil, e.err
}

func TestSSHFetchSucceedsWithDirKeyAndKnownHost(t *testing.T) {
	hostSigner := generateSSHSigner(t)
	clientPriv, clientSigner := generateEd25519(t)

	pack := fakePack(31)
	adv := gitV1AdvertisementBytes(testHeadCaps, [][2]string{{idOf(1).String(), "refs/heads/main"}})

	addr := startFakeSSHServer(t, sshServerConfig{
		hostKey:     hostSigner,
		allowedKeys: []ssh.PublicKey{clientSigner.PublicKey()},
		handle:      sshUploadPackHandler(t, adv, 0, pack),
	})

	dir := t.TempDir()
	writeKeyFile(t, dir, "id_ed25519", clientPriv, nil)

	opts := Options{
		Keys:     NewDirKeys(dir),
		HostKeys: NewKnownHosts(writeKnownHostsFile(t, addr, hostSigner.PublicKey()), neverConfirm(t)),
	}
	session, err := Dial(t.Context(), "ssh://"+addr+"/repo.git", UploadPack, opts)
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()

	resp, err := session.Fetch(t.Context(), FetchRequest{Wants: []hash.ObjectID{idOf(1)}}, nil)
	if err != nil {
		t.Fatalf("Fetch returned error %v", err)
	}
	defer func() { _ = resp.Pack.Close() }()
	got, err := io.ReadAll(resp.Pack)
	if err != nil {
		t.Fatalf("reading Pack returned error %v", err)
	}
	if string(got) != string(pack) {
		t.Fatalf("Pack = %x, want %x", got, pack)
	}
}

func TestSSHAdvertiseRejectsUnknownHostKeyWhenConfirmDeclines(t *testing.T) {
	hostSigner := generateSSHSigner(t)
	clientSigner := generateSSHSigner(t)
	adv := gitV1AdvertisementBytes(testHeadCaps, [][2]string{{idOf(1).String(), "refs/heads/main"}})

	addr := startFakeSSHServer(t, sshServerConfig{
		hostKey:     hostSigner,
		allowedKeys: []ssh.PublicKey{clientSigner.PublicKey()},
		handle:      sshUploadPackHandler(t, adv, 0, nil),
	})

	knownHostsPath := filepath.Join(t.TempDir(), "known_hosts")
	var confirmed int
	confirm := func(context.Context, HostKey) (bool, error) {
		confirmed++
		return false, nil
	}
	opts := Options{
		Keys:     testSignerKeySource{clientSigner},
		HostKeys: NewKnownHosts(knownHostsPath, confirm),
	}
	session, err := Dial(t.Context(), "ssh://"+addr+"/repo.git", UploadPack, opts)
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()

	_, err = session.Advertise(t.Context())
	if !errors.Is(err, ErrHostKeyRejected) {
		t.Fatalf("Advertise returned %v, want ErrHostKeyRejected", err)
	}
	if confirmed != 1 {
		t.Fatalf("confirm was called %d times, want 1", confirmed)
	}
	if _, statErr := os.Stat(knownHostsPath); statErr == nil {
		t.Fatalf("known hosts file was created despite rejection")
	}
}

func TestSSHAdvertiseAcceptsUnknownHostKeyAndPersistsIt(t *testing.T) {
	hostSigner := generateSSHSigner(t)
	clientSigner := generateSSHSigner(t)
	adv := gitV1AdvertisementBytes(testHeadCaps, [][2]string{{idOf(1).String(), "refs/heads/main"}})

	addr := startFakeSSHServer(t, sshServerConfig{
		hostKey:     hostSigner,
		allowedKeys: []ssh.PublicKey{clientSigner.PublicKey()},
		handle:      sshAdvertiseOnlyHandler(sshExecCommand(UploadPack, "/repo.git"), adv),
	})

	knownHostsPath := filepath.Join(t.TempDir(), "known_hosts")
	opts := Options{
		Keys:     testSignerKeySource{clientSigner},
		HostKeys: NewKnownHosts(knownHostsPath, fixedConfirm(true)),
	}
	session, err := Dial(t.Context(), "ssh://"+addr+"/repo.git", UploadPack, opts)
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	if _, err := session.Advertise(t.Context()); err != nil {
		t.Fatalf("Advertise returned error %v", err)
	}
	if err := session.Close(); err != nil {
		t.Fatalf("Close returned error %v", err)
	}

	data, err := os.ReadFile(knownHostsPath)
	if err != nil {
		t.Fatalf("ReadFile returned error %v", err)
	}
	want := sshKnownHostsLine(addr, hostSigner.PublicKey())
	if !bytes.Contains(data, []byte(want)) {
		t.Fatalf("known_hosts contents %q do not contain %q", data, want)
	}

	secondOpts := Options{
		Keys:     testSignerKeySource{clientSigner},
		HostKeys: NewKnownHosts(knownHostsPath, neverConfirm(t)),
	}
	secondSession, err := Dial(t.Context(), "ssh://"+addr+"/repo.git", UploadPack, secondOpts)
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = secondSession.Close() }()
	if _, err := secondSession.Advertise(t.Context()); err != nil {
		t.Fatalf("second Advertise returned error %v (host key should already be trusted)", err)
	}
}

func TestSSHAdvertiseFailsWhenHostKeyChanged(t *testing.T) {
	hostSigner := generateSSHSigner(t)
	decoySigner := generateSSHSigner(t)
	clientSigner := generateSSHSigner(t)
	adv := gitV1AdvertisementBytes(testHeadCaps, [][2]string{{idOf(1).String(), "refs/heads/main"}})

	addr := startFakeSSHServer(t, sshServerConfig{
		hostKey:     hostSigner,
		allowedKeys: []ssh.PublicKey{clientSigner.PublicKey()},
		handle:      sshUploadPackHandler(t, adv, 0, nil),
	})

	knownHostsPath := writeKnownHostsFile(t, addr, decoySigner.PublicKey())
	opts := Options{
		Keys:     testSignerKeySource{clientSigner},
		HostKeys: NewKnownHosts(knownHostsPath, neverConfirm(t)),
	}
	session, err := Dial(t.Context(), "ssh://"+addr+"/repo.git", UploadPack, opts)
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()
	_, err = session.Advertise(t.Context())
	if !errors.Is(err, ErrHostKeyChanged) {
		t.Fatalf("Advertise returned %v, want ErrHostKeyChanged", err)
	}
}

func TestSSHFetchWithPassphraseProtectedKey(t *testing.T) {
	hostSigner := generateSSHSigner(t)
	clientPriv, clientSigner := generateEd25519(t)
	pack := fakePack(41)
	adv := gitV1AdvertisementBytes(testHeadCaps, [][2]string{{idOf(1).String(), "refs/heads/main"}})
	addr := startFakeSSHServer(t, sshServerConfig{
		hostKey:     hostSigner,
		allowedKeys: []ssh.PublicKey{clientSigner.PublicKey()},
		handle:      sshUploadPackHandler(t, adv, 0, pack),
	})

	dir := t.TempDir()
	writeKeyFile(t, dir, "id_ed25519", clientPriv, []byte("correct-passphrase"))

	keys, err := NewDirKeys(dir).Keys(t.Context(), "irrelevant")
	if err != nil {
		t.Fatalf("Keys returned error %v", err)
	}
	for i := range keys {
		keys[i].Passphrase = []byte("correct-passphrase")
	}
	opts := Options{
		Keys:     fixedKeySource{keys: keys},
		HostKeys: NewKnownHosts(writeKnownHostsFile(t, addr, hostSigner.PublicKey()), neverConfirm(t)),
	}
	session, err := Dial(t.Context(), "ssh://"+addr+"/repo.git", UploadPack, opts)
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()
	resp, err := session.Fetch(t.Context(), FetchRequest{Wants: []hash.ObjectID{idOf(1)}}, nil)
	if err != nil {
		t.Fatalf("Fetch returned error %v", err)
	}
	defer func() { _ = resp.Pack.Close() }()
	got, err := io.ReadAll(resp.Pack)
	if err != nil {
		t.Fatalf("reading Pack returned error %v", err)
	}
	if string(got) != string(pack) {
		t.Fatalf("Pack = %x, want %x", got, pack)
	}
}

func TestSSHFetchWithWrongPassphraseFailsWithClearError(t *testing.T) {
	hostSigner := generateSSHSigner(t)
	clientPriv, clientSigner := generateEd25519(t)
	addr := startFakeSSHServer(t, sshServerConfig{
		hostKey:     hostSigner,
		allowedKeys: []ssh.PublicKey{clientSigner.PublicKey()},
		handle:      func(*testing.T, ssh.Channel, string) {},
	})

	dir := t.TempDir()
	path := writeKeyFile(t, dir, "id_ed25519", clientPriv, []byte("correct-passphrase"))
	keys, err := NewDirKeys(dir).Keys(t.Context(), "irrelevant")
	if err != nil {
		t.Fatalf("Keys returned error %v", err)
	}
	for i := range keys {
		keys[i].Passphrase = []byte("wrong-passphrase")
	}
	opts := Options{
		Keys:     fixedKeySource{keys: keys},
		HostKeys: NewKnownHosts(writeKnownHostsFile(t, addr, hostSigner.PublicKey()), neverConfirm(t)),
	}
	session, err := Dial(t.Context(), "ssh://"+addr+"/repo.git", UploadPack, opts)
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()
	_, err = session.Advertise(t.Context())
	if err == nil {
		t.Fatalf("Advertise succeeded, want a decrypt error")
	}
	if strings.Contains(err.Error(), "correct-passphrase") || strings.Contains(err.Error(), "wrong-passphrase") {
		t.Fatalf("error leaked the passphrase: %v", err)
	}
	if !strings.Contains(err.Error(), filepath.Base(path)) {
		t.Fatalf("error %q does not mention the key file %q", err, filepath.Base(path))
	}
}

func TestSSHDialFailsWithErrNoKeysWhenNoKeysAreConfigured(t *testing.T) {
	dir := t.TempDir()
	opts := Options{
		Keys:     NewDirKeys(dir),
		HostKeys: NewKnownHosts(filepath.Join(dir, "known_hosts"), neverConfirm(t)),
	}
	session, err := Dial(t.Context(), "ssh://127.0.0.1:1/repo.git", UploadPack, opts)
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()
	_, err = session.Advertise(t.Context())
	if !errors.Is(err, ErrNoKeys) {
		t.Fatalf("Advertise returned %v, want ErrNoKeys", err)
	}
}

func TestSSHDialFailsWithErrNoKeysWhenKeySourceIsNil(t *testing.T) {
	session, err := Dial(t.Context(), "ssh://127.0.0.1:1/repo.git", UploadPack, Options{})
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()
	_, err = session.Advertise(t.Context())
	if !errors.Is(err, ErrNoKeys) {
		t.Fatalf("Advertise returned %v, want ErrNoKeys", err)
	}
}

func TestSSHConnectFailsWhenHostKeyPolicyIsNil(t *testing.T) {
	hostSigner := generateSSHSigner(t)
	clientSigner := generateSSHSigner(t)
	addr := startFakeSSHServer(t, sshServerConfig{
		hostKey:     hostSigner,
		allowedKeys: []ssh.PublicKey{clientSigner.PublicKey()},
		handle:      func(*testing.T, ssh.Channel, string) {},
	})
	opts := Options{Keys: testSignerKeySource{clientSigner}}
	session, err := Dial(t.Context(), "ssh://"+addr+"/repo.git", UploadPack, opts)
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()
	_, err = session.Advertise(t.Context())
	if !errors.Is(err, ErrHostKeyRejected) {
		t.Fatalf("Advertise returned %v, want ErrHostKeyRejected", err)
	}
}

func TestSSHOperationsFailAfterClose(t *testing.T) {
	hostSigner := generateSSHSigner(t)
	clientSigner := generateSSHSigner(t)
	addr := startFakeSSHServer(t, sshServerConfig{
		hostKey:     hostSigner,
		allowedKeys: []ssh.PublicKey{clientSigner.PublicKey()},
		handle:      func(*testing.T, ssh.Channel, string) {},
	})
	opts := Options{
		Keys:     testSignerKeySource{clientSigner},
		HostKeys: NewKnownHosts(writeKnownHostsFile(t, addr, hostSigner.PublicKey()), neverConfirm(t)),
	}
	session, err := Dial(t.Context(), "ssh://"+addr+"/repo.git", UploadPack, opts)
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	if err := session.Close(); err != nil {
		t.Fatalf("Close returned error %v", err)
	}
	if err := session.Close(); err != nil {
		t.Fatalf("second Close returned error %v, want nil", err)
	}
	if _, err := session.Advertise(t.Context()); !errors.Is(err, ErrProtocol) {
		t.Fatalf("Advertise returned %v, want ErrProtocol", err)
	}
	if _, err := session.Fetch(t.Context(), FetchRequest{}, nil); !errors.Is(err, ErrProtocol) {
		t.Fatalf("Fetch returned %v, want ErrProtocol", err)
	}
	if _, err := session.Push(t.Context(), PushRequest{}); !errors.Is(err, ErrProtocol) {
		t.Fatalf("Push returned %v, want ErrProtocol", err)
	}
}

func TestSSHAdvertiseThenFetchReusesTheSameSession(t *testing.T) {
	hostSigner := generateSSHSigner(t)
	clientSigner := generateSSHSigner(t)
	pack := fakePack(61)
	adv := gitV1AdvertisementBytes(testHeadCaps, [][2]string{{idOf(1).String(), "refs/heads/main"}})
	addr := startFakeSSHServer(t, sshServerConfig{
		hostKey:     hostSigner,
		allowedKeys: []ssh.PublicKey{clientSigner.PublicKey()},
		handle:      sshUploadPackHandler(t, adv, 0, pack),
	})
	opts := Options{
		Keys:     testSignerKeySource{clientSigner},
		HostKeys: NewKnownHosts(writeKnownHostsFile(t, addr, hostSigner.PublicKey()), neverConfirm(t)),
	}
	session, err := Dial(t.Context(), "ssh://"+addr+"/repo.git", UploadPack, opts)
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()
	if _, err := session.Advertise(t.Context()); err != nil {
		t.Fatalf("Advertise returned error %v", err)
	}
	resp, err := session.Fetch(t.Context(), FetchRequest{Wants: []hash.ObjectID{idOf(1)}}, nil)
	if err != nil {
		t.Fatalf("Fetch returned error %v", err)
	}
	defer func() { _ = resp.Pack.Close() }()
	got, err := io.ReadAll(resp.Pack)
	if err != nil {
		t.Fatalf("reading Pack returned error %v", err)
	}
	if string(got) != string(pack) {
		t.Fatalf("Pack = %x, want %x", got, pack)
	}
}

func TestSSHFetchCanceledByContext(t *testing.T) {
	hostSigner := generateSSHSigner(t)
	clientSigner := generateSSHSigner(t)
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })
	addr := startFakeSSHServer(t, sshServerConfig{
		hostKey:     hostSigner,
		allowedKeys: []ssh.PublicKey{clientSigner.PublicKey()},
		handle: func(t *testing.T, ch ssh.Channel, cmd string) {
			<-block
		},
	})
	opts := Options{
		Keys:     testSignerKeySource{clientSigner},
		HostKeys: NewKnownHosts(writeKnownHostsFile(t, addr, hostSigner.PublicKey()), neverConfirm(t)),
	}
	session, err := Dial(t.Context(), "ssh://"+addr+"/repo.git", UploadPack, opts)
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()

	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	_, err = session.Advertise(ctx)
	if err == nil {
		t.Fatalf("Advertise succeeded, want a context error")
	}
}

func TestSSHPushSucceeds(t *testing.T) {
	hostSigner := generateSSHSigner(t)
	clientSigner := generateSSHSigner(t)
	pack := fakePack(53)
	addr := startFakeSSHServer(t, sshServerConfig{
		hostKey:     hostSigner,
		allowedKeys: []ssh.PublicKey{clientSigner.PublicKey()},
		handle: func(t *testing.T, ch ssh.Channel, cmd string) {
			wantCmd := sshExecCommand(ReceivePack, "/repo.git")
			if cmd != wantCmd {
				t.Errorf("cmd = %q, want %q", cmd, wantCmd)
				return
			}
			if _, err := ch.Write(gitV1AdvertisementBytes(testPushHeadCaps, [][2]string{{idOf(1).String(), "refs/heads/main"}})); err != nil {
				t.Errorf("Write returned error %v", err)
				return
			}
			dec := NewDecoder(ch)
			if _, typ, err := readOnePktLine(dec); err != nil || typ != PktData {
				t.Errorf("expected an update command, err=%v", err)
				return
			}
			for {
				_, typ, err := readOnePktLine(dec)
				if err != nil {
					t.Errorf("readOnePktLine returned error %v", err)
					return
				}
				if typ == PktFlush {
					break
				}
			}
			gotPack := make([]byte, len(pack))
			if _, err := io.ReadFull(ch, gotPack); err != nil {
				t.Errorf("reading pack returned error %v", err)
				return
			}
			if string(gotPack) != string(pack) {
				t.Errorf("pack = %x, want %x", gotPack, pack)
			}
			resp := reportStatusBody("ok", []string{"ok refs/heads/main"})
			if _, err := ch.Write(resp); err != nil {
				t.Errorf("Write returned error %v", err)
			}
		},
	})
	opts := Options{
		Keys:     testSignerKeySource{clientSigner},
		HostKeys: NewKnownHosts(writeKnownHostsFile(t, addr, hostSigner.PublicKey()), neverConfirm(t)),
	}
	session, err := Dial(t.Context(), "ssh://"+addr+"/repo.git", ReceivePack, opts)
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()
	result, err := session.Push(t.Context(), PushRequest{
		Updates: []Update{{Name: "refs/heads/main", Old: hash.Zero, New: idOf(1)}},
		Pack:    bytes.NewReader(pack),
	})
	if err != nil {
		t.Fatalf("Push returned error %v", err)
	}
	if !result.UnpackOK {
		t.Fatalf("UnpackOK = false, want true")
	}
	if len(result.Refs) != 1 || !result.Refs[0].OK {
		t.Fatalf("Refs = %v, want one ok ref", result.Refs)
	}
}

func TestDialSupportsSCPLikeURLAgainstFakeServer(t *testing.T) {
	hostSigner := generateSSHSigner(t)
	clientSigner := generateSSHSigner(t)
	adv := gitV1AdvertisementBytes(testHeadCaps, [][2]string{{idOf(1).String(), "refs/heads/main"}})
	addr := startFakeSSHServer(t, sshServerConfig{
		hostKey:     hostSigner,
		allowedKeys: []ssh.PublicKey{clientSigner.PublicKey()},
		handle:      sshAdvertiseOnlyHandler(sshExecCommand(UploadPack, "/repo.git"), adv),
	})
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("SplitHostPort returned error %v", err)
	}
	opts := Options{
		Keys:     testSignerKeySource{clientSigner},
		HostKeys: NewKnownHosts(writeKnownHostsFile(t, addr, hostSigner.PublicKey()), neverConfirm(t)),
	}
	endpoint := Endpoint{Scheme: SchemeSSH, Host: host, Port: port, Path: "/repo.git"}
	session := newSSHSession(endpoint, UploadPack, opts)
	defer func() { _ = session.Close() }()
	if _, err := session.Advertise(t.Context()); err != nil {
		t.Fatalf("Advertise returned error %v", err)
	}
}

func TestSignerFromKeyRejectsEmptyPrivate(t *testing.T) {
	_, err := signerFromKey(Key{Path: "empty"})
	if !errors.Is(err, ErrNoKeys) {
		t.Fatalf("signerFromKey returned %v, want ErrNoKeys", err)
	}
}

func TestSignerFromKeyRejectsGarbagePrivate(t *testing.T) {
	_, err := signerFromKey(Key{Path: "garbage", Private: []byte("not a key")})
	if err == nil {
		t.Fatalf("signerFromKey succeeded, want an error")
	}
}

func TestSignerFromKeyRejectsEncryptedKeyWithoutPassphrase(t *testing.T) {
	priv, _ := generateEd25519(t)
	encoded := encodePrivateKey(t, priv, []byte("super-secret-passphrase"))
	_, err := signerFromKey(Key{Path: "enc", Private: encoded})
	if err == nil {
		t.Fatalf("signerFromKey succeeded, want an error since no passphrase was supplied")
	}
	if strings.Contains(err.Error(), "super-secret-passphrase") {
		t.Fatalf("error leaked the passphrase: %v", err)
	}
}

func TestSSHUsernameFallsBackToEmptyWhenCurrentUserFails(t *testing.T) {
	restore := currentUser
	currentUser = func() (*user.User, error) { return nil, fmt.Errorf("boom") }
	t.Cleanup(func() { currentUser = restore })
	if got := sshUsername(""); got != "" {
		t.Fatalf("sshUsername = %q, want empty string", got)
	}
}

func TestSignersFromKeysReturnsErrNoKeysForEmptySlice(t *testing.T) {
	_, err := signersFromKeys(nil)
	if !errors.Is(err, ErrNoKeys) {
		t.Fatalf("signersFromKeys returned %v, want ErrNoKeys", err)
	}
}

func TestGatherSignersReturnsErrNoKeysForNilSource(t *testing.T) {
	_, err := gatherSigners(t.Context(), "host", nil)
	if !errors.Is(err, ErrNoKeys) {
		t.Fatalf("gatherSigners returned %v, want ErrNoKeys", err)
	}
}

func TestGatherSignersPropagatesKeySourceError(t *testing.T) {
	wantErr := fmt.Errorf("boom")
	_, err := gatherSigners(t.Context(), "host", erroringKeySource{err: wantErr})
	if !errors.Is(err, wantErr) {
		t.Fatalf("gatherSigners returned %v, want %v", err, wantErr)
	}
}

func TestMultiKeysCombinesMultipleSources(t *testing.T) {
	signerA := generateSSHSigner(t)
	signerB := generateSSHSigner(t)
	source := MultiKeys(testSignerKeySource{signerA}, testSignerKeySource{signerB})
	signers, err := gatherSigners(t.Context(), "host", source)
	if err != nil {
		t.Fatalf("gatherSigners returned error %v", err)
	}
	if len(signers) != 2 {
		t.Fatalf("len(signers) = %d, want 2", len(signers))
	}
}

func TestMultiKeysSkipsNilSourcesAndFailingSources(t *testing.T) {
	good := generateSSHSigner(t)
	source := MultiKeys(nil, erroringKeySource{err: fmt.Errorf("boom")}, testSignerKeySource{good})
	signers, err := gatherSigners(t.Context(), "host", source)
	if err != nil {
		t.Fatalf("gatherSigners returned error %v", err)
	}
	if len(signers) != 1 {
		t.Fatalf("len(signers) = %d, want 1", len(signers))
	}
}

func TestMultiKeysReturnsErrNoKeysWhenAllSourcesEmpty(t *testing.T) {
	source := MultiKeys(fixedKeySource{}, fixedKeySource{})
	_, err := gatherSigners(t.Context(), "host", source)
	if !errors.Is(err, ErrNoKeys) {
		t.Fatalf("gatherSigners returned %v, want ErrNoKeys", err)
	}
}

func TestMultiKeysKeysAggregatesAndIgnoresFailingSources(t *testing.T) {
	source := MultiKeys(
		nil,
		fixedKeySource{keys: []Key{{Path: "a"}}},
		erroringKeySource{err: fmt.Errorf("boom")},
		fixedKeySource{keys: []Key{{Path: "b"}}},
	)
	keys, err := source.Keys(t.Context(), "host")
	if err != nil {
		t.Fatalf("Keys returned error %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("len(keys) = %d, want 2", len(keys))
	}
}

func TestMultiKeysListSignersReturnsErrNoKeysWhenAllSourcesNil(t *testing.T) {
	source := MultiKeys(nil, nil)
	_, err := gatherSigners(t.Context(), "host", source)
	if !errors.Is(err, ErrNoKeys) {
		t.Fatalf("gatherSigners returned %v, want ErrNoKeys", err)
	}
}

func TestMultiKeysKeysPropagatesErrorWhenEverySourceFails(t *testing.T) {
	wantErr := fmt.Errorf("boom")
	source := MultiKeys(erroringKeySource{err: wantErr})
	_, err := source.Keys(t.Context(), "host")
	if !errors.Is(err, wantErr) {
		t.Fatalf("Keys returned %v, want %v", err, wantErr)
	}
}

func TestNewDirKeysReadsConventionalKeyNames(t *testing.T) {
	dir := t.TempDir()
	priv, _ := generateEd25519(t)
	writeKeyFile(t, dir, "id_ed25519", priv, nil)
	if err := os.WriteFile(filepath.Join(dir, "id_ed25519.pub"), []byte("ignored"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error %v", err)
	}
	keys, err := NewDirKeys(dir).Keys(t.Context(), "host")
	if err != nil {
		t.Fatalf("Keys returned error %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("len(keys) = %d, want 1", len(keys))
	}
	if keys[0].Path != filepath.Join(dir, "id_ed25519") {
		t.Fatalf("Path = %q", keys[0].Path)
	}
}

func TestNewDirKeysReturnsEmptyForMissingDirectory(t *testing.T) {
	keys, err := NewDirKeys(filepath.Join(t.TempDir(), "missing")).Keys(t.Context(), "host")
	if err != nil {
		t.Fatalf("Keys returned error %v", err)
	}
	if len(keys) != 0 {
		t.Fatalf("len(keys) = %d, want 0", len(keys))
	}
}

func TestNewDirKeysRespectsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := NewDirKeys(t.TempDir()).Keys(ctx, "host")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Keys returned %v, want context.Canceled", err)
	}
}

func TestAgentKeysUsesInjectedDialerForSigners(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("ed25519.GenerateKey returned error %v", err)
	}
	keyring := agent.NewKeyring()
	if err := keyring.Add(agent.AddedKey{PrivateKey: priv}); err != nil {
		t.Fatalf("Add returned error %v", err)
	}

	restore := agentDialer
	agentDialer = func() (io.ReadWriteCloser, error) {
		serverEnd, clientEnd := net.Pipe()
		go func() { _ = agent.ServeAgent(keyring, serverEnd) }()
		return clientEnd, nil
	}
	t.Cleanup(func() { agentDialer = restore })

	signers, err := NewAgentKeys().(signerLister).listSigners(t.Context(), "host")
	if err != nil {
		t.Fatalf("listSigners returned error %v", err)
	}
	if len(signers) != 1 {
		t.Fatalf("len(signers) = %d, want 1", len(signers))
	}

	keys, err := NewAgentKeys().Keys(t.Context(), "host")
	if err != nil {
		t.Fatalf("Keys returned error %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("len(keys) = %d, want 1", len(keys))
	}
}

func TestAgentKeysReturnsErrNoKeysWhenDialerFails(t *testing.T) {
	restore := agentDialer
	agentDialer = func() (io.ReadWriteCloser, error) { return nil, fmt.Errorf("no agent") }
	t.Cleanup(func() { agentDialer = restore })

	_, err := NewAgentKeys().Keys(t.Context(), "host")
	if !errors.Is(err, ErrNoKeys) {
		t.Fatalf("Keys returned %v, want ErrNoKeys", err)
	}
}

func TestAgentKeysReturnsErrNoKeysWhenAgentHasNoIdentities(t *testing.T) {
	serverEnd, clientEnd := net.Pipe()
	t.Cleanup(func() { _ = serverEnd.Close() })
	go func() { _ = agent.ServeAgent(agent.NewKeyring(), serverEnd) }()

	restore := agentDialer
	agentDialer = func() (io.ReadWriteCloser, error) { return clientEnd, nil }
	t.Cleanup(func() { agentDialer = restore })

	_, err := NewAgentKeys().Keys(t.Context(), "host")
	if !errors.Is(err, ErrNoKeys) {
		t.Fatalf("Keys returned %v, want ErrNoKeys", err)
	}
}

func TestAgentKeysRespectsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := NewAgentKeys().Keys(ctx, "host")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Keys returned %v, want context.Canceled", err)
	}
}

func TestShellQuoteSingleEscapesQuotes(t *testing.T) {
	got := shellQuoteSingle("/repo's/path.git")
	want := `'/repo'\''s/path.git'`
	if got != want {
		t.Fatalf("shellQuoteSingle = %q, want %q", got, want)
	}
}

func TestSSHExecCommandBuildsGitCommandLine(t *testing.T) {
	got := sshExecCommand(UploadPack, "/repo.git")
	want := "git-upload-pack '/repo.git'"
	if got != want {
		t.Fatalf("sshExecCommand = %q, want %q", got, want)
	}
}

func TestSSHUsernamePrefersExplicitValue(t *testing.T) {
	if got := sshUsername("explicit"); got != "explicit" {
		t.Fatalf("sshUsername = %q, want %q", got, "explicit")
	}
}

func TestSSHConnectDefaultsToPort22AndPropagatesDialError(t *testing.T) {
	restore := dialSSHTCP
	var gotNetwork, gotAddr string
	wantErr := fmt.Errorf("boom")
	dialSSHTCP = func(_ context.Context, network, addr string) (net.Conn, error) {
		gotNetwork, gotAddr = network, addr
		return nil, wantErr
	}
	t.Cleanup(func() { dialSSHTCP = restore })

	opts := Options{
		Keys:     testSignerKeySource{generateSSHSigner(t)},
		HostKeys: NewKnownHosts(filepath.Join(t.TempDir(), "known_hosts"), neverConfirm(t)),
	}
	endpoint := Endpoint{Scheme: SchemeSSH, Host: "example.com", Path: "/repo.git"}
	session := newSSHSession(endpoint, UploadPack, opts)
	defer func() { _ = session.Close() }()

	_, err := session.Advertise(t.Context())
	if !errors.Is(err, wantErr) {
		t.Fatalf("Advertise returned %v, want %v", err, wantErr)
	}
	if gotNetwork != "tcp" {
		t.Fatalf("network = %q, want tcp", gotNetwork)
	}
	if gotAddr != "example.com:22" {
		t.Fatalf("addr = %q, want example.com:22 (default port)", gotAddr)
	}
}

func TestSSHConnectFailsWhenServerRejectsSessionChannel(t *testing.T) {
	hostSigner := generateSSHSigner(t)
	clientSigner := generateSSHSigner(t)
	addr := startFakeSSHServer(t, sshServerConfig{
		hostKey:       hostSigner,
		allowedKeys:   []ssh.PublicKey{clientSigner.PublicKey()},
		rejectSession: true,
	})
	opts := Options{
		Keys:     testSignerKeySource{clientSigner},
		HostKeys: NewKnownHosts(writeKnownHostsFile(t, addr, hostSigner.PublicKey()), neverConfirm(t)),
	}
	session, err := Dial(t.Context(), "ssh://"+addr+"/repo.git", UploadPack, opts)
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()
	if _, err := session.Advertise(t.Context()); err == nil {
		t.Fatalf("Advertise succeeded, want an error since the server rejects session channels")
	}
}

func TestSSHConnectFailsWhenServerRejectsExecRequest(t *testing.T) {
	hostSigner := generateSSHSigner(t)
	clientSigner := generateSSHSigner(t)
	addr := startFakeSSHServer(t, sshServerConfig{
		hostKey:     hostSigner,
		allowedKeys: []ssh.PublicKey{clientSigner.PublicKey()},
		rejectExec:  true,
	})
	opts := Options{
		Keys:     testSignerKeySource{clientSigner},
		HostKeys: NewKnownHosts(writeKnownHostsFile(t, addr, hostSigner.PublicKey()), neverConfirm(t)),
	}
	session, err := Dial(t.Context(), "ssh://"+addr+"/repo.git", UploadPack, opts)
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()
	if _, err := session.Advertise(t.Context()); err == nil {
		t.Fatalf("Advertise succeeded, want an error since the server rejects the exec request")
	}
}

func TestSSHAdvertiseFailsWithProtocolErrorForMalformedAdvertisement(t *testing.T) {
	hostSigner := generateSSHSigner(t)
	clientSigner := generateSSHSigner(t)
	addr := startFakeSSHServer(t, sshServerConfig{
		hostKey:     hostSigner,
		allowedKeys: []ssh.PublicKey{clientSigner.PublicKey()},
		handle: func(t *testing.T, ch ssh.Channel, cmd string) {
			if _, err := ch.Write([]byte("not a pkt-line advertisement at all\n")); err != nil {
				t.Errorf("Write returned error %v", err)
			}
		},
	})
	opts := Options{
		Keys:     testSignerKeySource{clientSigner},
		HostKeys: NewKnownHosts(writeKnownHostsFile(t, addr, hostSigner.PublicKey()), neverConfirm(t)),
	}
	session, err := Dial(t.Context(), "ssh://"+addr+"/repo.git", UploadPack, opts)
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()
	_, err = session.Advertise(t.Context())
	if err == nil {
		t.Fatalf("Advertise succeeded, want a parse error")
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Advertise returned a context error %v, want a protocol parse error", err)
	}
}

func TestSSHAdvertiseV2ListsRefsThroughLsRefs(t *testing.T) {
	hostSigner := generateSSHSigner(t)
	clientSigner := generateSSHSigner(t)
	addr := startFakeSSHServer(t, sshServerConfig{
		hostKey:     hostSigner,
		allowedKeys: []ssh.PublicKey{clientSigner.PublicKey()},
		handle: func(t *testing.T, ch ssh.Channel, cmd string) {
			wantCmd := sshExecCommand(UploadPack, "/repo.git")
			if cmd != wantCmd {
				t.Errorf("cmd = %q, want %q", cmd, wantCmd)
				return
			}
			if _, err := ch.Write(gitV2AdvertisementBytes([]string{"ls-refs", "fetch=shallow", "agent=git/test"})); err != nil {
				t.Errorf("Write returned error %v", err)
				return
			}
			dec := NewDecoder(ch)
			line, typ, err := readOnePktLine(dec)
			if err != nil || typ != PktData || line != "command=ls-refs" {
				t.Errorf("expected a ls-refs command, got %q type %v err %v", line, typ, err)
				return
			}
			for {
				_, typ, err := readOnePktLine(dec)
				if err != nil {
					t.Errorf("readOnePktLine returned error %v", err)
					return
				}
				if typ == PktFlush {
					break
				}
			}
			resp := newPktBuilder().
				line(idOf(1).String() + " HEAD symref-target:refs/heads/main\n").
				line(idOf(1).String() + " refs/heads/main\n").
				flush().bytes()
			if _, err := ch.Write(resp); err != nil {
				t.Errorf("Write returned error %v", err)
			}
		},
	})
	opts := Options{
		Keys:     testSignerKeySource{clientSigner},
		HostKeys: NewKnownHosts(writeKnownHostsFile(t, addr, hostSigner.PublicKey()), neverConfirm(t)),
	}
	session, err := Dial(t.Context(), "ssh://"+addr+"/repo.git", UploadPack, opts)
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()
	adv, err := session.Advertise(t.Context())
	if err != nil {
		t.Fatalf("Advertise returned error %v", err)
	}
	if adv.Version != 2 {
		t.Fatalf("Version = %d, want 2", adv.Version)
	}
	if len(adv.Refs) != 2 || adv.Head != "refs/heads/main" {
		t.Fatalf("Refs/Head = %v/%q, want two refs and head refs/heads/main", adv.Refs, adv.Head)
	}
}

func TestSSHFetchV2SendsPackfileAfterDone(t *testing.T) {
	hostSigner := generateSSHSigner(t)
	clientSigner := generateSSHSigner(t)
	pack := fakePack(71)
	addr := startFakeSSHServer(t, sshServerConfig{
		hostKey:     hostSigner,
		allowedKeys: []ssh.PublicKey{clientSigner.PublicKey()},
		handle: func(t *testing.T, ch ssh.Channel, cmd string) {
			if _, err := ch.Write(gitV2AdvertisementBytes([]string{"ls-refs", "fetch=shallow"})); err != nil {
				t.Errorf("Write returned error %v", err)
				return
			}
			dec := NewDecoder(ch)
			if _, typ, err := readOnePktLine(dec); err != nil || typ != PktData {
				t.Errorf("expected a ls-refs command, err=%v", err)
				return
			}
			for {
				_, typ, err := readOnePktLine(dec)
				if err != nil {
					t.Errorf("readOnePktLine returned error %v", err)
					return
				}
				if typ == PktFlush {
					break
				}
			}
			if _, err := ch.Write(newPktBuilder().line(idOf(1).String() + " refs/heads/main\n").flush().bytes()); err != nil {
				t.Errorf("Write returned error %v", err)
				return
			}
			if _, typ, err := readOnePktLine(dec); err != nil || typ != PktData {
				t.Errorf("expected a fetch command, err=%v", err)
				return
			}
			var sawDone bool
			for {
				line, typ, err := readOnePktLine(dec)
				if err != nil {
					t.Errorf("readOnePktLine returned error %v", err)
					return
				}
				if typ == PktFlush {
					break
				}
				if line == "done" {
					sawDone = true
				}
			}
			if !sawDone {
				t.Errorf("fetch command never sent done")
				return
			}
			if _, err := ch.Write(fetchV2PackfileBody(pack)); err != nil {
				t.Errorf("Write returned error %v", err)
			}
		},
	})
	opts := Options{
		Keys:     testSignerKeySource{clientSigner},
		HostKeys: NewKnownHosts(writeKnownHostsFile(t, addr, hostSigner.PublicKey()), neverConfirm(t)),
	}
	session, err := Dial(t.Context(), "ssh://"+addr+"/repo.git", UploadPack, opts)
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()
	resp, err := session.Fetch(t.Context(), FetchRequest{Wants: []hash.ObjectID{idOf(1)}}, nil)
	if err != nil {
		t.Fatalf("Fetch returned error %v", err)
	}
	defer func() { _ = resp.Pack.Close() }()
	got, err := io.ReadAll(resp.Pack)
	if err != nil {
		t.Fatalf("reading Pack returned error %v", err)
	}
	if string(got) != string(pack) {
		t.Fatalf("Pack = %x, want %x", got, pack)
	}
}

func TestSSHFetchFailsWithErrNoKeysWithoutPriorAdvertise(t *testing.T) {
	dir := t.TempDir()
	opts := Options{
		Keys:     NewDirKeys(dir),
		HostKeys: NewKnownHosts(filepath.Join(dir, "known_hosts"), neverConfirm(t)),
	}
	session, err := Dial(t.Context(), "ssh://127.0.0.1:1/repo.git", UploadPack, opts)
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()
	_, err = session.Fetch(t.Context(), FetchRequest{}, nil)
	if !errors.Is(err, ErrNoKeys) {
		t.Fatalf("Fetch returned %v, want ErrNoKeys", err)
	}
}

func TestSSHPushFailsWithErrNoKeysWithoutPriorAdvertise(t *testing.T) {
	dir := t.TempDir()
	opts := Options{
		Keys:     NewDirKeys(dir),
		HostKeys: NewKnownHosts(filepath.Join(dir, "known_hosts"), neverConfirm(t)),
	}
	session, err := Dial(t.Context(), "ssh://127.0.0.1:1/repo.git", ReceivePack, opts)
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()
	_, err = session.Push(t.Context(), PushRequest{})
	if !errors.Is(err, ErrNoKeys) {
		t.Fatalf("Push returned %v, want ErrNoKeys", err)
	}
}

func TestSSHFetchFailsWhenAdvertisementIsMalformed(t *testing.T) {
	hostSigner := generateSSHSigner(t)
	clientSigner := generateSSHSigner(t)
	addr := startFakeSSHServer(t, sshServerConfig{
		hostKey:     hostSigner,
		allowedKeys: []ssh.PublicKey{clientSigner.PublicKey()},
		handle: func(t *testing.T, ch ssh.Channel, cmd string) {
			if _, err := ch.Write([]byte("garbage\n")); err != nil {
				t.Errorf("Write returned error %v", err)
			}
		},
	})
	opts := Options{
		Keys:     testSignerKeySource{clientSigner},
		HostKeys: NewKnownHosts(writeKnownHostsFile(t, addr, hostSigner.PublicKey()), neverConfirm(t)),
	}
	session, err := Dial(t.Context(), "ssh://"+addr+"/repo.git", UploadPack, opts)
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()
	_, err = session.Fetch(t.Context(), FetchRequest{Wants: []hash.ObjectID{idOf(1)}}, nil)
	if err == nil {
		t.Fatalf("Fetch succeeded, want a parse error")
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Fetch returned a context error %v, want a protocol parse error", err)
	}
}

func TestSSHPushFailsWhenAdvertisementIsMalformed(t *testing.T) {
	hostSigner := generateSSHSigner(t)
	clientSigner := generateSSHSigner(t)
	addr := startFakeSSHServer(t, sshServerConfig{
		hostKey:     hostSigner,
		allowedKeys: []ssh.PublicKey{clientSigner.PublicKey()},
		handle: func(t *testing.T, ch ssh.Channel, cmd string) {
			if _, err := ch.Write([]byte("garbage\n")); err != nil {
				t.Errorf("Write returned error %v", err)
			}
		},
	})
	opts := Options{
		Keys:     testSignerKeySource{clientSigner},
		HostKeys: NewKnownHosts(writeKnownHostsFile(t, addr, hostSigner.PublicKey()), neverConfirm(t)),
	}
	session, err := Dial(t.Context(), "ssh://"+addr+"/repo.git", ReceivePack, opts)
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()
	_, err = session.Push(t.Context(), PushRequest{
		Updates: []Update{{Name: "refs/heads/main", Old: hash.Zero, New: idOf(1)}},
		Pack:    bytes.NewReader(fakePack(1)),
	})
	if err == nil {
		t.Fatalf("Push succeeded, want a parse error")
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Push returned a context error %v, want a protocol parse error", err)
	}
}

func TestSSHFetchFailsWhenServerClosesChannelAfterAdvertisement(t *testing.T) {
	hostSigner := generateSSHSigner(t)
	clientSigner := generateSSHSigner(t)
	adv := gitV1AdvertisementBytes(testHeadCaps, [][2]string{{idOf(1).String(), "refs/heads/main"}})
	addr := startFakeSSHServer(t, sshServerConfig{
		hostKey:     hostSigner,
		allowedKeys: []ssh.PublicKey{clientSigner.PublicKey()},
		handle:      sshAdvertiseOnlyHandler(sshExecCommand(UploadPack, "/repo.git"), adv),
	})
	opts := Options{
		Keys:     testSignerKeySource{clientSigner},
		HostKeys: NewKnownHosts(writeKnownHostsFile(t, addr, hostSigner.PublicKey()), neverConfirm(t)),
	}
	session, err := Dial(t.Context(), "ssh://"+addr+"/repo.git", UploadPack, opts)
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()
	_, err = session.Fetch(t.Context(), FetchRequest{Wants: []hash.ObjectID{idOf(1)}}, nil)
	if err == nil {
		t.Fatalf("Fetch succeeded, want an error since the server closed the channel")
	}
}

func TestSSHPushFailsWhenServerClosesChannelAfterAdvertisement(t *testing.T) {
	hostSigner := generateSSHSigner(t)
	clientSigner := generateSSHSigner(t)
	adv := gitV1AdvertisementBytes(testPushHeadCaps, [][2]string{{idOf(1).String(), "refs/heads/main"}})
	addr := startFakeSSHServer(t, sshServerConfig{
		hostKey:     hostSigner,
		allowedKeys: []ssh.PublicKey{clientSigner.PublicKey()},
		handle:      sshAdvertiseOnlyHandler(sshExecCommand(ReceivePack, "/repo.git"), adv),
	})
	opts := Options{
		Keys:     testSignerKeySource{clientSigner},
		HostKeys: NewKnownHosts(writeKnownHostsFile(t, addr, hostSigner.PublicKey()), neverConfirm(t)),
	}
	session, err := Dial(t.Context(), "ssh://"+addr+"/repo.git", ReceivePack, opts)
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()
	_, err = session.Push(t.Context(), PushRequest{
		Updates: []Update{{Name: "refs/heads/main", Old: hash.Zero, New: idOf(1)}},
		Pack:    bytes.NewReader(fakePack(2)),
	})
	if err == nil {
		t.Fatalf("Push succeeded, want an error since the server closed the channel")
	}
}

func TestSSHFetchCanceledDuringEnsureAdvertisedFromFetch(t *testing.T) {
	hostSigner := generateSSHSigner(t)
	clientSigner := generateSSHSigner(t)
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })
	addr := startFakeSSHServer(t, sshServerConfig{
		hostKey:     hostSigner,
		allowedKeys: []ssh.PublicKey{clientSigner.PublicKey()},
		handle: func(t *testing.T, ch ssh.Channel, cmd string) {
			<-block
		},
	})
	opts := Options{
		Keys:     testSignerKeySource{clientSigner},
		HostKeys: NewKnownHosts(writeKnownHostsFile(t, addr, hostSigner.PublicKey()), neverConfirm(t)),
	}
	session, err := Dial(t.Context(), "ssh://"+addr+"/repo.git", UploadPack, opts)
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()

	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	_, err = session.Fetch(ctx, FetchRequest{Wants: []hash.ObjectID{idOf(1)}}, nil)
	if err == nil {
		t.Fatalf("Fetch succeeded, want a context error")
	}
}

func TestSSHPushCanceledDuringEnsureAdvertisedFromPush(t *testing.T) {
	hostSigner := generateSSHSigner(t)
	clientSigner := generateSSHSigner(t)
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })
	addr := startFakeSSHServer(t, sshServerConfig{
		hostKey:     hostSigner,
		allowedKeys: []ssh.PublicKey{clientSigner.PublicKey()},
		handle: func(t *testing.T, ch ssh.Channel, cmd string) {
			<-block
		},
	})
	opts := Options{
		Keys:     testSignerKeySource{clientSigner},
		HostKeys: NewKnownHosts(writeKnownHostsFile(t, addr, hostSigner.PublicKey()), neverConfirm(t)),
	}
	session, err := Dial(t.Context(), "ssh://"+addr+"/repo.git", ReceivePack, opts)
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()

	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	_, err = session.Push(ctx, PushRequest{
		Updates: []Update{{Name: "refs/heads/main", Old: hash.Zero, New: idOf(1)}},
		Pack:    bytes.NewReader(fakePack(3)),
	})
	if err == nil {
		t.Fatalf("Push succeeded, want a context error")
	}
}

func TestAgentSignersWrapsErrorWhenAgentReturnsGarbageResponse(t *testing.T) {
	serverEnd, clientEnd := net.Pipe()
	go func() {
		buf := make([]byte, 4096)
		_, _ = serverEnd.Read(buf)
		_, _ = serverEnd.Write([]byte{0x7f, 0xff, 0xff, 0xff})
		_ = serverEnd.Close()
	}()

	restore := agentDialer
	agentDialer = func() (io.ReadWriteCloser, error) { return clientEnd, nil }
	t.Cleanup(func() { agentDialer = restore })

	_, err := agentSigners(t.Context())
	if !errors.Is(err, ErrNoKeys) {
		t.Fatalf("agentSigners returned %v, want ErrNoKeys", err)
	}
}

type erroringWriteCloser struct{ err error }

func (e erroringWriteCloser) Write([]byte) (int, error) { return 0, e.err }

func (e erroringWriteCloser) Close() error { return nil }

func TestSSHRoundTripperRoundPropagatesWriteError(t *testing.T) {
	wantErr := fmt.Errorf("boom")
	s := &sshSession{stdin: erroringWriteCloser{err: wantErr}, stdout: strings.NewReader("")}
	rt := sshRoundTripper{session: s}
	_, err := rt.round(t.Context(), []byte("data"))
	if !errors.Is(err, wantErr) {
		t.Fatalf("round returned %v, want %v", err, wantErr)
	}
}

func TestSSHRoundTripperRoundReturnsContextErrorWhenCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	s := &sshSession{stdin: erroringWriteCloser{err: fmt.Errorf("boom")}, stdout: strings.NewReader("")}
	rt := sshRoundTripper{session: s}
	_, err := rt.round(ctx, []byte("data"))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("round returned %v, want context.Canceled", err)
	}
}

func TestSSHConnectReturnsContextErrorWhenHandshakeTimesOut(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen returned error %v", err)
	}
	accepted := make(chan net.Conn, 1)
	go func() {
		conn, err := ln.Accept()
		if err == nil {
			accepted <- conn
		}
	}()
	t.Cleanup(func() {
		_ = ln.Close()
		select {
		case conn := <-accepted:
			_ = conn.Close()
		case <-time.After(time.Second):
		}
	})

	opts := Options{
		Keys:     testSignerKeySource{generateSSHSigner(t)},
		HostKeys: NewKnownHosts(filepath.Join(t.TempDir(), "known_hosts"), neverConfirm(t)),
	}
	session, err := Dial(t.Context(), "ssh://"+ln.Addr().String()+"/repo.git", UploadPack, opts)
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()

	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	_, err = session.Advertise(ctx)
	if err == nil {
		t.Fatalf("Advertise succeeded, want a context error since the server never speaks SSH")
	}
}

func TestSSHAdvertiseV2FailsWhenLsRefsFails(t *testing.T) {
	hostSigner := generateSSHSigner(t)
	clientSigner := generateSSHSigner(t)
	addr := startFakeSSHServer(t, sshServerConfig{
		hostKey:     hostSigner,
		allowedKeys: []ssh.PublicKey{clientSigner.PublicKey()},
		handle:      sshAdvertiseOnlyHandler(sshExecCommand(UploadPack, "/repo.git"), gitV2AdvertisementBytes([]string{"ls-refs", "fetch=shallow"})),
	})
	opts := Options{
		Keys:     testSignerKeySource{clientSigner},
		HostKeys: NewKnownHosts(writeKnownHostsFile(t, addr, hostSigner.PublicKey()), neverConfirm(t)),
	}
	session, err := Dial(t.Context(), "ssh://"+addr+"/repo.git", UploadPack, opts)
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()
	if _, err := session.Advertise(t.Context()); err == nil {
		t.Fatalf("Advertise succeeded despite the server closing the channel before answering ls-refs")
	}
}

func TestSSHFetchCanceledByContextDuringNegotiation(t *testing.T) {
	hostSigner := generateSSHSigner(t)
	clientSigner := generateSSHSigner(t)
	adv := gitV1AdvertisementBytes(testHeadCaps, [][2]string{{idOf(1).String(), "refs/heads/main"}})
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })
	addr := startFakeSSHServer(t, sshServerConfig{
		hostKey:     hostSigner,
		allowedKeys: []ssh.PublicKey{clientSigner.PublicKey()},
		handle: func(t *testing.T, ch ssh.Channel, cmd string) {
			if _, err := ch.Write(adv); err != nil {
				t.Errorf("Write returned error %v", err)
				return
			}
			<-block
		},
	})
	opts := Options{
		Keys:     testSignerKeySource{clientSigner},
		HostKeys: NewKnownHosts(writeKnownHostsFile(t, addr, hostSigner.PublicKey()), neverConfirm(t)),
	}
	session, err := Dial(t.Context(), "ssh://"+addr+"/repo.git", UploadPack, opts)
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()

	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	_, err = session.Fetch(ctx, FetchRequest{Wants: []hash.ObjectID{idOf(1)}}, nil)
	if err == nil {
		t.Fatalf("Fetch succeeded, want a context error")
	}
}

type erroringReader struct{ err error }

func (e erroringReader) Read([]byte) (int, error) { return 0, e.err }

func TestSSHPushFailsWhenPackReaderErrors(t *testing.T) {
	hostSigner := generateSSHSigner(t)
	clientSigner := generateSSHSigner(t)
	wantErr := fmt.Errorf("disk exploded")
	addr := startFakeSSHServer(t, sshServerConfig{
		hostKey:     hostSigner,
		allowedKeys: []ssh.PublicKey{clientSigner.PublicKey()},
		handle: func(t *testing.T, ch ssh.Channel, cmd string) {
			if _, err := ch.Write(gitV1AdvertisementBytes(testPushHeadCaps, [][2]string{{idOf(1).String(), "refs/heads/main"}})); err != nil {
				t.Errorf("Write returned error %v", err)
				return
			}
			dec := NewDecoder(ch)
			for {
				_, typ, err := readOnePktLine(dec)
				if err != nil {
					return
				}
				if typ == PktFlush {
					return
				}
			}
		},
	})
	opts := Options{
		Keys:     testSignerKeySource{clientSigner},
		HostKeys: NewKnownHosts(writeKnownHostsFile(t, addr, hostSigner.PublicKey()), neverConfirm(t)),
	}
	session, err := Dial(t.Context(), "ssh://"+addr+"/repo.git", ReceivePack, opts)
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()
	_, err = session.Push(t.Context(), PushRequest{
		Updates: []Update{{Name: "refs/heads/main", Old: hash.Zero, New: idOf(1)}},
		Pack:    erroringReader{err: wantErr},
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Push returned %v, want %v", err, wantErr)
	}
}

func TestAgentSignersCanceledByContextWhileWaitingForAgentReply(t *testing.T) {
	serverEnd, clientEnd := net.Pipe()
	t.Cleanup(func() { _ = serverEnd.Close() })

	restore := agentDialer
	agentDialer = func() (io.ReadWriteCloser, error) { return clientEnd, nil }
	t.Cleanup(func() { agentDialer = restore })

	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()

	done := make(chan struct{})
	var err error
	go func() {
		defer close(done)
		_, err = agentSigners(ctx)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("agentSigners did not return after the context deadline")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("agentSigners returned %v, want context.DeadlineExceeded", err)
	}
}
