//go:build oracle

package object_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
)

func gitEnv() []string {
	return []string{
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=A U Thor",
		"GIT_AUTHOR_EMAIL=author@example.com",
		"GIT_AUTHOR_DATE=1700000000 +0300",
		"GIT_COMMITTER_NAME=C O Mitter",
		"GIT_COMMITTER_EMAIL=committer@example.com",
		"GIT_COMMITTER_DATE=1700000100 +0300",
	}
}

func gitIn(t *testing.T, dir string, stdin []byte, args ...string) []byte {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	cmd.Env = append(cmd.Environ(), gitEnv()...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, stderr.String())
	}
	return out
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	return strings.TrimRight(string(gitIn(t, dir, nil, args...)), "\r\n")
}

func newRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "-q", "--initial-branch=main", ".")
	git(t, dir, "config", "core.autocrlf", "false")
	git(t, dir, "config", "core.safecrlf", "false")
	return dir
}

func writeWorkFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func looseObjectPaths(t *testing.T, dir string) []string {
	t.Helper()
	root := filepath.Join(dir, ".git", "objects")
	var paths []string
	fanouts, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, fanout := range fanouts {
		if !fanout.IsDir() || len(fanout.Name()) != 2 {
			continue
		}
		names, err := os.ReadDir(filepath.Join(root, fanout.Name()))
		if err != nil {
			t.Fatalf("ReadDir: %v", err)
		}
		for _, name := range names {
			paths = append(paths, filepath.Join(root, fanout.Name(), name.Name()))
		}
	}
	if len(paths) == 0 {
		t.Fatal("the repository has no loose objects")
	}
	return paths
}

func TestOracleReadsEveryObjectOfARepositoryGitCreated(t *testing.T) {
	dir := newRepo(t)
	writeWorkFile(t, dir, "file.txt", "hello\n")
	writeWorkFile(t, dir, "sub/deep.txt", "deep content\n")
	writeWorkFile(t, dir, "sub/nested/leaf.txt", "leaf\n")
	writeWorkFile(t, dir, "empty.txt", "")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-q", "-m", "initial commit")
	writeWorkFile(t, dir, "file.txt", "changed\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-q", "-m", "second commit\n\nwith a body")
	git(t, dir, "tag", "-a", "-m", "annotated tag", "v1.0")
	git(t, dir, "checkout", "-q", "-b", "side", "HEAD~1")
	writeWorkFile(t, dir, "side.txt", "side\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-q", "-m", "side commit")
	git(t, dir, "checkout", "-q", "main")
	git(t, dir, "merge", "-q", "--no-ff", "-m", "merge side", "side")
	git(t, dir, "update-index", "--add", "--cacheinfo",
		"160000,"+git(t, dir, "rev-parse", "HEAD")+",gitlink")
	linkTarget := strings.TrimRight(string(gitIn(t, dir, []byte("sub/deep.txt"),
		"hash-object", "-w", "--stdin")), "\r\n")
	git(t, dir, "update-index", "--add", "--cacheinfo", "120000,"+linkTarget+",link")
	git(t, dir, "write-tree")

	for _, path := range looseObjectPaths(t, dir) {
		obj, err := object.ReadLoose(path)
		if err != nil {
			t.Fatalf("ReadLoose(%s): %v", path, err)
		}
		id, err := object.IDFromLoosePath(path)
		if err != nil {
			t.Fatalf("IDFromLoosePath: %v", err)
		}
		kind := git(t, dir, "cat-file", "-t", id.String())
		if obj.Type().String() != kind {
			t.Fatalf("%s: we say %s, git says %s", id, obj.Type(), kind)
		}
		size := git(t, dir, "cat-file", "-s", id.String())
		if got := strconv.Itoa(len(obj.Encode())); got != size {
			t.Fatalf("%s: we say %s bytes, git says %s", id, got, size)
		}
		content := gitIn(t, dir, nil, "cat-file", kind, id.String())
		if !bytes.Equal(obj.Encode(), content) {
			t.Fatalf("%s: content differs from git\n ours: %q\n git: %q", id, obj.Encode(), content)
		}
	}
}

func TestOracleGitAcceptsObjectsWeWrote(t *testing.T) {
	dir := newRepo(t)
	objects := filepath.Join(dir, ".git", "objects")

	blob := &object.Blob{Data: []byte("written by gogit\n")}
	blobID, err := object.WriteLoose(objects, blob)
	if err != nil {
		t.Fatalf("WriteLoose blob: %v", err)
	}
	inner := &object.Tree{Entries: []object.TreeEntry{{Mode: object.ModeBlob, Name: "inner.txt", ID: blobID}}}
	innerID, err := object.WriteLoose(objects, inner)
	if err != nil {
		t.Fatalf("WriteLoose inner tree: %v", err)
	}
	link := &object.Blob{Data: []byte("inner.txt")}
	linkID, err := object.WriteLoose(objects, link)
	if err != nil {
		t.Fatalf("WriteLoose symlink target: %v", err)
	}
	tree := &object.Tree{Entries: []object.TreeEntry{
		{Mode: object.ModeExecutable, Name: "run.sh", ID: blobID},
		{Mode: object.ModeTree, Name: "dir", ID: innerID},
		{Mode: object.ModeSymlink, Name: "link", ID: linkID},
		{Mode: object.ModeBlob, Name: "dir.txt", ID: blobID},
	}}
	tree.Sort()
	treeID, err := object.WriteLoose(objects, tree)
	if err != nil {
		t.Fatalf("WriteLoose tree: %v", err)
	}
	when := time.Unix(1700000000, 0).In(time.FixedZone("+0300", 3*3600))
	author := object.Signature{Name: "A U Thor", Email: "author@example.com", When: when}
	commit := &object.Commit{
		Tree:      treeID,
		Author:    author,
		Committer: author,
		Message:   "commit written by gogit\n",
	}
	commitID, err := object.WriteLoose(objects, commit)
	if err != nil {
		t.Fatalf("WriteLoose commit: %v", err)
	}
	tag := &object.Tag{
		Object:     commitID,
		ObjectType: object.TypeCommit,
		Name:       "v0.1",
		Tagger:     &author,
		Message:    "tag written by gogit\n",
	}
	tagID, err := object.WriteLoose(objects, tag)
	if err != nil {
		t.Fatalf("WriteLoose tag: %v", err)
	}

	git(t, dir, "update-ref", "refs/heads/main", commitID.String())
	git(t, dir, "update-ref", "refs/tags/v0.1", tagID.String())
	git(t, dir, "fsck", "--strict", "--no-progress")

	if got := git(t, dir, "rev-parse", "HEAD^{tree}"); got != treeID.String() {
		t.Fatalf("git resolved the tree as %s, we wrote %s", got, treeID)
	}
	if got := git(t, dir, "log", "-1", "--format=%s%n%an <%ae>%n%ad", "--date=raw"); got !=
		"commit written by gogit\nA U Thor <author@example.com>\n1700000000 +0300" {
		t.Fatalf("git log printed:\n%s", got)
	}
	if got := git(t, dir, "cat-file", "-p", tagID.String()); !strings.Contains(got, "tag v0.1") {
		t.Fatalf("git cat-file printed:\n%s", got)
	}
	if got := git(t, dir, "ls-tree", "-r", "--full-tree", "HEAD"); !strings.Contains(got, "120000 blob") ||
		!strings.Contains(got, "100755 blob") {
		t.Fatalf("git ls-tree printed:\n%s", got)
	}
	git(t, dir, "checkout", "-q", "--force", "main")
	if got := git(t, dir, "status", "--porcelain"); got != "" {
		t.Fatalf("checkout of our tree left changes:\n%s", got)
	}
}

func TestOracleTreeSortMatchesGitMktree(t *testing.T) {
	dir := newRepo(t)
	blobID, err := hash.Parse(git(t, dir, "hash-object", "-w", "--stdin"))
	if err != nil {
		t.Fatalf("hash-object: %v", err)
	}
	subID, err := hash.Parse(git(t, dir, "mktree"))
	if err != nil {
		t.Fatalf("mktree: %v", err)
	}
	moduleID, err := hash.Parse(git(t, dir, "commit-tree", subID.String(), "-m", "submodule head"))
	if err != nil {
		t.Fatalf("commit-tree: %v", err)
	}
	entries := []object.TreeEntry{
		{Mode: object.ModeTree, Name: "a", ID: subID},
		{Mode: object.ModeBlob, Name: "a.b", ID: blobID},
		{Mode: object.ModeSubmodule, Name: "a-mod", ID: moduleID},
		{Mode: object.ModeBlob, Name: "a-file", ID: blobID},
		{Mode: object.ModeTree, Name: "a-dir", ID: subID},
		{Mode: object.ModeSymlink, Name: "aa", ID: blobID},
	}
	var listing strings.Builder
	for _, entry := range entries {
		listing.WriteString(entry.Mode.String() + " " + entry.Mode.ObjectType().String() +
			" " + entry.ID.String() + "\t" + entry.Name + "\n")
	}
	want := strings.TrimSpace(string(gitIn(t, dir, []byte(listing.String()), "mktree", "--missing")))

	tree := &object.Tree{Entries: entries}
	tree.Sort()
	if got := tree.ID().String(); got != want {
		t.Fatalf("our sorted tree is %s, git mktree produced %s\nours:\n%s", got, want,
			git(t, dir, "cat-file", "-p", want))
	}
}

func TestOracleCommitIDMatchesGitCommitTree(t *testing.T) {
	dir := newRepo(t)
	treeID, err := hash.Parse(git(t, dir, "mktree"))
	if err != nil {
		t.Fatalf("mktree: %v", err)
	}
	rootID, err := hash.Parse(git(t, dir, "commit-tree", treeID.String(), "-m", "root"))
	if err != nil {
		t.Fatalf("commit-tree: %v", err)
	}
	want := git(t, dir, "commit-tree", treeID.String(), "-p", rootID.String(), "-m", "child")
	when := time.Unix(1700000000, 0).In(time.FixedZone("+0300", 3*3600))
	commit := &object.Commit{
		Tree:      treeID,
		Parents:   []hash.ObjectID{rootID},
		Author:    object.Signature{Name: "A U Thor", Email: "author@example.com", When: when},
		Committer: object.Signature{Name: "C O Mitter", Email: "committer@example.com", When: when.Add(100 * time.Second)},
		Message:   "child\n",
	}
	if got := commit.ID().String(); got != want {
		t.Fatalf("our commit is %s, git commit-tree produced %s\nours:\n%q\ngit:\n%q",
			got, want, commit.Encode(), git(t, dir, "cat-file", "commit", want))
	}
}

func TestOracleFixtureIDsMatchGitHashObject(t *testing.T) {
	dir := newRepo(t)
	for _, f := range fixtures(t) {
		t.Run(f.name, func(t *testing.T) {
			raw := f.raw(t)
			out := gitIn(t, dir, raw, "hash-object", "-t", f.kind.String(), "--stdin", "--literally")
			want := strings.TrimSpace(string(out))
			if got := f.object(t).ID().String(); got != want {
				t.Fatalf("our id %s, git says %s", got, want)
			}
		})
	}
}
