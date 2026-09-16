//go:build !race

package ops

import (
	"errors"
	"io/fs"
	"os"
	"runtime"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/object"
)

type symlinkInfo struct{ fs.FileInfo }

func (symlinkInfo) Mode() fs.FileMode { return os.ModeSymlink }

func pretendSymlink(t *testing.T, name string) {
	t.Helper()
	swapSeam(t, &fsRootLstat, func(original func(*os.Root, string) (fs.FileInfo, error)) func(*os.Root, string) (fs.FileInfo, error) {
		return func(root *os.Root, path string) (fs.FileInfo, error) {
			info, err := original(root, path)
			if err == nil && path == name {
				return symlinkInfo{info}, nil
			}
			return info, err
		}
	})
}

func TestDiscardLeavesNestedRepositoriesAndConflictedFilesAlone(t *testing.T) {
	tr := newTestRepo(t)
	tr.conflictingFork()
	if _, err := tr.merge("feature", MergeOptions{}); err != nil {
		t.Fatalf("merge returned error %v", err)
	}
	tr.writeFile("libs/nested/.git/HEAD", "ref: refs/heads/main\n")
	tr.writeFile("libs/nested/code.txt", "precious\n")
	tr.writeFile("libs/junk.txt", "junk\n")

	if err := Discard(t.Context(), tr.repo, []string{"libs", "f"}, DiscardOptions{RemoveUntracked: true}); err != nil {
		t.Fatalf("Discard returned error %v", err)
	}

	if !tr.exists("libs/nested/code.txt") || !tr.exists("libs/nested/.git/HEAD") {
		t.Fatal("Discard removed a nested repository")
	}
	if tr.exists("libs/junk.txt") {
		t.Fatal("Discard kept an untracked file")
	}
	if !tr.exists("f") {
		t.Fatal("Discard removed a file with an unresolved conflict")
	}
}

func TestSwitchNeedsNoIdentity(t *testing.T) {
	tr := newTestRepoNoIdentity(t)
	base := tr.commitFiles("base", map[string]string{"a": "a\n"})
	tr.createBranch("feature", base)

	if err := Switch(t.Context(), tr.repo, "feature", SwitchOptions{}); err != nil {
		t.Fatalf("Switch returned error %v", err)
	}
}

func TestSwitchReplacesASymlinkInsteadOfWritingThroughIt(t *testing.T) {
	tr := newTestRepo(t)
	tr.writeFile("other.txt", "other\n")
	if !tr.symlink("other.txt", "link") {
		t.Skip("symlinks are not available")
	}
	if err := Stage(t.Context(), tr.repo, []string{"other.txt", "link"}, StageOptions{}); err != nil {
		t.Fatalf("Stage returned error %v", err)
	}
	base := tr.commitAll("base")
	tr.createBranch("feature", base)
	tr.switchTo("feature")
	tr.remove("link")
	tr.commitFiles("regular", map[string]string{"link": "regular\n"})
	tr.switchTo("main")

	tr.switchTo("feature")

	if tr.readFile("other.txt") != "other\n" {
		t.Fatal("the switch wrote through the symlink")
	}
	info, err := os.Lstat(tr.path("link"))
	if err != nil || info.Mode()&os.ModeSymlink != 0 || tr.readFile("link") != "regular\n" {
		t.Fatalf("link = %v, %v", info, err)
	}
}

func openTestWorkingTree(t *testing.T, tr *testRepo) *workingTree {
	t.Helper()
	wt, err := openWorkingTree(tr.repo)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = wt.close() })
	return wt
}

func TestWriteWorktreeBlobFollowsTheExecutableBit(t *testing.T) {
	tr := newTestRepo(t)
	wt := openTestWorkingTree(t, tr)
	wt.fileMode = true

	for range 2 {
		if err := writeWorktreeBlob(wt, "run.sh", object.ModeExecutable, []byte("echo\n")); err != nil {
			t.Fatalf("writeWorktreeBlob returned error %v", err)
		}
	}
	if runtime.GOOS != "windows" {
		if info, err := os.Stat(tr.path("run.sh")); err != nil || info.Mode().Perm()&0o100 == 0 {
			t.Fatalf("run.sh = %v, %v; want it executable", info, err)
		}
	}
	if err := writeWorktreeBlob(wt, "run.sh", object.ModeBlob, []byte("echo\n")); err != nil {
		t.Fatalf("writeWorktreeBlob returned error %v", err)
	}
	if runtime.GOOS != "windows" {
		if info, err := os.Stat(tr.path("run.sh")); err != nil || info.Mode().Perm()&0o111 != 0 {
			t.Fatalf("run.sh = %v, %v; want it plain", info, err)
		}
	}
}

func TestWriteWorktreeBlobReportsAFailingChmodOrStat(t *testing.T) {
	tr := newTestRepo(t)
	wt := openTestWorkingTree(t, tr)
	wt.fileMode = true
	if err := writeWorktreeBlob(wt, "run.sh", object.ModeBlob, []byte("echo\n")); err != nil {
		t.Fatalf("writeWorktreeBlob returned error %v", err)
	}
	swapSeam(t, &fsRootChmod, func(func(*os.Root, string, fs.FileMode) error) func(*os.Root, string, fs.FileMode) error {
		return func(*os.Root, string, fs.FileMode) error { return errInjected }
	})
	if err := writeWorktreeBlob(wt, "run.sh", object.ModeExecutable, []byte("echo\n")); !errors.Is(err, errInjected) {
		t.Fatalf("a failing chmod returned %v", err)
	}
	swapSeam(t, &fsRootLstat, func(func(*os.Root, string) (fs.FileInfo, error)) func(*os.Root, string) (fs.FileInfo, error) {
		return func(*os.Root, string) (fs.FileInfo, error) { return nil, errInjected }
	})
	if err := writeWorktreeBlob(wt, "run.sh", object.ModeBlob, []byte("echo\n")); !errors.Is(err, errInjected) {
		t.Fatalf("a failing stat returned %v", err)
	}
}

func TestWriteWorktreeBlobRemovesASymlinkBeforeWriting(t *testing.T) {
	tr := newTestRepo(t)
	tr.writeFile("link", "old\n")
	wt := openTestWorkingTree(t, tr)
	pretendSymlink(t, "link")

	if err := writeWorktreeBlob(wt, "link", object.ModeBlob, []byte("new\n")); err != nil {
		t.Fatalf("writeWorktreeBlob returned error %v", err)
	}
	if tr.readFile("link") != "new\n" {
		t.Fatalf("link = %q", tr.readFile("link"))
	}

	swapSeam(t, &fsRootRemove, func(func(*os.Root, string) error) func(*os.Root, string) error {
		return func(*os.Root, string) error { return errInjected }
	})
	if err := writeWorktreeBlob(wt, "link", object.ModeBlob, []byte("x\n")); !errors.Is(err, errInjected) {
		t.Fatalf("writeWorktreeBlob returned %v", err)
	}
	if tr.readFile("link") != "new\n" {
		t.Fatal("a symlink that could not be removed was written through")
	}
}
