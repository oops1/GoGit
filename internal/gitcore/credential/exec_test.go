package credential

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExecHelperGetReturnsAnswer(t *testing.T) {
	h := fakeHelper(t, map[string]string{
		"GOGIT_CREDENTIAL_FAKE_STDOUT": "username=bob\npassword=secret\n",
	})
	ans, ok, err := h.Get(context.Background(), Query{Protocol: "https", Host: "example.com"})
	if err != nil {
		t.Fatalf("Get returned %v", err)
	}
	if !ok {
		t.Fatalf("Get reported not found")
	}
	if ans.Username != "bob" || string(ans.Password) != "secret" {
		t.Fatalf("Get returned %+v", ans)
	}
}

func TestExecHelperGetIncompleteAnswerIsNotFound(t *testing.T) {
	h := fakeHelper(t, map[string]string{
		"GOGIT_CREDENTIAL_FAKE_STDOUT": "username=bob\n",
	})
	ans, ok, err := h.Get(context.Background(), Query{Protocol: "https", Host: "example.com"})
	if err != nil {
		t.Fatalf("Get returned %v", err)
	}
	if ok {
		t.Fatalf("Get reported found for an incomplete answer: %+v", ans)
	}
}

func TestExecHelperGetMalformedAnswerReturnsError(t *testing.T) {
	h := fakeHelper(t, map[string]string{
		"GOGIT_CREDENTIAL_FAKE_STDOUT": "garbage without an equals sign\n",
	})
	_, _, err := h.Get(context.Background(), Query{Protocol: "https", Host: "example.com"})
	if !errors.Is(err, ErrMalformedAnswer) {
		t.Fatalf("Get returned %v, want ErrMalformedAnswer", err)
	}
}

func TestExecHelperGetOutputOverLimitReturnsErrHelperFailed(t *testing.T) {
	h := fakeHelper(t, map[string]string{
		"GOGIT_CREDENTIAL_FAKE_STDOUT": "username=" + strings.Repeat("a", maxAnswerBytes+1),
	})
	_, _, err := h.Get(context.Background(), Query{Protocol: "https", Host: "example.com"})
	if !errors.Is(err, ErrHelperFailed) {
		t.Fatalf("Get returned %v, want ErrHelperFailed", err)
	}
}

func TestDecodeAnswerLineTooLongReturnsMalformed(t *testing.T) {
	data := []byte("username=" + strings.Repeat("a", maxAnswerBytes+1))
	_, _, err := decodeAnswer(data)
	if !errors.Is(err, ErrMalformedAnswer) {
		t.Fatalf("decodeAnswer returned %v, want ErrMalformedAnswer", err)
	}
}

func TestExecHelperNonZeroExitReturnsErrHelperFailed(t *testing.T) {
	h := fakeHelper(t, map[string]string{
		"GOGIT_CREDENTIAL_FAKE_EXIT": "7",
	})
	_, _, err := h.Get(context.Background(), Query{Protocol: "https", Host: "example.com"})
	if !errors.Is(err, ErrHelperFailed) {
		t.Fatalf("Get returned %v, want ErrHelperFailed", err)
	}
}

func TestExecHelperNotFoundReturnsErrHelperFailed(t *testing.T) {
	h := &execHelper{name: "missing", exe: "git-credential-this-does-not-exist-anywhere"}
	_, _, err := h.Get(context.Background(), Query{Protocol: "https", Host: "example.com"})
	if !errors.Is(err, ErrHelperFailed) {
		t.Fatalf("Get returned %v, want ErrHelperFailed", err)
	}
}

func TestExecHelperErasePropagatesFailure(t *testing.T) {
	h := fakeHelper(t, map[string]string{
		"GOGIT_CREDENTIAL_FAKE_EXIT": "1",
	})
	err := h.Erase(context.Background(), Query{Protocol: "https", Host: "example.com"})
	if !errors.Is(err, ErrHelperFailed) {
		t.Fatalf("Erase returned %v, want ErrHelperFailed", err)
	}
}

func TestExecHelperStorePasswordNeverAppearsInArgs(t *testing.T) {
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args.txt")
	stdinFile := filepath.Join(dir, "stdin.txt")
	h := fakeHelper(t, map[string]string{
		"GOGIT_CREDENTIAL_FAKE_ARGS_FILE":  argsFile,
		"GOGIT_CREDENTIAL_FAKE_STDIN_FILE": stdinFile,
	})
	const secret = "the-super-secret-password"
	q := Query{Protocol: "https", Host: "example.com", Username: "alice"}
	if err := h.Store(context.Background(), q, Answer{Username: "alice", Password: []byte(secret)}); err != nil {
		t.Fatalf("Store returned %v", err)
	}
	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("reading args file: %v", err)
	}
	if strings.Contains(string(args), secret) {
		t.Fatalf("process arguments contained the password: %q", args)
	}
	if string(args) != "store" {
		t.Fatalf("args = %q, want %q", args, "store")
	}
	stdin, err := os.ReadFile(stdinFile)
	if err != nil {
		t.Fatalf("reading stdin file: %v", err)
	}
	if !bytes.Contains(stdin, []byte("password="+secret)) {
		t.Fatalf("stdin did not contain the password field: %q", stdin)
	}
	if !bytes.Contains(stdin, []byte("username=alice")) {
		t.Fatalf("stdin did not contain the username field: %q", stdin)
	}
}

func TestExecHelperContextCancelKillsHangingProcess(t *testing.T) {
	h := fakeHelper(t, map[string]string{
		"GOGIT_CREDENTIAL_FAKE_SLEEP": "10s",
	})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, _, err := h.Get(ctx, Query{Protocol: "https", Host: "example.com"})
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("Get took %v, the hanging process was not killed promptly", elapsed)
	}
	if !errors.Is(err, ErrHelperFailed) {
		t.Fatalf("Get returned %v, want ErrHelperFailed", err)
	}
}

func TestExecHelperContextCanceledBeforeStart(t *testing.T) {
	h := fakeHelper(t, map[string]string{
		"GOGIT_CREDENTIAL_FAKE_SLEEP": "10s",
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := h.Get(ctx, Query{Protocol: "https", Host: "example.com"})
	if err == nil {
		t.Fatalf("Get succeeded despite an already-canceled context")
	}
}

func TestEncodeRequestOmitsEmptyFields(t *testing.T) {
	got := string(encodeRequest(Query{Protocol: "https", Host: "example.com"}, nil))
	want := "protocol=https\nhost=example.com\n\n"
	if got != want {
		t.Fatalf("encodeRequest = %q, want %q", got, want)
	}
}

func TestEncodeRequestIncludesAllFieldsAndPassword(t *testing.T) {
	q := Query{Protocol: "https", Host: "example.com", Path: "org/repo.git", Username: "alice"}
	got := string(encodeRequest(q, []byte("s3cret")))
	want := "protocol=https\nhost=example.com\npath=org/repo.git\nusername=alice\npassword=s3cret\n\n"
	if got != want {
		t.Fatalf("encodeRequest = %q, want %q", got, want)
	}
}

func TestDecodeAnswerIgnoresUnknownKeys(t *testing.T) {
	ans, complete, err := decodeAnswer([]byte("url=https://example.com\nusername=bob\npassword=x\nquit=1\n"))
	if err != nil {
		t.Fatalf("decodeAnswer returned %v", err)
	}
	if !complete {
		t.Fatalf("decodeAnswer reported incomplete")
	}
	if ans.Username != "bob" || string(ans.Password) != "x" {
		t.Fatalf("decodeAnswer = %+v", ans)
	}
}

func TestDecodeAnswerSkipsBlankLines(t *testing.T) {
	ans, complete, err := decodeAnswer([]byte("username=bob\n\npassword=x\n"))
	if err != nil {
		t.Fatalf("decodeAnswer returned %v", err)
	}
	if !complete || ans.Username != "bob" || string(ans.Password) != "x" {
		t.Fatalf("decodeAnswer = %+v, %v", ans, complete)
	}
}

func TestDecodeAnswerEmptyKeyIsMalformed(t *testing.T) {
	_, _, err := decodeAnswer([]byte("=value\n"))
	if !errors.Is(err, ErrMalformedAnswer) {
		t.Fatalf("decodeAnswer returned %v, want ErrMalformedAnswer", err)
	}
}

func TestBoundedBufferAccumulatesAcrossWrites(t *testing.T) {
	b := boundedBuffer{limit: 10}
	n, err := b.Write([]byte("abc"))
	if err != nil || n != 3 {
		t.Fatalf("Write = %d, %v, want 3, nil", n, err)
	}
	n, err = b.Write([]byte("def"))
	if err != nil || n != 3 {
		t.Fatalf("Write = %d, %v, want 3, nil", n, err)
	}
	if string(b.data) != "abcdef" {
		t.Fatalf("data = %q, want %q", b.data, "abcdef")
	}
}

func TestBoundedBufferRejectsWriteThatCrossesLimit(t *testing.T) {
	b := boundedBuffer{limit: 5}
	if _, err := b.Write([]byte("abc")); err != nil {
		t.Fatalf("first Write returned %v", err)
	}
	_, err := b.Write([]byte("def"))
	if !errors.Is(err, errHelperOutputTooLarge) {
		t.Fatalf("second Write returned %v, want errHelperOutputTooLarge", err)
	}
}

func TestExecHelperResolvesThroughPATH(t *testing.T) {
	dir := t.TempDir()
	installFakePathHelper(t, dir, "git-credential-pathfake")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	h, err := resolveHelper("pathfake")
	if err != nil {
		t.Fatalf("resolveHelper returned %v", err)
	}
	ans, ok, err := h.Get(context.Background(), Query{Protocol: "https", Host: "example.com"})
	if err != nil {
		t.Fatalf("Get returned %v", err)
	}
	if !ok || ans.Username != "pathuser" || string(ans.Password) != "pathpass" {
		t.Fatalf("Get = %+v, %v, want pathuser/pathpass", ans, ok)
	}
}
