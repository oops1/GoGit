package transport

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

func TestKnownHostsLineUsable(t *testing.T) {
	key := generateSSHSigner(t).PublicKey()
	keyText := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key)))
	tests := []struct {
		line string
		want bool
	}{
		{line: "", want: true},
		{line: "   # comment", want: true},
		{line: "example.com " + keyText, want: true},
		{line: "[example.com]:2222,10.0.0.1 " + keyText, want: true},
		{line: "*.example.com,!secret.example.com " + keyText, want: true},
		{line: "@cert-authority *.example.com " + keyText, want: true},
		{line: "@revoked * " + keyText, want: true},
		{line: knownhosts.HashHostname("example.com") + " " + keyText, want: true},
		{line: "@future example.com " + keyText, want: false},
		{line: "example.com ssh-xmss@openssh.com AAAAFHNzaC14bXNzQG9wZW5zc2guY29t", want: false},
		{line: "onlyoneword", want: false},
		{line: "|1|!!!|abc " + keyText, want: false},
		{line: "|1|c2FsdA==|!!! " + keyText, want: false},
		{line: "|2|c2FsdA==|aGFzaA== " + keyText, want: false},
		{line: "|1|c2FsdA== " + keyText, want: false},
		{line: "example.com,! " + keyText, want: false},
		{line: "[example.com:22 " + keyText, want: false},
	}
	for _, tt := range tests {
		if got := knownHostsLineUsable([]byte(tt.line)); got != tt.want {
			t.Fatalf("knownHostsLineUsable(%q) = %v, want %v", tt.line, got, tt.want)
		}
	}
}

func knownHostsWithBrokenLine(t *testing.T, host string, signer ssh.Signer) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "known_hosts")
	content := "@future broken line\n" + sshKnownHostsLine(host, signer.PublicKey())
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestKnownHostsCheckRemovesItsScratchCopy(t *testing.T) {
	signer := generateSSHSigner(t)
	path := knownHostsWithBrokenLine(t, "example.com:22", signer)
	scratchRoot := t.TempDir()
	var scratch string
	restore := makeKnownHostsScratch
	makeKnownHostsScratch = func() (string, error) {
		dir, err := os.MkdirTemp(scratchRoot, "copy-")
		scratch = dir
		return dir, err
	}
	t.Cleanup(func() { makeKnownHostsScratch = restore })

	if err := NewKnownHosts(path, neverConfirm(t)).Check(t.Context(), hostKeyFor(t, "example.com:22", signer)); err != nil {
		t.Fatalf("Check returned error %v", err)
	}
	if scratch == "" {
		t.Fatalf("no scratch copy was made for a file with a broken line")
	}
	if _, err := os.Stat(scratch); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("scratch copy %s was left behind: %v", scratch, err)
	}
}

func TestKnownHostsCheckFailsWhenTheFilteredCopyCannotBeWritten(t *testing.T) {
	signer := generateSSHSigner(t)
	path := knownHostsWithBrokenLine(t, "example.com:22", signer)
	wantErr := errors.New("no temp")
	cases := map[string]func() (string, error){
		"scratch directory": func() (string, error) { return "", wantErr },
		"scratch file":      func() (string, error) { return filepath.Join(t.TempDir(), "missing", "dir"), nil },
	}
	for name, scratch := range cases {
		restore := makeKnownHostsScratch
		makeKnownHostsScratch = scratch
		err := NewKnownHosts(path, neverConfirm(t)).Check(t.Context(), hostKeyFor(t, "example.com:22", signer))
		makeKnownHostsScratch = restore
		if err == nil || errors.Is(err, ErrHostKeyRejected) {
			t.Fatalf("%s: Check returned %v, want the copy error", name, err)
		}
	}
}

func TestKnownHostsCheckPropagatesTheCallbackBuildError(t *testing.T) {
	wantErr := errors.New("boom")
	restore := newKnownHostsCallback
	newKnownHostsCallback = func(...string) (ssh.HostKeyCallback, error) { return nil, wantErr }
	t.Cleanup(func() { newKnownHostsCallback = restore })

	policy := NewKnownHosts(filepath.Join(t.TempDir(), "known_hosts"), neverConfirm(t))
	if err := policy.Check(t.Context(), hostKeyFor(t, "example.com:22", generateSSHSigner(t))); !errors.Is(err, wantErr) {
		t.Fatalf("Check returned %v, want %v", err, wantErr)
	}
}

func TestKnownHostsForHopReadsTheConfiguredUserFiles(t *testing.T) {
	signer := generateSSHSigner(t)
	userFile := writeKnownHostsFile(t, "example.com:22", signer.PublicKey())
	base := NewKnownHosts(filepath.Join(t.TempDir(), "known_hosts"), nil).(*knownHostsPolicy)

	if err := base.forHop([]string{userFile}, "").Check(t.Context(), hostKeyFor(t, "example.com:22", signer)); err != nil {
		t.Fatalf("Check returned %v, want the UserKnownHostsFile to be trusted", err)
	}
	if err := base.forHop(nil, "").Check(t.Context(), hostKeyFor(t, "example.com:22", signer)); !errors.Is(err, ErrHostKeyRejected) {
		t.Fatalf("Check without the user file returned %v, want ErrHostKeyRejected", err)
	}
}

func TestKnownHostsStrictHostKeyChecking(t *testing.T) {
	for _, strict := range []string{"yes", "true"} {
		policy := NewKnownHosts(filepath.Join(t.TempDir(), "known_hosts"), neverConfirm(t)).(*knownHostsPolicy).forHop(nil, strict)
		if err := policy.Check(t.Context(), hostKeyFor(t, "example.com:22", generateSSHSigner(t))); !errors.Is(err, ErrHostKeyRejected) {
			t.Fatalf("StrictHostKeyChecking %s returned %v, want ErrHostKeyRejected", strict, err)
		}
	}
	for _, strict := range []string{"no", "off", "false", "accept-new"} {
		path := filepath.Join(t.TempDir(), "known_hosts")
		signer := generateSSHSigner(t)
		policy := NewKnownHosts(path, neverConfirm(t)).(*knownHostsPolicy).forHop(nil, strict)
		if err := policy.Check(t.Context(), hostKeyFor(t, "example.com:22", signer)); err != nil {
			t.Fatalf("StrictHostKeyChecking %s returned %v, want the new key accepted", strict, err)
		}
		changed := policy.Check(t.Context(), hostKeyFor(t, "example.com:22", generateSSHSigner(t)))
		if !errors.Is(changed, ErrHostKeyChanged) {
			t.Fatalf("StrictHostKeyChecking %s returned %v for a changed key, want ErrHostKeyChanged", strict, changed)
		}
	}
}
