package credential

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func newTestStoreHelper(t *testing.T) (*storeHelper, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), ".git-credentials")
	return &storeHelper{name: "store", path: path}, path
}

func fixtureCredentialLine(user, pass, host, path string) string {
	u := &url.URL{Scheme: "https", User: url.UserPassword(user, pass), Host: host, Path: path}
	return u.String()
}

func TestStoreHelperGetOnMissingFileIsNotFound(t *testing.T) {
	h, _ := newTestStoreHelper(t)
	_, ok, err := h.Get(context.Background(), Query{Protocol: "https", Host: "example.com"})
	if err != nil {
		t.Fatalf("Get returned %v", err)
	}
	if ok {
		t.Fatalf("Get reported found against a missing file")
	}
}

func TestStoreHelperGetSkipsMalformedLines(t *testing.T) {
	h, path := newTestStoreHelper(t)
	content := "not a url\n" + fixtureCredentialLine("alice", "pw", "example.com", "/") + "\n\n# a comment\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	ans, ok, err := h.Get(context.Background(), Query{Protocol: "https", Host: "example.com"})
	if err != nil {
		t.Fatalf("Get returned %v", err)
	}
	if !ok || ans.Username != "alice" || string(ans.Password) != "pw" {
		t.Fatalf("Get = %+v, %v", ans, ok)
	}
}

func TestStoreHelperGetPicksLongestPathMatch(t *testing.T) {
	h, path := newTestStoreHelper(t)
	content := fixtureCredentialLine("alice", "root", "example.com", "/") + "\n" +
		fixtureCredentialLine("bob", "scoped", "example.com", "/org/repo.git") + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	ans, ok, err := h.Get(context.Background(), Query{Protocol: "https", Host: "example.com", Path: "org/repo.git"})
	if err != nil || !ok {
		t.Fatalf("Get = %+v, %v, %v", ans, ok, err)
	}
	if ans.Username != "bob" || string(ans.Password) != "scoped" {
		t.Fatalf("Get returned %+v, want bob/scoped", ans)
	}
	ans, ok, err = h.Get(context.Background(), Query{Protocol: "https", Host: "example.com", Path: "other/repo.git"})
	if err != nil || !ok {
		t.Fatalf("Get = %+v, %v, %v", ans, ok, err)
	}
	if ans.Username != "alice" {
		t.Fatalf("Get returned %+v, want alice", ans)
	}
}

func TestStoreHelperGetFiltersByUsername(t *testing.T) {
	h, path := newTestStoreHelper(t)
	content := fixtureCredentialLine("alice", "pw1", "example.com", "/") + "\n" +
		fixtureCredentialLine("bob", "pw2", "example.com", "/") + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	ans, ok, err := h.Get(context.Background(), Query{Protocol: "https", Host: "example.com", Username: "bob"})
	if err != nil || !ok {
		t.Fatalf("Get = %+v, %v, %v", ans, ok, err)
	}
	if ans.Username != "bob" || string(ans.Password) != "pw2" {
		t.Fatalf("Get returned %+v, want bob/pw2", ans)
	}
	_, ok, err = h.Get(context.Background(), Query{Protocol: "https", Host: "example.com", Username: "carol"})
	if err != nil {
		t.Fatalf("Get returned %v", err)
	}
	if ok {
		t.Fatalf("Get reported found for an unknown username")
	}
}

func TestStoreHelperStoreAppendsNewEntry(t *testing.T) {
	h, path := newTestStoreHelper(t)
	q := Query{Protocol: "https", Host: "example.com", Username: "alice"}
	if err := h.Store(context.Background(), q, Answer{Username: "alice", Password: []byte("s3cret")}); err != nil {
		t.Fatalf("Store returned %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading credentials file: %v", err)
	}
	if !strings.Contains(string(data), "https://alice:s3cret@example.com") {
		t.Fatalf("credentials file = %q", data)
	}
	ans, ok, err := h.Get(context.Background(), q)
	if err != nil || !ok || string(ans.Password) != "s3cret" {
		t.Fatalf("Get = %+v, %v, %v", ans, ok, err)
	}
}

func TestStoreHelperStoreReplacesExistingEntry(t *testing.T) {
	h, path := newTestStoreHelper(t)
	q := Query{Protocol: "https", Host: "example.com", Username: "alice"}
	if err := h.Store(context.Background(), q, Answer{Username: "alice", Password: []byte("old")}); err != nil {
		t.Fatalf("Store returned %v", err)
	}
	if err := h.Store(context.Background(), q, Answer{Username: "alice", Password: []byte("new")}); err != nil {
		t.Fatalf("Store returned %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 {
		t.Fatalf("credentials file has %d lines, want 1: %q", len(lines), data)
	}
	ans, ok, err := h.Get(context.Background(), q)
	if err != nil || !ok || string(ans.Password) != "new" {
		t.Fatalf("Get = %+v, %v, %v", ans, ok, err)
	}
}

func TestStoreHelperStoreWithPathIsRoundTripped(t *testing.T) {
	h, path := newTestStoreHelper(t)
	q := Query{Protocol: "https", Host: "example.com", Path: "org/repo.git", Username: "alice"}
	if err := h.Store(context.Background(), q, Answer{Username: "alice", Password: []byte("pw")}); err != nil {
		t.Fatalf("Store returned %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "https://alice:pw@example.com/org/repo.git") {
		t.Fatalf("credentials file = %q", data)
	}
	ans, ok, err := h.Get(context.Background(), q)
	if err != nil || !ok || ans.Username != "alice" {
		t.Fatalf("Get = %+v, %v, %v", ans, ok, err)
	}
}

func TestStoreHelperEraseRemovesOnlyMatchingEntries(t *testing.T) {
	h, _ := newTestStoreHelper(t)
	alice := Query{Protocol: "https", Host: "example.com", Username: "alice"}
	bob := Query{Protocol: "https", Host: "example.com", Username: "bob"}
	if err := h.Store(context.Background(), alice, Answer{Username: "alice", Password: []byte("pw1")}); err != nil {
		t.Fatal(err)
	}
	if err := h.Store(context.Background(), bob, Answer{Username: "bob", Password: []byte("pw2")}); err != nil {
		t.Fatal(err)
	}
	if err := h.Erase(context.Background(), alice); err != nil {
		t.Fatalf("Erase returned %v", err)
	}
	if _, ok, _ := h.Get(context.Background(), alice); ok {
		t.Fatalf("Get still found the erased entry")
	}
	ans, ok, err := h.Get(context.Background(), bob)
	if err != nil || !ok || string(ans.Password) != "pw2" {
		t.Fatalf("Get = %+v, %v, %v, want bob/pw2 to survive", ans, ok, err)
	}
}

func TestStoreHelperGetSkipsEntriesForOtherHosts(t *testing.T) {
	h, path := newTestStoreHelper(t)
	content := fixtureCredentialLine("alice", "pw1", "other.example", "/") + "\n" +
		fixtureCredentialLine("bob", "pw2", "example.com", "/") + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	ans, ok, err := h.Get(context.Background(), Query{Protocol: "https", Host: "example.com"})
	if err != nil || !ok {
		t.Fatalf("Get = %+v, %v, %v", ans, ok, err)
	}
	if ans.Username != "bob" {
		t.Fatalf("Get returned %+v, want bob (the other.example entry must not match)", ans)
	}
}

func TestStoreHelperStoreWithoutPasswordKeepsUsername(t *testing.T) {
	h, _ := newTestStoreHelper(t)
	q := Query{Protocol: "https", Host: "example.com", Username: "alice"}
	if err := h.Store(context.Background(), q, Answer{Username: "alice"}); err != nil {
		t.Fatalf("Store returned %v", err)
	}
	ans, ok, err := h.Get(context.Background(), q)
	if err != nil || !ok {
		t.Fatalf("Get = %+v, %v, %v", ans, ok, err)
	}
	if ans.Username != "alice" || len(ans.Password) != 0 {
		t.Fatalf("Get returned %+v, want alice with no password", ans)
	}
}

func TestStoreHelperReadTooLongLineReturnsError(t *testing.T) {
	h, path := newTestStoreHelper(t)
	content := "https://alice:" + strings.Repeat("x", 100000) + "@example.com/\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := h.Get(context.Background(), Query{Protocol: "https", Host: "example.com"}); err == nil {
		t.Fatalf("Get succeeded despite a credentials line exceeding the scanner limit")
	}
}

func TestStoreHelperStoreReturnsReadError(t *testing.T) {
	h, path := newTestStoreHelper(t)
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
	err := h.Store(context.Background(), Query{Protocol: "https", Host: "example.com"}, Answer{Password: []byte("pw")})
	if err == nil {
		t.Fatalf("Store succeeded despite the credentials path being a directory")
	}
}

func TestStoreHelperEraseReturnsReadError(t *testing.T) {
	h, path := newTestStoreHelper(t)
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := h.Erase(context.Background(), Query{Protocol: "https", Host: "example.com"}); err == nil {
		t.Fatalf("Erase succeeded despite the credentials path being a directory")
	}
}

func TestStoreHelperStoreMkdirAllFailure(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := &storeHelper{name: "store", path: filepath.Join(blocker, "sub", ".git-credentials")}
	err := h.Store(context.Background(), Query{Protocol: "https", Host: "example.com"}, Answer{Password: []byte("pw")})
	if err == nil {
		t.Fatalf("Store succeeded despite an unmakeable parent directory")
	}
}

func TestStoreHelperFileHasOwnerOnlyPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix permission bits are not meaningful on windows")
	}
	h, path := newTestStoreHelper(t)
	q := Query{Protocol: "https", Host: "example.com", Username: "alice"}
	if err := h.Store(context.Background(), q, Answer{Username: "alice", Password: []byte("pw")}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("file mode = %o, want 0600", perm)
	}
}

func TestStoreHelperGetReturnsReadError(t *testing.T) {
	h, path := newTestStoreHelper(t)
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, _, err := h.Get(context.Background(), Query{Protocol: "https", Host: "example.com"}); err == nil {
		t.Fatalf("Get succeeded despite the credentials path being a directory")
	}
}

func TestStoreHelperStoreReturnsWriteError(t *testing.T) {
	h, path := newTestStoreHelper(t)
	if err := os.MkdirAll(path+".tmp", 0o700); err != nil {
		t.Fatal(err)
	}
	err := h.Store(context.Background(), Query{Protocol: "https", Host: "example.com"}, Answer{Password: []byte("pw")})
	if err == nil {
		t.Fatalf("Store succeeded despite a directory occupying the temp file path")
	}
}
