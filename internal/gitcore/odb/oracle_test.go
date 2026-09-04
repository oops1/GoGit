//go:build oracle

package odb

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
)

type oracle struct {
	t      *testing.T
	root   string
	repo   string
	borrow string
	env    []string
}

func newOracle(t *testing.T) *oracle {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git is not available: %v", err)
	}
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatalf("MkdirAll returned error %v", err)
	}
	return &oracle{
		t:      t,
		root:   root,
		repo:   filepath.Join(root, "repo"),
		borrow: filepath.Join(root, "borrow"),
		env: []string{
			"PATH=" + os.Getenv("PATH"),
			"SystemRoot=" + os.Getenv("SystemRoot"),
			"HOME=" + home,
			"USERPROFILE=" + home,
			"GIT_CONFIG_NOSYSTEM=1",
			"GIT_TERMINAL_PROMPT=0",
			"GIT_AUTHOR_NAME=oracle",
			"GIT_AUTHOR_EMAIL=oracle@example.com",
			"GIT_COMMITTER_NAME=oracle",
			"GIT_COMMITTER_EMAIL=oracle@example.com",
			"GIT_AUTHOR_DATE=2024-01-02T03:04:05 +0000",
			"GIT_COMMITTER_DATE=2024-01-02T03:04:05 +0000",
		},
	}
}

func (o *oracle) command(dir string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(o.t.Context(), "git", args...)
	cmd.Dir = dir
	cmd.Env = o.env
	return cmd
}

func (o *oracle) raw(dir string, args ...string) []byte {
	o.t.Helper()
	var errors bytes.Buffer
	cmd := o.command(dir, args...)
	cmd.Stderr = &errors
	out, err := cmd.Output()
	if err != nil {
		o.t.Fatalf("git %s returned error %v: %s", strings.Join(args, " "), err, errors.String())
	}
	return out
}

func (o *oracle) run(dir string, args ...string) string {
	o.t.Helper()
	return string(o.raw(dir, args...))
}

func (o *oracle) lines(dir string, args ...string) []string {
	o.t.Helper()
	text := strings.ReplaceAll(o.run(dir, args...), "\r\n", "\n")
	return strings.Split(strings.TrimSuffix(text, "\n"), "\n")
}

func (o *oracle) init(dir string) {
	o.t.Helper()
	o.run(o.root, "init", "-q", "-b", "main", dir)
	for _, args := range [][]string{
		{"config", "user.name", "oracle"},
		{"config", "user.email", "oracle@example.com"},
		{"config", "core.autocrlf", "false"},
		{"config", "gc.auto", "0"},
	} {
		o.run(dir, args...)
	}
}

func (o *oracle) build() {
	o.t.Helper()
	o.init(o.repo)
	if err := os.MkdirAll(filepath.Join(o.repo, "lib", "deep"), 0o755); err != nil {
		o.t.Fatalf("MkdirAll returned error %v", err)
	}
	for revision := range 8 {
		o.writeRevision(revision + 1)
		o.run(o.repo, "add", "-A")
		o.run(o.repo, "commit", "-q", "-m", fmt.Sprintf("revision %d", revision+1))
	}
	o.run(o.repo, "tag", "-a", "v1.0", "-m", "annotated oracle tag", "HEAD~3")
	o.run(o.repo, "tag", "lightweight", "HEAD~1")
	o.run(o.repo, "gc", "-q", "--aggressive")
	for revision := range 3 {
		o.writeRevision(revision + 9)
		o.run(o.repo, "add", "-A")
		o.run(o.repo, "commit", "-q", "-m", fmt.Sprintf("loose revision %d", revision+9))
	}
	o.run(o.repo, "tag", "-a", "v2.0", "-m", "loose annotated tag", "HEAD")
	o.run(o.root, "clone", "-q", "--shared", "--no-checkout", o.repo, o.borrow)
	o.run(o.borrow, "commit", "-q", "--allow-empty", "-m", "borrowed commit")
}

func (o *oracle) writeRevision(revision int) {
	o.t.Helper()
	var body strings.Builder
	for line := range 200 {
		fmt.Fprintf(&body, "revision %d line %d\n", revision, line*revision%97)
	}
	files := map[string]string{
		"README.md":         fmt.Sprintf("# oracle repository\n\nrevision %d\n", revision),
		"lib/library.txt":   body.String(),
		"lib/deep/note.txt": strings.Repeat(fmt.Sprintf("note %d\n", revision), revision*3),
	}
	for name, content := range files {
		path := filepath.Join(o.repo, filepath.FromSlash(name))
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			o.t.Fatalf("WriteFile(%q) returned error %v", path, err)
		}
	}
}

func (o *oracle) objectsDir(repo string) string {
	o.t.Helper()
	return filepath.Join(repo, ".git", "objects")
}

func (o *oracle) knownObjects(repo string) []hash.ObjectID {
	o.t.Helper()
	var all []hash.ObjectID
	for _, line := range o.lines(repo, "cat-file", "--batch-all-objects", "--batch-check=%(objectname)") {
		all = append(all, parseID(o.t, line))
	}
	return all
}

func (o *oracle) tryRevParse(dir, arg string) (string, bool) {
	o.t.Helper()
	cmd := o.command(dir, "rev-parse", "--verify", "--quiet", arg)
	out, err := cmd.Output()
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(out)), true
}

func (o *oracle) fsck(repo string) {
	o.t.Helper()
	var errors bytes.Buffer
	cmd := o.command(repo, "fsck", "--strict", "--no-progress")
	cmd.Stderr = &errors
	out, err := cmd.Output()
	if err != nil {
		o.t.Fatalf("git fsck returned error %v: %s%s", err, out, errors.String())
	}
	for _, line := range strings.Split(errors.String(), "\n") {
		if line == "" || strings.HasPrefix(line, "dangling ") || strings.HasPrefix(line, "notice:") {
			continue
		}
		o.t.Fatalf("git fsck reported %q", line)
	}
}

func TestOracleReadsEveryObjectGitLists(t *testing.T) {
	git := newOracle(t)
	git.build()
	db := openDB(t, git.objectsDir(git.repo), Options{})
	names := git.lines(git.repo, "rev-list", "--objects", "--all")
	if len(names) < 20 {
		t.Fatalf("git listed %d objects, want more", len(names))
	}
	for _, line := range names {
		id := parseID(t, strings.Fields(line)[0])
		kind, data, err := db.Get(id)
		if err != nil {
			t.Fatalf("Get(%s) returned error %v", id, err)
		}
		want := git.raw(git.repo, "cat-file", kind.String(), id.String())
		if !bytes.Equal(data, want) {
			t.Fatalf("Get(%s) gave %d bytes, git gave %d", id, len(data), len(want))
		}
		wantKind := strings.TrimSpace(git.run(git.repo, "cat-file", "-t", id.String()))
		if kind.String() != wantKind {
			t.Fatalf("Get(%s) gave type %s, git says %s", id, kind, wantKind)
		}
		wantSize, err := strconv.ParseInt(strings.TrimSpace(git.run(git.repo, "cat-file", "-s", id.String())), 10, 64)
		if err != nil {
			t.Fatalf("ParseInt returned error %v", err)
		}
		size, err := db.Size(id)
		if err != nil || size != wantSize {
			t.Fatalf("Size(%s) gave (%d, %v), git says %d", id, size, err, wantSize)
		}
	}
}

func TestOracleListsEveryObjectGitKnows(t *testing.T) {
	git := newOracle(t)
	git.build()
	db := openDB(t, git.objectsDir(git.repo), Options{})
	found := collectIDs(t, db.All())
	slices.SortFunc(found, func(a, b hash.ObjectID) int { return a.Compare(b) })
	want := git.knownObjects(git.repo)
	slices.SortFunc(want, func(a, b hash.ObjectID) int { return a.Compare(b) })
	if !slices.Equal(found, want) {
		t.Fatalf("All listed %d objects, git knows %d", len(found), len(want))
	}
}

func TestOracleReadsObjectsThroughAlternates(t *testing.T) {
	git := newOracle(t)
	git.build()
	db := openDB(t, git.objectsDir(git.borrow), Options{})
	for _, id := range git.knownObjects(git.borrow) {
		kind, data, err := db.Get(id)
		if err != nil {
			t.Fatalf("Get(%s) returned error %v", id, err)
		}
		if !bytes.Equal(data, git.raw(git.borrow, "cat-file", kind.String(), id.String())) {
			t.Fatalf("Get(%s) differs from git", id)
		}
	}
	if len(db.Alternates()) != 1 {
		t.Fatalf("Open linked %d alternates, want 1", len(db.Alternates()))
	}
}

func TestOraclePeelsTagsLikeGit(t *testing.T) {
	git := newOracle(t)
	git.build()
	db := openDB(t, git.objectsDir(git.repo), Options{})
	for _, name := range []string{"v1.0", "v2.0", "lightweight", "main"} {
		id := parseID(t, strings.TrimSpace(git.run(git.repo, "rev-parse", name)))
		want := parseID(t, strings.TrimSpace(git.run(git.repo, "rev-parse", name+"^{}")))
		kind, target, err := db.Peel(id)
		if err != nil || target != want {
			t.Fatalf("Peel(%s) gave (%s, %s, %v), git says %s", name, kind, target, err, want)
		}
		isTag := strings.TrimSpace(git.run(git.repo, "cat-file", "-t", id.String())) == "tag"
		peeled, reportedTag, err := db.PeelTag(id)
		if err != nil || reportedTag != isTag {
			t.Fatalf("PeelTag(%s) gave (%s, %v, %v), git says tag is %v", name, peeled, reportedTag, err, isTag)
		}
		if isTag && peeled != want {
			t.Fatalf("PeelTag(%s) gave %s, git says %s", name, peeled, want)
		}
	}
}

func TestOracleGitReadsWhatWePut(t *testing.T) {
	git := newOracle(t)
	repo := filepath.Join(git.root, "written")
	git.init(repo)
	db := openDB(t, git.objectsDir(repo), Options{})
	content := []byte("written by gogit\n")
	blob, err := db.Put(object.TypeBlob, content)
	if err != nil {
		t.Fatalf("Put returned error %v", err)
	}
	tree := &object.Tree{Entries: []object.TreeEntry{{Mode: object.ModeBlob, Name: "file.txt", ID: blob}}}
	treeID, err := db.PutObject(tree)
	if err != nil {
		t.Fatalf("PutObject returned error %v", err)
	}
	when := time.Date(2024, time.January, 2, 3, 4, 5, 0, time.UTC)
	who := object.Signature{Name: "oracle", Email: "oracle@example.com", When: when}
	commit := &object.Commit{Tree: treeID, Author: who, Committer: who, Message: "written by gogit\n"}
	commitID, err := db.PutObject(commit)
	if err != nil {
		t.Fatalf("PutObject returned error %v", err)
	}
	tag := &object.Tag{Object: commitID, ObjectType: object.TypeCommit, Name: "gogit", Tagger: &who, Message: "tagged by gogit\n"}
	tagID, err := db.PutObject(tag)
	if err != nil {
		t.Fatalf("PutObject returned error %v", err)
	}
	git.run(repo, "update-ref", "refs/heads/main", commitID.String())
	git.run(repo, "update-ref", "refs/tags/gogit", tagID.String())
	if got := git.run(repo, "cat-file", "blob", blob.String()); got != string(content) {
		t.Fatalf("git read %q, want %q", got, content)
	}
	if got := git.run(repo, "show", commitID.String()+":file.txt"); got != string(content) {
		t.Fatalf("git show gave %q, want %q", got, content)
	}
	if got := strings.TrimSpace(git.run(repo, "rev-parse", "refs/tags/gogit^{}")); got != commitID.String() {
		t.Fatalf("git peeled the tag to %s, want %s", got, commitID)
	}
	git.fsck(repo)
}

func TestOracleGitReadsStreamedObjects(t *testing.T) {
	git := newOracle(t)
	repo := filepath.Join(git.root, "streamed")
	git.init(repo)
	db := openDB(t, git.objectsDir(repo), Options{})
	payload := bytes.Repeat([]byte("streamed by gogit\n"), 1<<16)
	writer, err := db.Writer(object.TypeBlob, int64(len(payload)))
	if err != nil {
		t.Fatalf("Writer returned error %v", err)
	}
	for chunk := range slices.Chunk(payload, 8192) {
		if _, err := writer.Write(chunk); err != nil {
			t.Fatalf("Write returned error %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close returned error %v", err)
	}
	id := writer.ID()
	if got := strings.TrimSpace(git.run(repo, "cat-file", "-s", id.String())); got != strconv.Itoa(len(payload)) {
		t.Fatalf("git says the object holds %s bytes, want %d", got, len(payload))
	}
	if !bytes.Equal(git.raw(repo, "cat-file", "blob", id.String()), payload) {
		t.Fatalf("git read different content for %s", id)
	}
	git.fsck(repo)
}

func TestOracleResolveShortAgreesWithGitRevParse(t *testing.T) {
	git := newOracle(t)
	git.build()
	db := openDB(t, git.objectsDir(git.repo), Options{})
	for _, id := range git.knownObjects(git.repo) {
		for _, length := range []int{4, 5, 6, 7} {
			prefix := id.String()[:length]
			ids, err := db.ResolveShort(prefix)
			if err != nil {
				t.Fatalf("ResolveShort(%q) returned error %v", prefix, err)
			}
			full, ok := git.tryRevParse(git.repo, prefix)
			switch {
			case ok:
				if len(ids) != 1 || ids[0].String() != full {
					t.Fatalf("ResolveShort(%q) gave %v, git resolved uniquely to %s", prefix, ids, full)
				}
			case len(ids) == 1:
				t.Fatalf("ResolveShort(%q) uniquely resolved to %s, git rejected the prefix as ambiguous or unknown", prefix, ids[0])
			}
		}
	}
}

func TestOracleAbbreviateIDAgreesWithGitShort(t *testing.T) {
	git := newOracle(t)
	git.build()
	db := openDB(t, git.objectsDir(git.repo), Options{})
	for _, id := range git.knownObjects(git.repo) {
		want := strings.TrimSpace(git.run(git.repo, "rev-parse", "--short=4", id.String()))
		got, err := db.AbbreviateID(id, 4)
		if err != nil {
			t.Fatalf("AbbreviateID(%s) returned error %v", id, err)
		}
		if got != want {
			t.Fatalf("AbbreviateID(%s) = %q, git rev-parse --short=4 gave %q", id, got, want)
		}
	}
}

func TestOracleReadsObjectsGitWroteAfterOurWrites(t *testing.T) {
	git := newOracle(t)
	repo := filepath.Join(git.root, "mixed")
	git.init(repo)
	db := openDB(t, git.objectsDir(repo), Options{})
	ours, err := db.Put(object.TypeBlob, []byte("ours\n"))
	if err != nil {
		t.Fatalf("Put returned error %v", err)
	}
	theirs := parseID(t, strings.TrimSpace(git.run(repo, "hash-object", "-w", "--stdin", "--literally")))
	if _, _, err := db.Get(theirs); err != nil {
		t.Fatalf("Get(%s) returned error %v", theirs, err)
	}
	if got := strings.TrimSpace(git.run(repo, "cat-file", "-t", ours.String())); got != "blob" {
		t.Fatalf("git says our object is a %s", got)
	}
	git.run(repo, "gc", "-q")
	if _, err := db.Reload(); err != nil {
		t.Fatalf("Reload returned error %v", err)
	}
	known, err := db.Has(ours)
	if err != nil || !known {
		t.Fatalf("Has gave (%v, %v) after git repacked the repository", known, err)
	}
}
