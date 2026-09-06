package transport

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/ssh"
)

func hostKeyFor(t *testing.T, host string, signer ssh.Signer) HostKey {
	t.Helper()
	pub := signer.PublicKey()
	return HostKey{
		Host:        host,
		Algorithm:   pub.Type(),
		Fingerprint: ssh.FingerprintSHA256(pub),
		Key:         pub.Marshal(),
	}
}

func TestStringAddrImplementsNetAddr(t *testing.T) {
	a := stringAddr("127.0.0.1:22")
	if a.Network() != "tcp" {
		t.Fatalf("Network() = %q, want tcp", a.Network())
	}
	if a.String() != "127.0.0.1:22" {
		t.Fatalf("String() = %q, want 127.0.0.1:22", a.String())
	}
}

func TestKnownHostsCheckAcceptsMatchingKeyFromOurFile(t *testing.T) {
	signer := generateSSHSigner(t)
	path := writeKnownHostsFile(t, "example.com:22", signer.PublicKey())
	policy := NewKnownHosts(path, neverConfirm(t))
	err := policy.Check(t.Context(), hostKeyFor(t, "example.com:22", signer))
	if err != nil {
		t.Fatalf("Check returned error %v", err)
	}
}

func TestKnownHostsCheckReadsUserKnownHostsFile(t *testing.T) {
	signer := generateSSHSigner(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatalf("MkdirAll returned error %v", err)
	}
	if err := os.WriteFile(filepath.Join(sshDir, "known_hosts"), []byte(sshKnownHostsLine("example.com:22", signer.PublicKey())+"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error %v", err)
	}
	ourPath := filepath.Join(t.TempDir(), "known_hosts")
	policy := NewKnownHosts(ourPath, neverConfirm(t))
	if err := policy.Check(t.Context(), hostKeyFor(t, "example.com:22", signer)); err != nil {
		t.Fatalf("Check returned error %v", err)
	}
	if _, err := os.Stat(ourPath); err == nil {
		t.Fatalf("our known_hosts file was created even though the user file already trusted the key")
	}
}

func TestKnownHostsCheckRejectsUnknownWithoutConfirmFunc(t *testing.T) {
	signer := generateSSHSigner(t)
	path := filepath.Join(t.TempDir(), "known_hosts")
	policy := NewKnownHosts(path, nil)
	err := policy.Check(t.Context(), hostKeyFor(t, "example.com:22", signer))
	if !errors.Is(err, ErrHostKeyRejected) {
		t.Fatalf("Check returned %v, want ErrHostKeyRejected", err)
	}
}

func TestKnownHostsCheckPropagatesConfirmError(t *testing.T) {
	signer := generateSSHSigner(t)
	path := filepath.Join(t.TempDir(), "known_hosts")
	wantErr := errors.New("boom")
	policy := NewKnownHosts(path, func(context.Context, HostKey) (bool, error) { return false, wantErr })
	err := policy.Check(t.Context(), hostKeyFor(t, "example.com:22", signer))
	if !errors.Is(err, wantErr) {
		t.Fatalf("Check returned %v, want %v", err, wantErr)
	}
}

func TestKnownHostsCheckRejectsMalformedKeyBytes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "known_hosts")
	policy := NewKnownHosts(path, neverConfirm(t))
	err := policy.Check(t.Context(), HostKey{Host: "example.com:22", Key: []byte("not a key")})
	if !errors.Is(err, ErrHostKeyRejected) {
		t.Fatalf("Check returned %v, want ErrHostKeyRejected", err)
	}
}

func TestKnownHostsAcceptRejectsMalformedKeyBytes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "known_hosts")
	policy := NewKnownHosts(path, neverConfirm(t))
	err := policy.Accept(t.Context(), HostKey{Host: "example.com:22", Key: []byte("not a key")})
	if !errors.Is(err, ErrHostKeyRejected) {
		t.Fatalf("Accept returned %v, want ErrHostKeyRejected", err)
	}
}

func TestKnownHostsAcceptCreatesParentDirectory(t *testing.T) {
	signer := generateSSHSigner(t)
	path := filepath.Join(t.TempDir(), "nested", "dir", "known_hosts")
	policy := NewKnownHosts(path, neverConfirm(t))
	if err := policy.Accept(t.Context(), hostKeyFor(t, "example.com:22", signer)); err != nil {
		t.Fatalf("Accept returned error %v", err)
	}
	if err := policy.Check(t.Context(), hostKeyFor(t, "example.com:22", signer)); err != nil {
		t.Fatalf("Check returned error %v after Accept", err)
	}
}

func TestKnownHostsBuildCallbackPropagatesParseError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "known_hosts")
	if err := os.WriteFile(path, []byte("this is not a known_hosts line at all @@@\n"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error %v", err)
	}
	signer := generateSSHSigner(t)
	policy := NewKnownHosts(path, neverConfirm(t))
	err := policy.Check(t.Context(), hostKeyFor(t, "example.com:22", signer))
	if err == nil {
		t.Fatalf("Check succeeded, want a parse error")
	}
	if errors.Is(err, ErrHostKeyRejected) || errors.Is(err, ErrHostKeyChanged) {
		t.Fatalf("Check returned %v, want a raw parse error", err)
	}
}

func TestKnownHostsCheckReturnsRawErrorForRevokedKey(t *testing.T) {
	signer := generateSSHSigner(t)
	path := filepath.Join(t.TempDir(), "known_hosts")
	line := "@revoked " + sshKnownHostsLine("example.com:22", signer.PublicKey()) + "\n"
	if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
		t.Fatalf("WriteFile returned error %v", err)
	}
	policy := NewKnownHosts(path, neverConfirm(t))
	err := policy.Check(t.Context(), hostKeyFor(t, "example.com:22", signer))
	if err == nil {
		t.Fatalf("Check succeeded, want an error for a revoked key")
	}
	if errors.Is(err, ErrHostKeyRejected) || errors.Is(err, ErrHostKeyChanged) {
		t.Fatalf("Check returned %v, want the raw revocation error", err)
	}
}

func TestHomeKnownHostsPathIsEmptyWhenUserHomeDirFails(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	t.Setenv("HOMEDRIVE", "")
	t.Setenv("HOMEPATH", "")
	if got := homeKnownHostsPath(); got != "" {
		t.Fatalf("homeKnownHostsPath() = %q, want empty string", got)
	}
}

func TestKnownHostsAcceptFailsWhenParentPathIsAFile(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error %v", err)
	}
	signer := generateSSHSigner(t)
	policy := NewKnownHosts(filepath.Join(blocker, "nested", "known_hosts"), neverConfirm(t))
	err := policy.Accept(t.Context(), hostKeyFor(t, "example.com:22", signer))
	if err == nil {
		t.Fatalf("Accept succeeded, want an error since a path component is a regular file")
	}
}

func TestKnownHostsAcceptFailsWhenPathIsADirectory(t *testing.T) {
	dir := t.TempDir()
	signer := generateSSHSigner(t)
	policy := NewKnownHosts(dir, neverConfirm(t))
	err := policy.Accept(t.Context(), hostKeyFor(t, "example.com:22", signer))
	if err == nil {
		t.Fatalf("Accept succeeded, want an error since the path is a directory")
	}
}

func TestHomeKnownHostsPathIsUnderDotSSH(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	got := homeKnownHostsPath()
	want := filepath.Join(home, ".ssh", "known_hosts")
	if got != want {
		t.Fatalf("homeKnownHostsPath() = %q, want %q", got, want)
	}
}

func TestExistingFilesSkipsMissingAndEmptyPaths(t *testing.T) {
	dir := t.TempDir()
	present := filepath.Join(dir, "present")
	if err := os.WriteFile(present, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error %v", err)
	}
	missing := filepath.Join(dir, "missing")
	got := existingFiles("", missing, present)
	if len(got) != 1 || got[0] != present {
		t.Fatalf("existingFiles = %v, want [%q]", got, present)
	}
}
