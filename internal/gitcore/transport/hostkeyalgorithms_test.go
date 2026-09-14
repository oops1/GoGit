package transport

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

func TestHostKeyAlgorithmsListTheKeyTypesKnownForTheHost(t *testing.T) {
	addr := "127.0.0.1:2222"
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	rsaPublic, err := ssh.NewPublicKey(&rsaKey.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "known_hosts")
	lines := sshKnownHostsLine(addr, generateSSHSigner(t).PublicKey()) + "\n" + sshKnownHostsLine(addr, rsaPublic) + "\n"
	if err := os.WriteFile(path, []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}
	policy := NewKnownHosts(path, nil).(*knownHostsPolicy)

	got := policy.HostKeyAlgorithms(addr)
	want := []string{ssh.KeyAlgoED25519, ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSA}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("HostKeyAlgorithms = %v, want %v", got, want)
	}
	if unknown := policy.HostKeyAlgorithms("127.0.0.1:1"); unknown != nil {
		t.Fatalf("an unknown host got algorithms %v", unknown)
	}
	if broken := NewKnownHosts(t.TempDir(), nil).(*knownHostsPolicy).HostKeyAlgorithms(addr); broken != nil {
		t.Fatalf("an unreadable known_hosts gave algorithms %v", broken)
	}
	revokedPath := filepath.Join(t.TempDir(), "known_hosts")
	if err := os.WriteFile(revokedPath, []byte("@revoked "+sshKnownHostsLine("*", probeHostKey)), 0o600); err != nil {
		t.Fatal(err)
	}
	if revoked := NewKnownHosts(revokedPath, nil).(*knownHostsPolicy).HostKeyAlgorithms(addr); revoked != nil {
		t.Fatalf("a known_hosts that revokes the probe key gave algorithms %v", revoked)
	}
}

func TestDialPicksTheHostKeyTypeThatIsKnown(t *testing.T) {
	ecdsaKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ecdsaSigner, err := ssh.NewSignerFromKey(ecdsaKey)
	if err != nil {
		t.Fatal(err)
	}
	hostSigner := generateSSHSigner(t)
	clientPriv, clientSigner := generateEd25519(t)
	adv := gitV1AdvertisementBytes(testHeadCaps, [][2]string{{idOf(1).String(), "refs/heads/main"}})
	addr := startFakeSSHServer(t, sshServerConfig{
		hostKey:       ecdsaSigner,
		extraHostKeys: []ssh.Signer{hostSigner},
		allowedKeys:   []ssh.PublicKey{clientSigner.PublicKey()},
		handle:        sshUploadPackHandler(t, adv, 0, fakePack(31)),
	})
	dir := t.TempDir()
	writeKeyFile(t, dir, "id_ed25519", clientPriv, nil)

	session, err := Dial(t.Context(), "ssh://"+addr+"/repo.git", UploadPack, Options{
		Keys:     NewDirKeys(dir),
		HostKeys: NewKnownHosts(writeKnownHostsFile(t, addr, hostSigner.PublicKey()), neverConfirm(t)),
	})
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()
	resp, err := session.Fetch(t.Context(), FetchRequest{Wants: []hash.ObjectID{idOf(1)}}, nil)
	if err != nil {
		t.Fatalf("Fetch returned error %v; the known ed25519 key was not chosen", err)
	}
	defer func() { _ = resp.Pack.Close() }()
	if _, err := io.ReadAll(resp.Pack); err != nil {
		t.Fatalf("reading the pack returned error %v", err)
	}
}
