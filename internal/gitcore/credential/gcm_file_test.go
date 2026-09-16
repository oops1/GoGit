package credential

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestGCMFileStore(t *testing.T) *gcmFileStore {
	t.Helper()
	return &gcmFileStore{root: filepath.Join(t.TempDir(), "store"), namespace: "git"}
}

func TestGCMFileStoreLaysOutFilesLikeTheManager(t *testing.T) {
	s := newTestGCMFileStore(t)
	cases := map[string]string{
		"https://github.com":                    filepath.Join(s.root, "git", "https", "github.com"),
		"https://example.com:8443/org/repo.git": filepath.Join(s.root, "git", "https", "example.com"+gcmPortSeparator+"8443", "org", "repo.git"),
		"not a uri":                             filepath.Join(s.root, "git", "not a uri"),
	}
	for service, want := range cases {
		if got := s.serviceDirectory(service); got != want {
			t.Errorf("serviceDirectory(%q) = %q, want %q", service, got, want)
		}
	}
	bare := &gcmFileStore{root: s.root}
	if got := bare.serviceDirectory("https://github.com"); got != filepath.Join(s.root, "https", "github.com") {
		t.Errorf("serviceDirectory without namespace = %q", got)
	}
}

func TestGCMFileStoreWritesTheManagerFileFormat(t *testing.T) {
	s := newTestGCMFileStore(t)
	ctx := context.Background()
	if err := s.put(ctx, "https://github.com", "bob", []byte("pw")); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(s.root, "git", "https", "github.com", "bob.credential"))
	if err != nil {
		t.Fatal(err)
	}
	want := "pw" + gcmFileNewline + "service=https://github.com" + gcmFileNewline + "account=bob" + gcmFileNewline
	if string(data) != want {
		t.Fatalf("file = %q, want %q", data, want)
	}
	ans, ok, err := s.get(ctx, "https://github.com", "")
	if err != nil || !ok || ans.Username != "bob" || string(ans.Password) != "pw" {
		t.Fatalf("get = %+v, %v, %v", ans, ok, err)
	}
	ans, ok, err = s.get(ctx, "https://GITHUB.com", "BOB")
	if err != nil || !ok || string(ans.Password) != "pw" {
		t.Fatalf("case-insensitive get = %+v, %v, %v", ans, ok, err)
	}
}

func TestGCMFileStoreSkipsForeignAndBrokenFiles(t *testing.T) {
	s := newTestGCMFileStore(t)
	dir := s.serviceDirectory("https://github.com")
	if err := os.MkdirAll(filepath.Join(dir, "sub.credential"), 0o700); err != nil {
		t.Fatal(err)
	}
	nl := gcmFileNewline
	files := map[string]string{
		"notes.txt":            "pw" + nl + "service=https://github.com" + nl + "account=notes" + nl,
		"empty.credential":     nl + "service=https://github.com" + nl,
		"noservice.credential": "pw" + nl + "account=noservice" + nl + "junk" + nl,
		"other.credential":     "pw" + nl + "service=https://gitlab.com" + nl + "account=other" + nl,
		"renamed.credential":   "pw" + nl + "service=https://github.com" + nl + "account=someone" + nl,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, account := range []string{"", "notes", "empty", "noservice", "other", "renamed"} {
		ans, ok, err := s.get(context.Background(), "https://github.com", account)
		if err != nil {
			t.Fatal(err)
		}
		if account == "" && (!ok || ans.Username != "someone") {
			t.Fatalf("any-account get = %+v, %v", ans, ok)
		}
		if account != "" && ok {
			t.Fatalf("get(%q) = %+v, want a miss", account, ans)
		}
	}
}

func TestGCMFileStorePutSkipsAnUnchangedCredential(t *testing.T) {
	s := newTestGCMFileStore(t)
	ctx := context.Background()
	if err := s.put(ctx, "https://github.com", "bob", []byte("pw")); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(s.serviceDirectory("https://github.com"), "bob.credential")
	old := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	if err := s.put(ctx, "https://github.com", "bob", []byte("pw")); err != nil {
		t.Fatal(err)
	}
	if after, err := os.Stat(path); err != nil || !after.ModTime().Equal(old) {
		t.Fatalf("an unchanged credential was rewritten: %v", err)
	}
	if err := s.put(ctx, "https://github.com", "bob", []byte("new")); err != nil {
		t.Fatal(err)
	}
	ans, _, _ := s.get(ctx, "https://github.com", "bob")
	if string(ans.Password) != "new" {
		t.Fatalf("password = %q", ans.Password)
	}
}

func TestGCMFileStoreRejectsAccountsThatEscapeTheDirectory(t *testing.T) {
	s := newTestGCMFileStore(t)
	for _, account := range []string{"../evil", `..\evil`, ".", ".."} {
		if err := s.put(context.Background(), "https://github.com", account, []byte("pw")); !errors.Is(err, ErrInvalidAccount) {
			t.Errorf("put(%q) = %v, want ErrInvalidAccount", account, err)
		}
	}
}

func TestGCMFileStoreRemoveDeletesOnlyTheRejectedSecret(t *testing.T) {
	s := newTestGCMFileStore(t)
	ctx := context.Background()
	if err := s.put(ctx, "https://github.com", "bob", []byte("new")); err != nil {
		t.Fatal(err)
	}
	if err := s.remove(ctx, "https://github.com", "bob", []byte("old")); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.get(ctx, "https://github.com", "bob"); !ok {
		t.Fatal("a stale reject removed the new credential")
	}
	if err := s.remove(ctx, "https://github.com", "bob", []byte("new")); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.get(ctx, "https://github.com", "bob"); ok {
		t.Fatal("the rejected credential is still stored")
	}
	if err := s.remove(ctx, "https://github.com", "bob", nil); err != nil {
		t.Fatal(err)
	}
}

func TestGCMFileStoreReportsFileSystemFailures(t *testing.T) {
	ctx := context.Background()

	s := newTestGCMFileStore(t)
	prevReadDir := readGCMDir
	readGCMDir = func(string) ([]os.DirEntry, error) { return nil, errors.New("unlistable") }
	t.Cleanup(func() { readGCMDir = prevReadDir })
	if err := s.put(ctx, "https://github.com", "bob", []byte("pw")); err == nil {
		t.Fatal("put into a service directory that is a file succeeded")
	}
	if _, _, err := s.get(ctx, "https://github.com", "bob"); err == nil {
		t.Fatal("get from a service directory that is a file succeeded")
	}
	if err := s.remove(ctx, "https://github.com", "bob", nil); err == nil {
		t.Fatal("remove from a service directory that is a file succeeded")
	}
	readGCMDir = prevReadDir

	s = newTestGCMFileStore(t)
	blocked := filepath.Join(s.serviceDirectory("https://github.com"), "bob.credential")
	if err := os.MkdirAll(filepath.Join(blocked, "keep"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := s.put(ctx, "https://github.com", "bob", []byte("pw")); err == nil {
		t.Fatal("put over a directory succeeded")
	}

	s = newTestGCMFileStore(t)
	if err := s.put(ctx, "https://github.com", "bob", []byte("pw")); err != nil {
		t.Fatal(err)
	}
	withUnreadableGCMFiles(t)
	if _, _, err := s.get(ctx, "https://github.com", ""); err == nil || !strings.Contains(err.Error(), "unreadable") {
		t.Fatalf("get err = %v, want the read failure", err)
	}
	if err := s.put(ctx, "https://github.com", "bob", []byte("pw")); err == nil {
		t.Fatal("put succeeded although the existing file could not be read")
	}
}

func withUnreadableGCMFiles(t *testing.T) {
	t.Helper()
	prev := readGCMFile
	readGCMFile = func(string) ([]byte, error) { return nil, errors.New("unreadable") }
	t.Cleanup(func() { readGCMFile = prev })
}

func TestGCMFileStoreStopsAtTheFirstUnreadableFileAfterMatches(t *testing.T) {
	s := newTestGCMFileStore(t)
	ctx := context.Background()
	for _, account := range []string{"a", "b"} {
		if err := s.put(ctx, "https://github.com", account, []byte("pw")); err != nil {
			t.Fatal(err)
		}
	}
	calls := 0
	prev := readGCMFile
	readGCMFile = func(path string) ([]byte, error) {
		calls++
		if calls == 2 {
			return nil, errors.New("unreadable")
		}
		return prev(path)
	}
	t.Cleanup(func() { readGCMFile = prev })
	if _, _, err := s.get(ctx, "https://github.com", ""); err == nil {
		t.Fatal("expected the second read to fail")
	}
}
