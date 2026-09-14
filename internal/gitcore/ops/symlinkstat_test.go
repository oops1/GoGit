package ops

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/oops1/gogit/internal/gitcore/object"
)

type sizedSymlinkInfo struct {
	name string
	size int64
	when time.Time
}

func (f sizedSymlinkInfo) Name() string       { return f.name }
func (f sizedSymlinkInfo) Size() int64        { return f.size }
func (f sizedSymlinkInfo) Mode() fs.FileMode  { return fs.ModeSymlink | 0o777 }
func (f sizedSymlinkInfo) ModTime() time.Time { return f.when }
func (f sizedSymlinkInfo) IsDir() bool        { return false }
func (f sizedSymlinkInfo) Sys() any           { return nil }

func newSymlinksEnabledRepo(t *testing.T) *testRepo {
	t.Helper()
	r := newTestRepo(t)
	r.appendConfig("[core]\n\tsymlinks = true\n")
	r.repo = r.reopen()
	return r
}

func (r *testRepo) symlinkBranch(target string) {
	r.t.Helper()
	t := r.t.(*testing.T)
	main := r.initialCommit()
	tree := putTree(t, r,
		object.TreeEntry{Mode: object.ModeBlob, Name: "a.txt", ID: storedBlob(t, r, "hello\n")},
		object.TreeEntry{Mode: object.ModeSymlink, Name: "link", ID: storedBlob(t, r, target)},
	)
	r.createBranch("feature", putCommit(t, r, tree, main))
}

func symlinksSupported(t *testing.T) bool {
	t.Helper()
	dir := t.TempDir()
	return os.Symlink("missing", filepath.Join(dir, "probe")) == nil
}

func TestCheckoutRecordsTheSymlinkItselfRatherThanItsTarget(t *testing.T) {
	r := newSymlinksEnabledRepo(t)
	target := "nowhere/at/all.txt"
	r.symlinkBranch(target)
	when := time.Unix(1700000123, 456)
	originalSymlink := fsRootSymlink
	created := false
	fsRootSymlink = func(root *os.Root, got, name string) error {
		created = got == target && filepath.ToSlash(name) == "link"
		return nil
	}
	t.Cleanup(func() { fsRootSymlink = originalSymlink })
	originalLstat := fsRootLstat
	fsRootLstat = func(root *os.Root, name string) (fs.FileInfo, error) {
		if created && filepath.ToSlash(name) == "link" {
			return sizedSymlinkInfo{name: "link", size: int64(len(target)), when: when}, nil
		}
		return originalLstat(root, name)
	}
	t.Cleanup(func() { fsRootLstat = originalLstat })

	if err := Switch(t.Context(), r.repo, "feature", SwitchOptions{}); err != nil {
		t.Fatalf("Switch returned error %v", err)
	}
	entry, ok := entryOf(t, r.index(), "link")
	if !ok || entry.Mode != object.ModeSymlink {
		t.Fatalf("link = %+v, %v", entry, ok)
	}
	if entry.Stat.Size != uint32(len(target)) || !entry.Stat.MTime.Equal(when) {
		t.Fatalf("stat = %+v, want the size %d and time of the link itself", entry.Stat, len(target))
	}
}

func TestCheckoutWritesADanglingSymlinkAndRecordsItsLstat(t *testing.T) {
	if !symlinksSupported(t) {
		t.Skip("symbolic links are not supported")
	}
	r := newSymlinksEnabledRepo(t)
	target := "missing-target.txt"
	r.symlinkBranch(target)

	if err := Switch(t.Context(), r.repo, "feature", SwitchOptions{}); err != nil {
		t.Fatalf("Switch returned error %v", err)
	}
	info, err := os.Lstat(r.path("link"))
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("link = %v, %v, want a symbolic link", info, err)
	}
	if got, err := os.Readlink(r.path("link")); err != nil || got != target {
		t.Fatalf("link points to %q, %v", got, err)
	}
	entry, ok := entryOf(t, r.index(), "link")
	if !ok || entry.Mode != object.ModeSymlink {
		t.Fatalf("link = %+v, %v", entry, ok)
	}
	if int64(entry.Stat.Size) != info.Size() || !entry.Matches(info, false, true) {
		t.Fatalf("stat = %+v does not describe the link of %d bytes changed at %v", entry.Stat, info.Size(), info.ModTime())
	}
}
