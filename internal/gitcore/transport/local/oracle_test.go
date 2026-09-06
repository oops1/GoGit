//go:build oracle

package local

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/pack"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/transport"
)

func oracleGit(t *testing.T, dir string, args ...string) []byte {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", args...)
	cmd.Dir = dir
	cmd.Env = append(cmd.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_AUTHOR_NAME=oracle",
		"GIT_AUTHOR_EMAIL=oracle@example.com",
		"GIT_COMMITTER_NAME=oracle",
		"GIT_COMMITTER_EMAIL=oracle@example.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s returned error %v: %s", strings.Join(args, " "), err, out)
	}
	return out
}

func writeOracleFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile returned error %v", err)
	}
}

func TestFetchFromGitCreatedRepositoryMatchesRevList(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git is not available: %v", err)
	}
	dir := t.TempDir()
	oracleGit(t, dir, "init", "-q", "-b", "main")
	writeOracleFile(t, dir, "a.txt", "hello\n")
	oracleGit(t, dir, "add", "a.txt")
	oracleGit(t, dir, "commit", "-q", "-m", "initial")
	writeOracleFile(t, dir, "b.txt", "world\n")
	oracleGit(t, dir, "add", "b.txt")
	oracleGit(t, dir, "commit", "-q", "-m", "second")
	oracleGit(t, dir, "tag", "-a", "v1", "-m", "v1")

	ctx := t.Context()
	sess, err := Dial(ctx, dir, transport.UploadPack, transport.Options{})
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	t.Cleanup(func() { _ = sess.Close() })

	adv, err := sess.Advertise(ctx)
	if err != nil {
		t.Fatalf("Advertise returned error %v", err)
	}
	wants := make([]hash.ObjectID, 0, len(adv.Refs))
	for _, ref := range adv.Refs {
		if ref.Name == "HEAD" {
			continue
		}
		wants = append(wants, ref.ID)
	}
	resp, err := sess.Fetch(ctx, transport.FetchRequest{Wants: wants}, nil)
	if err != nil {
		t.Fatalf("Fetch returned error %v", err)
	}

	dst := newTestRepo(t, true)
	if _, err := pack.IndexPack(ctx, resp.Pack, dst.repo.PackDir(), pack.IndexOptions{}); err != nil {
		t.Fatalf("IndexPack returned error %v", err)
	}
	if err := resp.Pack.Close(); err != nil {
		t.Fatalf("closing the pack returned error %v", err)
	}
	tx := dst.refs.Begin()
	for _, ref := range adv.Refs {
		if ref.Name == "HEAD" {
			continue
		}
		if err := tx.Set(refs.Name(ref.Name), ref.ID); err != nil {
			t.Fatalf("Set(%s) returned error %v", ref.Name, err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit returned error %v", err)
	}

	if out := oracleGit(t, dst.dir, "fsck"); len(strings.TrimSpace(string(out))) != 0 {
		t.Fatalf("git fsck reported problems in the clone: %s", out)
	}
	srcRevs := oracleGit(t, dir, "rev-list", "--all")
	dstRevs := oracleGit(t, dst.dir, "rev-list", "--all")
	if string(srcRevs) != string(dstRevs) {
		t.Fatalf("git rev-list --all differs: source %q, clone %q", srcRevs, dstRevs)
	}
}

func TestPushToGitCreatedBareRepositoryPassesFsck(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git is not available: %v", err)
	}
	bareDir := t.TempDir()
	oracleGit(t, bareDir, "init", "-q", "--bare", "-b", "main")

	src := newTestRepo(t, true)
	first := src.commit("main", map[string]string{"a.txt": "hello"})
	second := src.commit("main", map[string]string{"a.txt": "hello", "b.txt": "world"}, first)

	ctx := t.Context()
	sess, err := Dial(ctx, bareDir, transport.ReceivePack, transport.Options{})
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	t.Cleanup(func() { _ = sess.Close() })

	ids := make([]hash.ObjectID, 0)
	for id := range src.allObjects() {
		ids = append(ids, id)
	}
	var packBytes bytes.Buffer
	if _, err := pack.WritePack(ctx, &packBytes, src.db, ids, pack.WriteOptions{}); err != nil {
		t.Fatalf("WritePack returned error %v", err)
	}
	result, err := sess.Push(ctx, transport.PushRequest{
		Updates: []transport.Update{{Name: "refs/heads/main", Old: hash.Zero, New: second}},
		Pack:    bytes.NewReader(packBytes.Bytes()),
	})
	if err != nil {
		t.Fatalf("Push returned error %v", err)
	}
	if !result.UnpackOK {
		t.Fatalf("result.UnpackOK = false, UnpackError = %q", result.UnpackError)
	}
	if len(result.Refs) != 1 || !result.Refs[0].OK {
		t.Fatalf("result.Refs = %+v, want a single OK status", result.Refs)
	}

	if out := oracleGit(t, bareDir, "fsck"); len(strings.TrimSpace(string(out))) != 0 {
		t.Fatalf("git fsck reported problems after the push: %s", out)
	}
	head := strings.TrimSpace(string(oracleGit(t, bareDir, "rev-parse", "refs/heads/main")))
	if head != second.String() {
		t.Fatalf("refs/heads/main = %s, want %s", head, second)
	}
	revs := strings.TrimSpace(string(oracleGit(t, bareDir, "rev-list", "--all")))
	want := second.String() + "\n" + first.String()
	if revs != want {
		t.Fatalf("git rev-list --all = %q, want %q", revs, want)
	}
}
