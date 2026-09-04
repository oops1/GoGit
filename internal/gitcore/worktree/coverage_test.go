package worktree

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/index"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/repo"
)

func TestCollectHeadTreeReturnsAnEmptyMapForAZeroID(t *testing.T) {
	tr := newTestRepo(t)
	w := tr.open()
	tree, err := w.collectHeadTree(t.Context(), hash.Zero)
	if err != nil {
		t.Fatalf("collectHeadTree returned error %v", err)
	}
	if len(tree) != 0 {
		t.Fatalf("collectHeadTree(zero) = %#v, want empty", tree)
	}
}

func TestStatusReportsStagedTypeChange(t *testing.T) {
	tr := newTestRepo(t)
	tr.stage("x.txt", "hello\n")
	tr.commit("initial")
	tr.unstage("x.txt")
	linkID, err := tr.db.Put(object.TypeBlob, []byte("target"))
	if err != nil {
		t.Fatalf("Put returned error %v", err)
	}
	tr.idx.Add(index.Entry{Path: "x.txt", Mode: object.ModeSymlink, ID: linkID, Stage: index.StageMerged})
	w := tr.open()
	status, err := w.Status(t.Context())
	if err != nil {
		t.Fatalf("Status returned error %v", err)
	}
	entry, ok := entryMap(status.Entries)["x.txt"]
	if !ok || entry.Staged != StatusTypeChanged {
		t.Fatalf("x.txt entry = %#v, want Staged=TypeChanged", entry)
	}
}

func TestStatusMatchesMultipleRenamesWithIdenticalContent(t *testing.T) {
	tr := newTestRepo(t)
	tr.stage("a.txt", "dup\n")
	tr.stage("b.txt", "dup\n")
	tr.commit("initial")
	tr.unstage("a.txt")
	tr.unstage("b.txt")
	tr.writeFile("c.txt", "dup\n")
	tr.stageExisting("c.txt", object.ModeBlob)
	tr.writeFile("d.txt", "dup\n")
	tr.stageExisting("d.txt", object.ModeBlob)
	w := tr.open()
	status, err := w.Status(t.Context())
	if err != nil {
		t.Fatalf("Status returned error %v", err)
	}
	entries := entryMap(status.Entries)
	cEntry, cOK := entries["c.txt"]
	dEntry, dOK := entries["d.txt"]
	if !cOK || !dOK || cEntry.Staged != StatusRenamed || dEntry.Staged != StatusRenamed {
		t.Fatalf("c.txt=%#v d.txt=%#v, want both Staged=Renamed", cEntry, dEntry)
	}
	origins := map[string]bool{cEntry.OrigPath: true, dEntry.OrigPath: true}
	if !origins["a.txt"] || !origins["b.txt"] {
		t.Fatalf("origins = %v, want a.txt and b.txt", origins)
	}
}

func TestStatusDoesNotMatchARenameAcrossDifferentObjectTypes(t *testing.T) {
	tr := newTestRepo(t)
	id, err := tr.db.Put(object.TypeBlob, []byte("hello\n"))
	if err != nil {
		t.Fatalf("Put returned error %v", err)
	}
	tr.idx.Add(index.Entry{Path: "a.txt", Mode: object.ModeBlob, ID: id, Stage: index.StageMerged})
	tr.commit("initial")
	tr.unstage("a.txt")
	tr.idx.Add(index.Entry{Path: "sub", Mode: object.ModeSubmodule, ID: id, Stage: index.StageMerged})
	w := tr.open()
	status, err := w.Status(t.Context())
	if err != nil {
		t.Fatalf("Status returned error %v", err)
	}
	entries := entryMap(status.Entries)
	subEntry, ok := entries["sub"]
	if !ok || subEntry.Staged != StatusAdded || subEntry.OrigPath != "" {
		t.Fatalf("sub entry = %#v, want Staged=Added with no OrigPath", subEntry)
	}
	aEntry, ok := entries["a.txt"]
	if !ok || aEntry.Staged != StatusDeleted {
		t.Fatalf("a.txt entry = %#v, want Staged=Deleted", aEntry)
	}
}

func TestStatusHidesADirectoryContainingOnlyIgnoredFiles(t *testing.T) {
	tr := newTestRepo(t)
	tr.stage(".gitignore", "*.log\n")
	tr.commit("initial")
	tr.writeFile("mixed/notes.log", "content\n")
	w := tr.open()
	status, err := w.Status(t.Context())
	if err != nil {
		t.Fatalf("Status returned error %v", err)
	}
	for path := range entryMap(status.Entries) {
		if path == "mixed/" || path == "mixed/notes.log" {
			t.Fatalf("a directory holding only ignored files should not be reported, got %q", path)
		}
	}
}

func TestStatusPropagatesReadDirFailuresFromNestedUntrackedDirectories(t *testing.T) {
	tr := newTestRepo(t)
	tr.stage("a.txt", "hi\n")
	tr.commit("initial")
	tr.writeFile("top/inner/deep.txt", "x\n")
	w := tr.open()
	original := fsOpenDir
	fsOpenDir = func(root *os.Root, name string) (*os.File, error) {
		if name == filepath.FromSlash("top/inner") {
			return nil, errors.New("boom")
		}
		return original(root, name)
	}
	t.Cleanup(func() { fsOpenDir = original })
	if _, err := w.Status(t.Context()); err == nil {
		t.Fatalf("Status returned no error")
	}
}

func TestCompareToWorktreeDetectsExecutableBitEvenWhenStatCacheMatches(t *testing.T) {
	tr := newTestRepo(t)
	id, err := tr.db.Put(object.TypeBlob, []byte("content"))
	if err != nil {
		t.Fatalf("Put returned error %v", err)
	}
	mtime := time.Unix(500, 0)
	tr.idx.Add(index.Entry{
		Path: "run.sh",
		Mode: object.ModeBlob,
		ID:   id,
		Stat: index.Stat{MTime: mtime, Size: 7},
	})
	w := tr.open()
	w.fileMode = true
	original := fsLstatFile
	fsLstatFile = func(root *os.Root, name string) (os.FileInfo, error) {
		return fakeFileInfo{mode: 0o755, size: 7, modTime: mtime}, nil
	}
	t.Cleanup(func() { fsLstatFile = original })
	status, err := w.Status(t.Context())
	if err != nil {
		t.Fatalf("Status returned error %v", err)
	}
	entry, ok := entryMap(status.Entries)["run.sh"]
	if !ok || entry.Unstaged != StatusModified {
		t.Fatalf("run.sh entry = %#v, want Unstaged=Modified", entry)
	}
}

func TestCompareToWorktreeDetectsExecutableBitAfterAFullContentRead(t *testing.T) {
	tr := newTestRepo(t)
	tr.writeFile("run2.sh", "echo\n")
	tr.stageContent("run2.sh", "echo\n")
	w := tr.open()
	w.fileMode = true
	original := fsLstatFile
	fsLstatFile = func(root *os.Root, name string) (os.FileInfo, error) {
		return fakeFileInfo{mode: 0o755, size: 999, modTime: time.Unix(999, 0)}, nil
	}
	t.Cleanup(func() { fsLstatFile = original })
	status, err := w.Status(t.Context())
	if err != nil {
		t.Fatalf("Status returned error %v", err)
	}
	entry, ok := entryMap(status.Entries)["run2.sh"]
	if !ok || entry.Unstaged != StatusModified {
		t.Fatalf("run2.sh entry = %#v, want Unstaged=Modified", entry)
	}
}

func TestCompareToWorktreeFailsWhenHashingASymlinkTargetWithAnUnsupportedFormat(t *testing.T) {
	tr := newTestRepo(t)
	linkID, err := tr.db.Put(object.TypeBlob, []byte("target"))
	if err != nil {
		t.Fatalf("Put returned error %v", err)
	}
	tr.idx.Add(index.Entry{
		Path: "link",
		Mode: object.ModeSymlink,
		ID:   linkID,
		Stat: index.Stat{MTime: time.Unix(1, 0), Size: 3},
	})
	w := tr.open()
	w.format = hash.Format(99)
	originalLstat, originalReadlink := fsLstatFile, fsReadlinkFile
	fsLstatFile = func(root *os.Root, name string) (os.FileInfo, error) {
		return fakeFileInfo{mode: os.ModeSymlink, size: 999, modTime: time.Unix(2, 0)}, nil
	}
	fsReadlinkFile = func(root *os.Root, name string) (string, error) { return "other-target", nil }
	t.Cleanup(func() {
		fsLstatFile = originalLstat
		fsReadlinkFile = originalReadlink
	})
	if _, err := w.Status(t.Context()); err == nil {
		t.Fatalf("Status returned no error, want an unsupported format failure")
	}
}

func TestCompareToWorktreeFailsWhenHashingFileContentWithAnUnsupportedFormat(t *testing.T) {
	tr := newTestRepo(t)
	tr.writeFile("plain.txt", "content\n")
	tr.stageContent("plain.txt", "content\n")
	w := tr.open()
	w.format = hash.Format(99)
	if _, err := w.Status(t.Context()); err == nil {
		t.Fatalf("Status returned no error, want an unsupported format failure")
	}
}

func TestOpenFailsWhenTheWorkingTreeDirectoryIsMissing(t *testing.T) {
	base := t.TempDir()
	workDir := filepath.Join(base, "work")
	gitDir := filepath.Join(base, "git")
	r, err := repo.Init(workDir, repo.InitOptions{InitialBranch: "main", SeparateGitDir: gitDir})
	if err != nil {
		t.Fatalf("repo.Init returned error %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })
	db, err := odb.Open(r.ObjectsDir(), odb.Options{})
	if err != nil {
		t.Fatalf("odb.Open returned error %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := os.RemoveAll(workDir); err != nil {
		t.Fatalf("RemoveAll returned error %v", err)
	}
	if _, err := Open(r, Options{DB: db}); err == nil {
		t.Fatalf("Open returned no error, want a missing working tree failure")
	}
}

func TestAttributesFileOfFallsBackWhenConfiguredValueIsEmpty(t *testing.T) {
	tr := newTestRepo(t)
	tr.saveIndex()
	tr.appendConfig("[core]\n\tattributesfile = \n")
	r2 := tr.reopen()
	w, err := Open(r2, Options{DB: tr.db, Refs: tr.refs})
	if err != nil {
		t.Fatalf("Open returned error %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })
}
