//go:build oracle

package index

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

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/odb"
)

type oracle struct {
	t    *testing.T
	root string
	repo string
	env  []string
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
	o := &oracle{
		t:    t,
		root: root,
		repo: filepath.Join(root, "repo"),
		env: []string{
			"PATH=" + os.Getenv("PATH"),
			"SystemRoot=" + os.Getenv("SystemRoot"),
			"HOME=" + home,
			"USERPROFILE=" + home,
			"XDG_CONFIG_HOME=" + filepath.Join(home, ".config"),
			"GIT_CONFIG_NOSYSTEM=1",
			"GIT_TERMINAL_PROMPT=0",
			"GIT_AUTHOR_NAME=oracle",
			"GIT_AUTHOR_EMAIL=oracle@example.com",
			"GIT_AUTHOR_DATE=1700000000 +0300",
			"GIT_COMMITTER_NAME=oracle",
			"GIT_COMMITTER_EMAIL=oracle@example.com",
			"GIT_COMMITTER_DATE=1700000000 +0300",
		},
	}
	o.run(root, "init", "-q", "-b", "main", "repo")
	o.git("config", "core.autocrlf", "false")
	o.git("config", "gc.auto", "0")
	return o
}

func (o *oracle) run(dir string, args ...string) string {
	o.t.Helper()
	var stderr bytes.Buffer
	cmd := exec.CommandContext(o.t.Context(), "git", args...)
	cmd.Dir = dir
	cmd.Env = o.env
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		o.t.Fatalf("git %s returned error %v: %s", strings.Join(args, " "), err, stderr.String())
	}
	return string(out)
}

func (o *oracle) git(args ...string) string {
	o.t.Helper()
	return o.run(o.repo, args...)
}

func (o *oracle) tryGit(args ...string) (string, error) {
	o.t.Helper()
	cmd := exec.CommandContext(o.t.Context(), "git", args...)
	cmd.Dir = o.repo
	cmd.Env = o.env
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (o *oracle) lines(args ...string) []string {
	o.t.Helper()
	text := strings.ReplaceAll(o.git(args...), "\r\n", "\n")
	text = strings.TrimSuffix(text, "\n")
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
}

func (o *oracle) write(name, content string) {
	o.t.Helper()
	path := filepath.Join(o.repo, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		o.t.Fatalf("MkdirAll returned error %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		o.t.Fatalf("WriteFile returned error %v", err)
	}
}

func (o *oracle) indexPath() string {
	return filepath.Join(o.repo, ".git", "index")
}

func (o *oracle) readIndex() *Index {
	o.t.Helper()
	idx, err := ReadFile(o.indexPath())
	if err != nil {
		o.t.Fatalf("ReadFile returned error %v", err)
	}
	return idx
}

func (o *oracle) objects() *odb.DB {
	o.t.Helper()
	db, err := odb.Open(filepath.Join(o.repo, ".git", "objects"), odb.Options{})
	if err != nil {
		o.t.Fatalf("odb.Open returned error %v", err)
	}
	o.t.Cleanup(func() { _ = db.Close() })
	return db
}

func (o *oracle) build() {
	o.t.Helper()
	o.write("a.txt", "alpha\n")
	o.write("b.txt", "beta\n")
	o.write("lib/library.txt", strings.Repeat("library\n", 40))
	o.write("lib/deep/note.txt", "deep\n")
	o.write("lib/deep/other.txt", "other\n")
	o.git("add", "-A")
	o.git("commit", "-q", "-m", "first")
	o.git("update-index", "--skip-worktree", "b.txt")
	o.write("intent.txt", "intent\n")
	o.git("add", "--intent-to-add", "intent.txt")
	o.git("update-index", "--assume-unchanged", "a.txt")
	blob := strings.TrimSpace(o.git("hash-object", "-w", "--stdin"))
	_ = blob
	o.git("update-index", "--add", "--cacheinfo",
		"160000,0000000000000000000000000000000000000001,module")
}

type debugEntry struct {
	mode  object.Mode
	id    hash.ObjectID
	stage Stage
	path  string
	mtime [2]uint32
	size  uint32
	flags uint64
}

func (o *oracle) debugEntries() []debugEntry {
	o.t.Helper()
	var entries []debugEntry
	for _, line := range o.lines("ls-files", "-s", "--debug") {
		if !strings.HasPrefix(line, "  ") {
			entries = append(entries, o.parseDebugHead(line))
			continue
		}
		current := &entries[len(entries)-1]
		for pair := range strings.SplitSeq(strings.TrimSpace(line), "\t") {
			key, value, found := strings.Cut(pair, ": ")
			if !found {
				continue
			}
			o.applyDebugField(current, key, value)
		}
	}
	return entries
}

func (o *oracle) parseDebugHead(line string) debugEntry {
	o.t.Helper()
	head, path, found := strings.Cut(line, "\t")
	if !found {
		o.t.Fatalf("ls-files printed %q without a tab", line)
	}
	fields := strings.Fields(head)
	if len(fields) != 3 {
		o.t.Fatalf("ls-files printed %q", head)
	}
	mode, err := object.ParseMode(fields[0])
	if err != nil {
		o.t.Fatalf("ParseMode(%q) returned error %v", fields[0], err)
	}
	stage, err := strconv.Atoi(fields[2])
	if err != nil {
		o.t.Fatalf("Atoi(%q) returned error %v", fields[2], err)
	}
	return debugEntry{mode: mode, id: mustParseID(o.t, fields[1]), stage: Stage(stage), path: path}
}

func (o *oracle) applyDebugField(entry *debugEntry, key, value string) {
	o.t.Helper()
	switch key {
	case "mtime":
		seconds, fraction, _ := strings.Cut(value, ":")
		entry.mtime = [2]uint32{uint32(o.number(seconds, 10)), uint32(o.number(fraction, 10))}
	case "size":
		entry.size = uint32(o.number(value, 10))
	case "flags":
		entry.flags = o.number(value, 16)
	}
}

func (o *oracle) number(text string, base int) uint64 {
	o.t.Helper()
	value, err := strconv.ParseUint(strings.TrimSpace(text), base, 64)
	if err != nil {
		o.t.Fatalf("ParseUint(%q) returned error %v", text, err)
	}
	return value
}

const (
	gitFlagAssumeValid  = 0x8000
	gitFlagIntentToAdd  = 1 << 29
	gitFlagSkipWorktree = 1 << 30
)

func TestOracleReadMatchesLsFilesDebugOutput(t *testing.T) {
	o := newOracle(t)
	o.build()
	want := o.debugEntries()
	idx := o.readIndex()
	if idx.Len() != len(want) {
		t.Fatalf("our index holds %d entries, git reports %d", idx.Len(), len(want))
	}
	for at, expected := range want {
		got := idx.At(at)
		if got.Path != expected.path || got.Mode != expected.mode || got.ID != expected.id || got.Stage != expected.stage {
			t.Fatalf("entry %d = (%q, %s, %s, %d), git reports (%q, %s, %s, %d)",
				at, got.Path, got.Mode, got.ID, got.Stage,
				expected.path, expected.mode, expected.id, expected.stage)
		}
		if got.Stat.Size != expected.size {
			t.Fatalf("size of %q = %d, git reports %d", got.Path, got.Stat.Size, expected.size)
		}
		mtime := [2]uint32{uint32(got.Stat.MTime.Unix()), uint32(got.Stat.MTime.Nanosecond())}
		if mtime != expected.mtime {
			t.Fatalf("mtime of %q = %v, git reports %v", got.Path, mtime, expected.mtime)
		}
		if got.AssumeValid != (expected.flags&gitFlagAssumeValid != 0) ||
			got.SkipWorktree != (expected.flags&gitFlagSkipWorktree != 0) ||
			got.IntentToAdd != (expected.flags&gitFlagIntentToAdd != 0) {
			t.Fatalf("flags of %q = (%v, %v, %v), git reports 0x%x", got.Path,
				got.AssumeValid, got.SkipWorktree, got.IntentToAdd, expected.flags)
		}
	}
}

func TestOracleReadMatchesLsFilesDuringAConflict(t *testing.T) {
	o := newOracle(t)
	o.write("conflict.txt", "base\n")
	o.write("keep.txt", "keep\n")
	o.git("add", "-A")
	o.git("commit", "-q", "-m", "base")
	o.git("checkout", "-q", "-b", "side")
	o.write("conflict.txt", "side\n")
	o.git("commit", "-q", "-am", "side")
	o.git("checkout", "-q", "main")
	o.write("conflict.txt", "main\n")
	o.git("commit", "-q", "-am", "main")
	if _, err := o.tryGit("merge", "side"); err == nil {
		t.Fatal("git merge did not report a conflict")
	}
	idx := o.readIndex()
	if !idx.HasConflicts() {
		t.Fatal("our index reports no conflicts")
	}
	want := o.debugEntries()
	if idx.Len() != len(want) {
		t.Fatalf("our index holds %d entries, git reports %d", idx.Len(), len(want))
	}
	for at, expected := range want {
		got := idx.At(at)
		if got.Path != expected.path || got.Stage != expected.stage || got.ID != expected.id {
			t.Fatalf("entry %d = (%q, %d, %s), git reports (%q, %d, %s)",
				at, got.Path, got.Stage, got.ID, expected.path, expected.stage, expected.id)
		}
	}
	if len(idx.Conflicts("conflict.txt")) != 3 {
		t.Fatalf("Conflicts returned %d entries", len(idx.Conflicts("conflict.txt")))
	}
}

func TestOracleGitReadsWhatWeWrite(t *testing.T) {
	for _, version := range []int{Version2, Version3, Version4} {
		t.Run(fmt.Sprintf("version%d", version), func(t *testing.T) {
			o := newOracle(t)
			o.build()
			o.git("write-tree")
			before := o.lines("ls-files", "-s")
			status := o.lines("status", "--porcelain")
			idx := o.readIndex()
			if err := os.Remove(o.indexPath()); err != nil {
				t.Fatalf("Remove returned error %v", err)
			}
			if err := idx.WriteFile(o.indexPath(), version); err != nil {
				t.Fatalf("WriteFile returned error %v", err)
			}
			if after := o.lines("ls-files", "-s"); !slices.Equal(after, before) {
				t.Fatalf("git reads %v from our index, it read %v from its own", after, before)
			}
			if after := o.lines("status", "--porcelain"); !slices.Equal(after, status) {
				t.Fatalf("git status reports %v after our write, it reported %v before", after, status)
			}
			if out, err := o.tryGit("fsck", "--no-progress"); err != nil {
				t.Fatalf("git fsck returned error %v: %s", err, out)
			}
			if got := o.lines("ls-files", "-v"); len(got) == 0 {
				t.Fatal("git ls-files -v printed nothing")
			}
		})
	}
}

func TestOracleGitAgreesWithOurCacheTree(t *testing.T) {
	o := newOracle(t)
	o.write("a.txt", "alpha\n")
	o.write("lib/library.txt", "library\n")
	o.write("lib/deep/note.txt", "deep\n")
	o.git("add", "-A")
	idx := o.readIndex()
	idx.CacheTree = nil
	id, err := idx.WriteTree(o.objects())
	if err != nil {
		t.Fatalf("WriteTree returned error %v", err)
	}
	if want := strings.TrimSpace(o.git("write-tree")); id.String() != want {
		t.Fatalf("WriteTree returned %s, git write-tree returned %s", id, want)
	}
	if err := os.Remove(o.indexPath()); err != nil {
		t.Fatalf("Remove returned error %v", err)
	}
	if err := idx.WriteFile(o.indexPath(), Version2); err != nil {
		t.Fatalf("WriteFile returned error %v", err)
	}
	if again := strings.TrimSpace(o.git("write-tree")); again != id.String() {
		t.Fatalf("git write-tree returned %s after our write, want %s", again, id)
	}
	if out, err := o.tryGit("fsck", "--no-progress"); err != nil {
		t.Fatalf("git fsck returned error %v: %s", err, out)
	}
}

func TestOracleWriteTreeReusesTheCacheTreeGitStored(t *testing.T) {
	o := newOracle(t)
	o.write("a.txt", "alpha\n")
	o.write("lib/deep/note.txt", "deep\n")
	o.git("add", "-A")
	want := strings.TrimSpace(o.git("write-tree"))
	idx := o.readIndex()
	if idx.CacheTree == nil || !idx.CacheTree.Valid() {
		t.Fatal("git did not store a valid cache tree")
	}
	id, err := idx.WriteTree(o.objects())
	if err != nil {
		t.Fatalf("WriteTree returned error %v", err)
	}
	if id.String() != want {
		t.Fatalf("WriteTree returned %s, git write-tree returned %s", id, want)
	}
}

func TestOracleGitSeesOurAddAndRemove(t *testing.T) {
	o := newOracle(t)
	o.build()
	o.git("write-tree")
	idx := o.readIndex()
	o.write("added.txt", "added\n")
	blob := strings.TrimSpace(o.git("hash-object", "-w", filepath.Join(o.repo, "added.txt")))
	idx.Add(Entry{Path: "added.txt", Mode: object.ModeBlob, ID: mustParseID(t, blob)})
	if !idx.Remove("b.txt") {
		t.Fatal("Remove reported that nothing was removed")
	}
	if err := os.Remove(o.indexPath()); err != nil {
		t.Fatalf("Remove returned error %v", err)
	}
	if err := idx.WriteFile(o.indexPath(), Version2); err != nil {
		t.Fatalf("WriteFile returned error %v", err)
	}
	listed := o.lines("ls-files", "-s")
	if !slices.ContainsFunc(listed, func(line string) bool { return strings.HasSuffix(line, "\tadded.txt") }) {
		t.Fatalf("git does not list the entry we added: %v", listed)
	}
	if slices.ContainsFunc(listed, func(line string) bool { return strings.HasSuffix(line, "\tb.txt") }) {
		t.Fatalf("git still lists the entry we removed: %v", listed)
	}
	id, err := idx.WriteTree(o.objects())
	if err != nil {
		t.Fatalf("WriteTree returned error %v", err)
	}
	if err := os.Remove(o.indexPath()); err != nil {
		t.Fatalf("Remove returned error %v", err)
	}
	if err := idx.WriteFile(o.indexPath(), Version2); err != nil {
		t.Fatalf("WriteFile returned error %v", err)
	}
	if want := strings.TrimSpace(o.git("write-tree")); want != id.String() {
		t.Fatalf("git write-tree returned %s after our changes, we wrote %s", want, id)
	}
	if out, err := o.tryGit("fsck", "--no-progress"); err != nil {
		t.Fatalf("git fsck returned error %v: %s", err, out)
	}
}

func TestOracleReadsIndexOfEveryVersionGitWrites(t *testing.T) {
	for _, version := range []int{2, 3, 4} {
		t.Run(fmt.Sprintf("version%d", version), func(t *testing.T) {
			o := newOracle(t)
			o.build()
			o.git("update-index", "--index-version", strconv.Itoa(version))
			raw, err := os.ReadFile(o.indexPath())
			if err != nil {
				t.Fatalf("ReadFile returned error %v", err)
			}
			idx := o.readIndex()
			if got := encodeIndex(t, idx, idx.Version); !bytes.Equal(got, raw) {
				t.Fatalf("our rewrite of a version %d index differs from the file git wrote", version)
			}
			if want := o.lines("ls-files", "-s"); len(want) != idx.Len() {
				t.Fatalf("our index holds %d entries, git lists %d", idx.Len(), len(want))
			}
		})
	}
}

func TestOracleRejectsTheSplitIndexGitWrites(t *testing.T) {
	o := newOracle(t)
	o.write("a.txt", "alpha\n")
	o.git("add", "-A")
	o.git("commit", "-q", "-m", "base")
	o.git("update-index", "--split-index")
	o.write("b.txt", "beta\n")
	o.git("add", "b.txt")
	if _, err := ReadFile(o.indexPath()); err == nil {
		t.Fatal("ReadFile accepted a split index")
	}
}

func TestOracleUntrackedCacheSurvivesOurRewrite(t *testing.T) {
	o := newOracle(t)
	o.write("a.txt", "alpha\n")
	o.write("sub/b.txt", "beta\n")
	o.write(".gitignore", "ignored\n")
	o.git("add", "-A")
	o.git("commit", "-q", "-m", "base")
	o.write("untracked.txt", "u\n")
	if out, err := o.tryGit("update-index", "--untracked-cache"); err != nil {
		t.Skipf("the untracked cache is not available here: %v: %s", err, out)
	}
	o.git("status", "--porcelain")
	raw, err := os.ReadFile(o.indexPath())
	if err != nil {
		t.Fatalf("ReadFile returned error %v", err)
	}
	idx := o.readIndex()
	if idx.Untracked == nil {
		t.Skip("git did not store an untracked cache")
	}
	if got := encodeIndex(t, idx, idx.Version); !bytes.Equal(got, raw) {
		t.Fatal("our rewrite of an index with an untracked cache differs from the file git wrote")
	}
	if err := os.Remove(o.indexPath()); err != nil {
		t.Fatalf("Remove returned error %v", err)
	}
	if err := idx.WriteFile(o.indexPath(), Version2); err != nil {
		t.Fatalf("WriteFile returned error %v", err)
	}
	if out, err := o.tryGit("status", "--porcelain"); err != nil {
		t.Fatalf("git status returned error %v: %s", err, out)
	}
}

func TestOracleResolveUndoSurvivesOurRewrite(t *testing.T) {
	o := newOracle(t)
	o.write("conflict.txt", "base\n")
	o.git("add", "-A")
	o.git("commit", "-q", "-m", "base")
	o.git("checkout", "-q", "-b", "side")
	o.write("conflict.txt", "side\n")
	o.git("commit", "-q", "-am", "side")
	o.git("checkout", "-q", "main")
	o.write("conflict.txt", "main\n")
	o.git("commit", "-q", "-am", "main")
	if _, err := o.tryGit("merge", "side"); err == nil {
		t.Fatal("git merge did not report a conflict")
	}
	o.write("conflict.txt", "resolved\n")
	o.git("add", "conflict.txt")
	want := o.lines("ls-files", "--resolve-undo")
	if len(want) != 3 {
		t.Fatalf("git reports %d resolve undo entries", len(want))
	}
	idx := o.readIndex()
	if len(idx.ResolveUndo) != 1 {
		t.Fatalf("our index holds %d resolve undo entries", len(idx.ResolveUndo))
	}
	if err := os.Remove(o.indexPath()); err != nil {
		t.Fatalf("Remove returned error %v", err)
	}
	if err := idx.WriteFile(o.indexPath(), Version2); err != nil {
		t.Fatalf("WriteFile returned error %v", err)
	}
	if got := o.lines("ls-files", "--resolve-undo"); !slices.Equal(got, want) {
		t.Fatalf("git reports %v after our write, it reported %v before", got, want)
	}
}
