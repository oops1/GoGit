package transport

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func startFakeBastion(t *testing.T, hostKey ssh.Signer, allowed ssh.PublicKey, forward bool) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen returned error %v", err)
	}
	config := &ssh.ServerConfig{
		PublicKeyCallback: func(_ ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if bytes.Equal(key.Marshal(), allowed.Marshal()) {
				return nil, nil
			}
			return nil, errors.New("unknown public key")
		},
	}
	config.AddHostKey(hostKey)
	var wg sync.WaitGroup
	wg.Go(func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			wg.Go(func() { serveFakeBastionConn(conn, config, forward) })
		}
	})
	t.Cleanup(func() {
		_ = ln.Close()
		done := make(chan struct{})
		go func() {
			wg.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Errorf("fake bastion did not finish within 5s")
		}
	})
	return ln.Addr().String()
}

func serveFakeBastionConn(conn net.Conn, config *ssh.ServerConfig, forward bool) {
	defer func() { _ = conn.Close() }()
	sConn, chans, reqs, err := ssh.NewServerConn(conn, config)
	if err != nil {
		return
	}
	defer func() { _ = sConn.Close() }()
	go ssh.DiscardRequests(reqs)
	var copies sync.WaitGroup
	defer copies.Wait()
	for newCh := range chans {
		if newCh.ChannelType() != "direct-tcpip" || !forward {
			_ = newCh.Reject(ssh.Prohibited, "forwarding is disabled")
			continue
		}
		var payload struct {
			Host       string
			Port       uint32
			OriginHost string
			OriginPort uint32
		}
		_ = ssh.Unmarshal(newCh.ExtraData(), &payload)
		target, err := net.Dial("tcp", net.JoinHostPort(payload.Host, strconv.Itoa(int(payload.Port))))
		if err != nil {
			_ = newCh.Reject(ssh.ConnectionFailed, err.Error())
			continue
		}
		ch, chReqs, err := newCh.Accept()
		if err != nil {
			_ = target.Close()
			continue
		}
		go ssh.DiscardRequests(chReqs)
		copies.Go(func() {
			_, _ = io.Copy(ch, target)
			_ = ch.Close()
		})
		copies.Go(func() {
			_, _ = io.Copy(target, ch)
			_ = target.Close()
		})
	}
}

type sshTestNetwork struct {
	client    ssh.Signer
	target    string
	targetKey ssh.Signer
	bastions  []string
	hostKeys  map[string]ssh.PublicKey
}

func newSSHTestNetwork(t *testing.T, forwards ...bool) *sshTestNetwork {
	t.Helper()
	n := &sshTestNetwork{client: generateSSHSigner(t), targetKey: generateSSHSigner(t), hostKeys: map[string]ssh.PublicKey{}}
	adv := gitV1AdvertisementBytes(testHeadCaps, [][2]string{{idOf(1).String(), "refs/heads/main"}})
	n.target = startFakeSSHServer(t, sshServerConfig{
		hostKey:     n.targetKey,
		allowedKeys: []ssh.PublicKey{n.client.PublicKey()},
		handle:      sshAdvertiseOnlyHandler(sshExecCommand(UploadPack, "/repo.git"), adv),
	})
	n.hostKeys[n.target] = n.targetKey.PublicKey()
	for _, forward := range forwards {
		key := generateSSHSigner(t)
		addr := startFakeBastion(t, key, n.client.PublicKey(), forward)
		n.bastions = append(n.bastions, addr)
		n.hostKeys[addr] = key.PublicKey()
	}
	return n
}

func (n *sshTestNetwork) knownHosts(t *testing.T) string {
	t.Helper()
	var lines []string
	for addr, key := range n.hostKeys {
		lines = append(lines, sshKnownHostsLine(addr, key))
	}
	path := filepath.Join(t.TempDir(), "known_hosts")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func hostBlock(alias, addr string, extra ...string) string {
	host, port, _ := net.SplitHostPort(addr)
	lines := append([]string{"Host " + alias, "  HostName " + host, "  Port " + port}, extra...)
	return strings.Join(lines, "\n") + "\n"
}

func (n *sshTestNetwork) jumpConfig(t *testing.T) string {
	t.Helper()
	var aliases []string
	var blocks []string
	for i, addr := range n.bastions {
		alias := fmt.Sprintf("bastion%d", i+1)
		aliases = append(aliases, alias)
		blocks = append(blocks, hostBlock(alias, addr))
	}
	target := hostBlock("target", n.target, "  ProxyJump "+strings.Join(aliases, ","))
	return writeSSHConfig(t, t.TempDir(), "config", target+strings.Join(blocks, ""))
}

func advertiseTarget(t *testing.T, opts Options) error {
	t.Helper()
	session, err := Dial(t.Context(), "ssh://target/repo.git", UploadPack, opts)
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()
	_, err = session.Advertise(t.Context())
	return err
}

func TestSSHAdvertiseReachesTheTargetThroughAProxyJumpChain(t *testing.T) {
	n := newSSHTestNetwork(t, true, true)
	opts := Options{
		Keys:     testSignerKeySource{n.client},
		HostKeys: NewKnownHosts(n.knownHosts(t), neverConfirm(t)),
		SSH:      SSHOptions{ConfigFiles: []string{n.jumpConfig(t)}},
	}
	if err := advertiseTarget(t, opts); err != nil {
		t.Fatalf("Advertise returned error %v, want the target reached through both bastions", err)
	}
}

func TestSSHAdvertiseFailsWhenTheBastionRefusesToForward(t *testing.T) {
	n := newSSHTestNetwork(t, false)
	opts := Options{
		Keys:     testSignerKeySource{n.client},
		HostKeys: NewKnownHosts(n.knownHosts(t), neverConfirm(t)),
		SSH:      SSHOptions{ConfigFiles: []string{n.jumpConfig(t)}},
	}
	err := advertiseTarget(t, opts)
	if err == nil || !strings.Contains(err.Error(), "forwarding is disabled") {
		t.Fatalf("Advertise returned %v, want the forwarding refusal", err)
	}
}

func TestSSHAdvertiseFailsWhenTheBastionRejectsTheKey(t *testing.T) {
	n := newSSHTestNetwork(t, true)
	opts := Options{
		Keys:     testSignerKeySource{generateSSHSigner(t)},
		HostKeys: NewKnownHosts(n.knownHosts(t), neverConfirm(t)),
		SSH:      SSHOptions{ConfigFiles: []string{n.jumpConfig(t)}},
	}
	if err := advertiseTarget(t, opts); !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("Advertise returned %v, want ErrAccessDenied from the bastion", err)
	}
}

type hostOnlyKeySource struct {
	host   string
	signer ssh.Signer
}

func (s hostOnlyKeySource) Keys(context.Context, string) ([]Key, error) {
	return nil, nil
}

func (s hostOnlyKeySource) listSigners(_ context.Context, target SSHTarget, _ *closerSet) ([]ssh.Signer, error) {
	if target.Host != s.host {
		return nil, ErrNoKeys
	}
	return []ssh.Signer{s.signer}, nil
}

func TestSSHAdvertiseClosesTheBastionWhenTheTargetHasNoKeys(t *testing.T) {
	n := newSSHTestNetwork(t, true)
	opts := Options{
		Keys:     hostOnlyKeySource{host: "bastion1", signer: n.client},
		HostKeys: NewKnownHosts(n.knownHosts(t), neverConfirm(t)),
		SSH:      SSHOptions{ConfigFiles: []string{n.jumpConfig(t)}},
	}
	if err := advertiseTarget(t, opts); !errors.Is(err, ErrNoKeys) {
		t.Fatalf("Advertise returned %v, want ErrNoKeys for the target", err)
	}
}

func TestSSHAdvertiseRefusesProxyCommand(t *testing.T) {
	config := writeSSHConfig(t, t.TempDir(), "config", "Host target\n  ProxyCommand nc %h %p\n")
	opts := Options{Keys: testSignerKeySource{generateSSHSigner(t)}, SSH: SSHOptions{ConfigFiles: []string{config}}}
	if err := advertiseTarget(t, opts); !errors.Is(err, ErrProxyCommandUnsupported) {
		t.Fatalf("Advertise returned %v, want ErrProxyCommandUnsupported", err)
	}
}

func TestSSHAdvertiseUsesTheConfiguredIdentityFileAndHostKeyAlias(t *testing.T) {
	n := newSSHTestNetwork(t)
	priv, signer := generateEd25519(t)
	n.client = signer
	adv := gitV1AdvertisementBytes(testHeadCaps, [][2]string{{idOf(1).String(), "refs/heads/main"}})
	n.target = startFakeSSHServer(t, sshServerConfig{
		hostKey:     n.targetKey,
		allowedKeys: []ssh.PublicKey{signer.PublicKey()},
		handle:      sshAdvertiseOnlyHandler(sshExecCommand(UploadPack, "/repo.git"), adv),
	})
	_, port, _ := net.SplitHostPort(n.target)
	keyFile := writeKeyFile(t, t.TempDir(), "work_key", priv, nil)
	userHosts := filepath.Join(t.TempDir(), "user_known_hosts")
	if err := os.WriteFile(userHosts, []byte(sshKnownHostsLine("pinned:"+port, n.targetKey.PublicKey())+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	config := writeSSHConfig(t, t.TempDir(), "config", hostBlock("target", n.target,
		"  IdentityFile \""+keyFile+"\"",
		"  HostKeyAlias pinned",
		"  UserKnownHostsFile \""+userHosts+"\"",
	))
	opts := Options{
		Keys:     NewDirKeys(t.TempDir()),
		HostKeys: NewKnownHosts(filepath.Join(t.TempDir(), "known_hosts"), neverConfirm(t)),
		SSH:      SSHOptions{ConfigFiles: []string{config}},
	}
	if err := advertiseTarget(t, opts); err != nil {
		t.Fatalf("Advertise returned error %v", err)
	}
}

type acceptAllHostKeys struct{}

func (acceptAllHostKeys) Check(context.Context, HostKey) error  { return nil }
func (acceptAllHostKeys) Accept(context.Context, HostKey) error { return nil }

func TestHostKeyPolicyForKeepsForeignPolicies(t *testing.T) {
	policy := acceptAllHostKeys{}
	if got := hostKeyPolicyFor(policy, sshHop{strictHostKeys: "yes"}); got != policy {
		t.Fatalf("hostKeyPolicyFor replaced a foreign policy with %T", got)
	}
}
