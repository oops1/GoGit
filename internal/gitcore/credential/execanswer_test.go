package credential

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readFakeHelperStdin(t *testing.T, action func(h *execHelper) error) string {
	t.Helper()
	stdinFile := filepath.Join(t.TempDir(), "stdin.txt")
	h := fakeHelper(t, map[string]string{"GOGIT_CREDENTIAL_FAKE_STDIN_FILE": stdinFile})
	if err := action(h); err != nil {
		t.Fatalf("helper action returned %v", err)
	}
	stdin, err := os.ReadFile(stdinFile)
	if err != nil {
		t.Fatalf("reading stdin file: %v", err)
	}
	return string(stdin)
}

func TestExecHelperStoreSendsTheAnsweredUsernameWhenTheQueryHasNone(t *testing.T) {
	got := readFakeHelperStdin(t, func(h *execHelper) error {
		return h.Store(context.Background(), Query{Protocol: "https", Host: "example.com:8443"}, Answer{Username: "bob", Password: []byte("pw")})
	})
	want := "protocol=https\nhost=example.com:8443\nusername=bob\npassword=pw\n\n"
	if got != want {
		t.Fatalf("stdin = %q, want %q", got, want)
	}
}

func TestExecHelperStorePrefersTheAnsweredUsernameOverTheQueried(t *testing.T) {
	got := readFakeHelperStdin(t, func(h *execHelper) error {
		return h.Store(context.Background(), Query{Protocol: "https", Host: "example.com", Username: "alice"}, Answer{Username: "bob", Password: []byte("pw")})
	})
	if !strings.Contains(got, "username=bob\n") || strings.Contains(got, "alice") {
		t.Fatalf("stdin = %q, want only the answered username", got)
	}
}

func TestExecHelperEraseSendsTheRejectedUsernameAndPassword(t *testing.T) {
	got := readFakeHelperStdin(t, func(h *execHelper) error {
		return h.Erase(context.Background(), Query{Protocol: "http", Host: "example.com:8080", Path: "repo.git"}, Answer{Username: "bob", Password: []byte("wrong")})
	})
	want := "protocol=http\nhost=example.com:8080\npath=repo.git\nusername=bob\npassword=wrong\n\n"
	if got != want {
		t.Fatalf("stdin = %q, want %q", got, want)
	}
}

func TestExecHelperEraseWithoutAnAnswerSendsOnlyTheQuery(t *testing.T) {
	got := readFakeHelperStdin(t, func(h *execHelper) error {
		return h.Erase(context.Background(), Query{Protocol: "https", Host: "example.com", Username: "alice"}, Answer{})
	})
	want := "protocol=https\nhost=example.com\nusername=alice\n\n"
	if got != want {
		t.Fatalf("stdin = %q, want %q", got, want)
	}
}

func TestEncodeRequestOmitsAnEmptyPassword(t *testing.T) {
	got := string(encodeRequest(Query{Protocol: "https", Host: "example.com"}, []byte{}))
	if strings.Contains(got, "password") {
		t.Fatalf("encodeRequest = %q, want no password field", got)
	}
}
