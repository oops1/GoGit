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
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/repo"
)

type fakeFileInfo struct {
	name    string
	size    int64
	mode    os.FileMode
	modTime time.Time
	dir     bool
}

func (f fakeFileInfo) Name() string       { return f.name }
func (f fakeFileInfo) Size() int64        { return f.size }
func (f fakeFileInfo) Mode() os.FileMode  { return f.mode }
func (f fakeFileInfo) ModTime() time.Time { return f.modTime }
func (f fakeFileInfo) IsDir() bool        { return f.dir }
func (f fakeFileInfo) Sys() any           { return nil }

func TestKindOfModeClassifiesEachModeKind(t *testing.T) {
	tests := []struct {
		mode object.Mode
		want entryKind
	}{
		{object.ModeBlob, kindRegular},
		{object.ModeExecutable, kindRegular},
		{object.ModeSymlink, kindSymlink},
		{object.ModeSubmodule, kindSubmodule},
	}
	for _, tc := range tests {
		if got := kindOfMode(tc.mode); got != tc.want {
			t.Errorf("kindOfMode(%v) = %v, want %v", tc.mode, got, tc.want)
		}
	}
}

func TestKindOfInfoClassifiesEachFileKind(t *testing.T) {
	tests := []struct {
		name string
		fi   fakeFileInfo
		want entryKind
	}{
		{"regular", fakeFileInfo{}, kindRegular},
		{"symlink", fakeFileInfo{mode: os.ModeSymlink}, kindSymlink},
		{"directory", fakeFileInfo{dir: true}, kindSubmodule},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := kindOfInfo(tc.fi); got != tc.want {
				t.Errorf("kindOfInfo(%v) = %v, want %v", tc.fi, got, tc.want)
			}
		})
	}
}

func TestStatusDetectsTypeChangeFromFileToDirectory(t *testing.T) {
	tr := newTestRepo(t)
	tr.stage("thing.txt", "content\n")
	tr.commit("initial")
	tr.remove("thing.txt")
	tr.mkdir("thing.txt")
	w := tr.open()
	status, err := w.Status(t.Context())
	if err != nil {
		t.Fatalf("Status returned error %v", err)
	}
	entry, ok := entryMap(status.Entries)["thing.txt"]
	if !ok || entry.Unstaged != StatusTypeChanged {
		t.Fatalf("thing.txt entry = %#v, want Unstaged=TypeChanged", entry)
	}
}

func TestResolveHeadFailsWhenHeadFormsASymbolicCycle(t *testing.T) {
	tr := newTestRepo(t)
	tr.stage("a.txt", "hi\n")
	tr.commit("initial")
	writeCommon := func(rel, content string) {
		if err := os.WriteFile(tr.repo.CommonPath(rel), []byte(content), 0o666); err != nil {
			t.Fatalf("WriteFile returned error %v", err)
		}
	}
	if err := os.WriteFile(tr.repo.GitPath("HEAD"), []byte("ref: refs/heads/a\n"), 0o666); err != nil {
		t.Fatalf("WriteFile returned error %v", err)
	}
	writeCommon("refs/heads/a", "ref: refs/heads/b\n")
	writeCommon("refs/heads/b", "ref: refs/heads/a\n")
	w := tr.open()
	if _, err := w.Status(t.Context()); !errors.Is(err, refs.ErrTooManySymlinks) {
		t.Fatalf("Status returned error %v, want ErrTooManySymlinks", err)
	}
}

func TestResolveHeadFailsWhenHeadContentIsMalformed(t *testing.T) {
	tr := newTestRepo(t)
	tr.stage("a.txt", "hi\n")
	tr.commit("initial")
	if err := os.WriteFile(tr.repo.GitPath("HEAD"), []byte("not a ref\n"), 0o666); err != nil {
		t.Fatalf("WriteFile returned error %v", err)
	}
	w := tr.open()
	if _, err := w.Status(t.Context()); err == nil {
		t.Fatalf("Status returned no error, want a malformed HEAD error")
	}
}

func TestResolveHeadFailsWhenTheBranchRefIsMalformed(t *testing.T) {
	tr := newTestRepo(t)
	tr.stage("a.txt", "hi\n")
	tr.commit("initial")
	if err := os.WriteFile(tr.repo.CommonPath("refs/heads/main"), []byte("not a hash\n"), 0o666); err != nil {
		t.Fatalf("WriteFile returned error %v", err)
	}
	w := tr.open()
	if _, err := w.Status(t.Context()); err == nil {
		t.Fatalf("Status returned no error, want a malformed ref error")
	}
}

func TestResolveHeadTreatsAMissingHeadFileAsDetachedUnborn(t *testing.T) {
	tr := newTestRepo(t)
	if err := os.Remove(tr.repo.GitPath("HEAD")); err != nil {
		t.Fatalf("Remove returned error %v", err)
	}
	w := tr.open()
	status, err := w.Status(t.Context())
	if err != nil {
		t.Fatalf("Status returned error %v", err)
	}
	if !status.Detached || status.HeadBranch != "" {
		t.Fatalf("Detached=%v HeadBranch=%q, want true/empty", status.Detached, status.HeadBranch)
	}
}

func TestStatusFailsWhenTheHeadCommitObjectIsMissing(t *testing.T) {
	tr := newTestRepo(t)
	tr.stage("a.txt", "hi\n")
	tr.commit("initial")
	tr.setBranchTarget(hash.SumSHA1("commit", []byte("does not exist")))
	w := tr.open()
	if _, err := w.Status(t.Context()); !errors.Is(err, ErrReadHead) {
		t.Fatalf("Status returned error %v, want ErrReadHead", err)
	}
}

func TestStatusFailsWhenTheHeadTreeObjectIsMissing(t *testing.T) {
	tr := newTestRepo(t)
	tr.stage("a.txt", "hi\n")
	tr.commit("initial")
	missingTree := hash.SumSHA1("tree", []byte("does not exist"))
	tr.commitWithTree(missingTree, "broken")
	w := tr.open()
	if _, err := w.Status(t.Context()); !errors.Is(err, ErrReadHeadTree) {
		t.Fatalf("Status returned error %v, want ErrReadHeadTree", err)
	}
}

func TestCollectHeadTreeStopsWhenContextIsCanceled(t *testing.T) {
	tr := newTestRepo(t)
	tr.stage("a.txt", "hi\n")
	tr.stage("dir/b.txt", "hi\n")
	commitID := tr.commit("initial")
	w := tr.open()
	commit, err := w.db.Commit(commitID)
	if err != nil {
		t.Fatalf("Commit returned error %v", err)
	}
	if _, err := w.collectHeadTree(newStepContext(0), commit.Tree); err == nil {
		t.Fatalf("collectHeadTree returned no error for a canceled context")
	}
}

func TestUnstagedStatusesStopsWhenContextIsCanceled(t *testing.T) {
	tr := newTestRepo(t)
	tr.stage("a.txt", "hi\n")
	tr.stage("b.txt", "hi\n")
	tr.commit("initial")
	w := tr.open()
	var entries []*index.Entry
	for entry := range w.index.Entries() {
		entries = append(entries, entry)
	}
	if _, err := w.unstagedStatuses(newStepContext(0), entries); err == nil {
		t.Fatalf("unstagedStatuses returned no error for a canceled context")
	}
}

func TestUntrackedEntriesStopsWhenContextIsCanceled(t *testing.T) {
	tr := newTestRepo(t)
	tr.writeFile("a.txt", "hi\n")
	tr.writeFile("dir/b.txt", "hi\n")
	w := tr.open()
	if _, err := w.untrackedEntries(newStepContext(0), map[string]bool{}, map[string]bool{}); err == nil {
		t.Fatalf("untrackedEntries returned no error for a canceled context")
	}
}

func TestOpenHonorsAnExplicitWorkerCount(t *testing.T) {
	tr := newTestRepo(t)
	w := tr.openWith(Options{Workers: 3})
	if w.workers != 3 {
		t.Fatalf("workers = %d, want 3", w.workers)
	}
}

func TestOpenFailsWhenTheRefsStoreCannotBeOpened(t *testing.T) {
	base := t.TempDir()
	workDir := filepath.Join(base, "work")
	gitDir := filepath.Join(base, "git")
	r, err := repo.Init(workDir, repo.InitOptions{InitialBranch: "main", SeparateGitDir: gitDir})
	if err != nil {
		t.Fatalf("repo.Init returned error %v", err)
	}
	db, err := odb.Open(r.ObjectsDir(), odb.Options{})
	if err != nil {
		t.Fatalf("odb.Open returned error %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close returned error %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("Close returned error %v", err)
	}
	if err := os.RemoveAll(gitDir); err != nil {
		t.Fatalf("RemoveAll returned error %v", err)
	}
	if _, err := Open(r, Options{DB: db}); err == nil {
		t.Fatalf("Open returned no error, want a refs.Open failure")
	}
}

func TestOpenFailsWhenTheAttributesFileConfigIsInvalid(t *testing.T) {
	tr := newTestRepo(t)
	tr.saveIndex()
	tr.appendConfig("[core]\n\tattributesfile = ~bob/x\n")
	r2 := tr.reopen()
	if _, err := Open(r2, Options{DB: tr.db, Refs: tr.refs}); err == nil {
		t.Fatalf("Open returned no error, want an invalid attributesfile error")
	}
}

func TestReadDirFailsWhenOpeningTheDirectoryFails(t *testing.T) {
	tr := newTestRepo(t)
	w := tr.open()
	original := fsOpenDir
	fsOpenDir = func(root *os.Root, name string) (*os.File, error) { return nil, errors.New("boom") }
	t.Cleanup(func() { fsOpenDir = original })
	if _, err := w.readDir(""); err == nil {
		t.Fatalf("readDir returned no error")
	}
}

func TestReadDirFailsWhenReadingEntriesFails(t *testing.T) {
	tr := newTestRepo(t)
	w := tr.open()
	original := fsReadDir
	fsReadDir = func(file *os.File) ([]os.DirEntry, error) { return nil, errors.New("boom") }
	t.Cleanup(func() { fsReadDir = original })
	if _, err := w.readDir(""); err == nil {
		t.Fatalf("readDir returned no error")
	}
}

func TestReadDirFailsWhenClosingTheDirectoryFails(t *testing.T) {
	tr := newTestRepo(t)
	w := tr.open()
	original := fsCloseDir
	fsCloseDir = func(file *os.File) error { return errors.New("boom") }
	t.Cleanup(func() { fsCloseDir = original })
	if _, err := w.readDir(""); err == nil {
		t.Fatalf("readDir returned no error")
	}
}

func TestStatusFailsWhenTheWorkingTreeCannotBeListed(t *testing.T) {
	tr := newTestRepo(t)
	tr.stage("a.txt", "hi\n")
	tr.commit("initial")
	w := tr.open()
	original := fsOpenDir
	fsOpenDir = func(root *os.Root, name string) (*os.File, error) { return nil, errors.New("boom") }
	t.Cleanup(func() { fsOpenDir = original })
	if _, err := w.Status(t.Context()); err == nil {
		t.Fatalf("Status returned no error")
	}
}

func TestUntrackedEntriesPropagatesFailuresFromTrackedSubdirectories(t *testing.T) {
	tr := newTestRepo(t)
	tr.stage("dir/tracked.txt", "hi\n")
	tr.commit("initial")
	w := tr.open()
	original := fsOpenDir
	fsOpenDir = func(root *os.Root, name string) (*os.File, error) {
		if name == filepath.FromSlash("dir") {
			return nil, errors.New("boom")
		}
		return original(root, name)
	}
	t.Cleanup(func() { fsOpenDir = original })
	if _, err := w.Status(t.Context()); err == nil {
		t.Fatalf("Status returned no error")
	}
}

func TestUntrackedEntriesPropagatesFailuresFromUntrackedSubdirectories(t *testing.T) {
	tr := newTestRepo(t)
	tr.stage("a.txt", "hi\n")
	tr.commit("initial")
	tr.writeFile("problem/inside.txt", "x\n")
	w := tr.open()
	original := fsOpenDir
	fsOpenDir = func(root *os.Root, name string) (*os.File, error) {
		if name == filepath.FromSlash("problem") {
			return nil, errors.New("boom")
		}
		return original(root, name)
	}
	t.Cleanup(func() { fsOpenDir = original })
	if _, err := w.Status(t.Context()); err == nil {
		t.Fatalf("Status returned no error")
	}
}

func TestUntrackedEntriesPropagatesFailuresWhileScanningAnIgnoredSubdirectoryForAnyFile(t *testing.T) {
	tr := newTestRepo(t)
	tr.stage(".gitignore", "problem/only/\n")
	tr.stage("a.txt", "hi\n")
	tr.commit("initial")
	tr.writeFile("problem/only/inside.txt", "x\n")
	w := tr.open()
	original := fsOpenDir
	fsOpenDir = func(root *os.Root, name string) (*os.File, error) {
		if name == filepath.FromSlash("problem/only") {
			return nil, errors.New("boom")
		}
		return original(root, name)
	}
	t.Cleanup(func() { fsOpenDir = original })
	if _, err := w.Status(t.Context()); err == nil {
		t.Fatalf("Status returned no error")
	}
}

func TestStatusFailsWhenTheFileLimitIsExceededInATrackedSubdirectory(t *testing.T) {
	tr := newTestRepo(t)
	tr.stage("dir/one.txt", "1\n")
	tr.stage("dir/two.txt", "2\n")
	tr.stage("dir/three.txt", "3\n")
	tr.commit("initial")
	w := tr.openWith(Options{MaxFiles: 2})
	if _, err := w.Status(t.Context()); !errors.Is(err, ErrTooManyFiles) {
		t.Fatalf("Status returned error %v, want ErrTooManyFiles", err)
	}
}

func TestCompareToWorktreeFailsWhenLstatFails(t *testing.T) {
	tr := newTestRepo(t)
	tr.stage("a.txt", "hi\n")
	tr.commit("initial")
	w := tr.open()
	original := fsLstatFile
	fsLstatFile = func(root *os.Root, name string) (os.FileInfo, error) { return nil, errors.New("boom") }
	t.Cleanup(func() { fsLstatFile = original })
	if _, err := w.Status(t.Context()); err == nil {
		t.Fatalf("Status returned no error")
	}
}

func TestCompareToWorktreeFailsWhenReadFileFails(t *testing.T) {
	tr := newTestRepo(t)
	tr.stageContent("a.txt", "content\n")
	tr.commit("initial")
	tr.writeFile("a.txt", "content\n")
	w := tr.open()
	original := fsReadFileFile
	fsReadFileFile = func(root *os.Root, name string) ([]byte, error) { return nil, errors.New("boom") }
	t.Cleanup(func() { fsReadFileFile = original })
	if _, err := w.Status(t.Context()); err == nil {
		t.Fatalf("Status returned no error")
	}
}

func TestCompareToWorktreeDetectsSymlinkChangesUsingFaultInjection(t *testing.T) {
	tr := newTestRepo(t)
	oldID, err := tr.db.Put(object.TypeBlob, []byte("old-target"))
	if err != nil {
		t.Fatalf("Put returned error %v", err)
	}
	tr.idx.Add(index.Entry{
		Path: "link",
		Mode: object.ModeSymlink,
		ID:   oldID,
		Stat: index.Stat{MTime: time.Unix(1, 0), Size: 3},
	})
	w := tr.open()

	originalLstat, originalReadlink := fsLstatFile, fsReadlinkFile
	t.Cleanup(func() {
		fsLstatFile = originalLstat
		fsReadlinkFile = originalReadlink
	})
	fsLstatFile = func(root *os.Root, name string) (os.FileInfo, error) {
		return fakeFileInfo{mode: os.ModeSymlink, size: 999, modTime: time.Unix(2, 0)}, nil
	}
	fsReadlinkFile = func(root *os.Root, name string) (string, error) { return "new-target", nil }

	status, err := w.Status(t.Context())
	if err != nil {
		t.Fatalf("Status returned error %v", err)
	}
	entry, ok := entryMap(status.Entries)["link"]
	if !ok || entry.Unstaged != StatusModified {
		t.Fatalf("link entry = %#v, want Unstaged=Modified", entry)
	}
}

func TestCompareToWorktreeIsUnmodifiedWhenTheSymlinkTargetIsUnchanged(t *testing.T) {
	tr := newTestRepo(t)
	oldID, err := tr.db.Put(object.TypeBlob, []byte("same-target"))
	if err != nil {
		t.Fatalf("Put returned error %v", err)
	}
	tr.idx.Add(index.Entry{
		Path: "link",
		Mode: object.ModeSymlink,
		ID:   oldID,
		Stat: index.Stat{MTime: time.Unix(1, 0), Size: 3},
	})
	w := tr.open()

	originalLstat, originalReadlink := fsLstatFile, fsReadlinkFile
	t.Cleanup(func() {
		fsLstatFile = originalLstat
		fsReadlinkFile = originalReadlink
	})
	fsLstatFile = func(root *os.Root, name string) (os.FileInfo, error) {
		return fakeFileInfo{mode: os.ModeSymlink, size: 999, modTime: time.Unix(2, 0)}, nil
	}
	fsReadlinkFile = func(root *os.Root, name string) (string, error) { return "same-target", nil }

	status, err := w.Status(t.Context())
	if err != nil {
		t.Fatalf("Status returned error %v", err)
	}
	entry, ok := entryMap(status.Entries)["link"]
	if !ok || entry.Unstaged != StatusUnmodified {
		t.Fatalf("link entry = %#v, want Unstaged=Unmodified", entry)
	}
}

func TestCompareToWorktreeFailsWhenReadlinkFails(t *testing.T) {
	tr := newTestRepo(t)
	oldID, err := tr.db.Put(object.TypeBlob, []byte("old-target"))
	if err != nil {
		t.Fatalf("Put returned error %v", err)
	}
	tr.idx.Add(index.Entry{
		Path: "link",
		Mode: object.ModeSymlink,
		ID:   oldID,
		Stat: index.Stat{MTime: time.Unix(1, 0), Size: 3},
	})
	w := tr.open()

	originalLstat, originalReadlink := fsLstatFile, fsReadlinkFile
	t.Cleanup(func() {
		fsLstatFile = originalLstat
		fsReadlinkFile = originalReadlink
	})
	fsLstatFile = func(root *os.Root, name string) (os.FileInfo, error) {
		return fakeFileInfo{mode: os.ModeSymlink, size: 999, modTime: time.Unix(2, 0)}, nil
	}
	fsReadlinkFile = func(root *os.Root, name string) (string, error) { return "", errors.New("boom") }

	if _, err := w.Status(t.Context()); err == nil {
		t.Fatalf("Status returned no error")
	}
}
