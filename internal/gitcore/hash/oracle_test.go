//go:build oracle

package hash_test

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

func gitHashObject(t *testing.T, objectType string, data []byte) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", "hash-object", "-t", objectType, "--stdin", "--literally")
	cmd.Stdin = bytes.NewReader(data)
	cmd.Env = append(cmd.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git hash-object: %v", err)
	}
	return strings.TrimSpace(string(out))
}

func TestOracleSumMatchesGitHashObject(t *testing.T) {
	cases := []struct {
		objectType string
		data       []byte
	}{
		{"blob", nil},
		{"blob", []byte("hello\n")},
		{"blob", []byte("no trailing newline")},
		{"blob", []byte("line one\r\nline two\r\n")},
		{"blob", bytes.Repeat([]byte{0}, 1024)},
		{"blob", []byte("файл в utf-8\n")},
		{"tree", nil},
		{"commit", []byte("tree 4b825dc642cb6eb9a060e54bf8d69288fbee4904\n" +
			"author A U Thor <author@example.com> 1700000000 +0300\n" +
			"committer C O Mitter <committer@example.com> 1700000100 +0300\n\nmessage\n")},
		{"tag", []byte("object 4b825dc642cb6eb9a060e54bf8d69288fbee4904\ntype tree\ntag t\n\nbody\n")},
	}
	for _, c := range cases {
		want := gitHashObject(t, c.objectType, c.data)
		got, err := hash.Sum(hash.SHA1, c.objectType, c.data)
		if err != nil {
			t.Fatalf("Sum: %v", err)
		}
		if got.String() != want {
			t.Fatalf("Sum(%s, %d bytes) = %s, git says %s", c.objectType, len(c.data), got, want)
		}
	}
}

func TestOracleHasherMatchesGitHashObjectOnLargeContent(t *testing.T) {
	data := bytes.Repeat([]byte("streamed through the hasher\n"), 40000)
	hasher, err := hash.NewHasher(hash.SHA1, "blob", int64(len(data)))
	if err != nil {
		t.Fatalf("NewHasher: %v", err)
	}
	for offset := 0; offset < len(data); offset += 4096 {
		end := min(offset+4096, len(data))
		if _, err := hasher.Write(data[offset:end]); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}
	got, err := hasher.Sum()
	if err != nil {
		t.Fatalf("Sum: %v", err)
	}
	if want := gitHashObject(t, "blob", data); got.String() != want {
		t.Fatalf("Hasher = %s, git says %s", got, want)
	}
}
