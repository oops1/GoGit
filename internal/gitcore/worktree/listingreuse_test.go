package worktree

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/index"
)

func countDirectoryOpens(t *testing.T) func() map[string]int {
	t.Helper()
	var mu sync.Mutex
	opened := map[string]int{}
	original := fsOpenDir
	fsOpenDir = func(root *os.Root, name string) (*os.File, error) {
		mu.Lock()
		opened[filepath.ToSlash(name)]++
		mu.Unlock()
		return original(root, name)
	}
	t.Cleanup(func() { fsOpenDir = original })
	return func() map[string]int {
		mu.Lock()
		defer mu.Unlock()
		return opened
	}
}

func TestStatusReadsEveryTrackedDirectoryOnlyOnce(t *testing.T) {
	tr := newTestRepo(t)
	tr.stage("a.txt", "a\n")
	tr.stage("dir/b.txt", "b\n")
	tr.stage("dir/sub/c.txt", "c\n")
	for i := range batchedStatMinimum {
		tr.stage(fmt.Sprintf("dir/sub/many%02d.txt", i), "many\n")
	}
	tr.commit("tracked")
	tr.writeFile("dir/new.txt", "new\n")
	tr.writeFile("dir/sub/c.txt", "changed\n")
	w := tr.open()
	opens := countDirectoryOpens(t)

	status, err := w.Status(t.Context())

	if err != nil {
		t.Fatalf("Status returned error %v", err)
	}
	for _, dir := range []string{".", "dir", "dir/sub"} {
		if got := opens()[dir]; got != 1 {
			t.Errorf("directory %q was read %d times, want once: %v", dir, got, opens())
		}
	}
	entries := entryMap(status.Entries)
	if entries["dir/new.txt"].Unstaged != StatusUntracked || entries["dir/sub/c.txt"].Unstaged != StatusModified {
		t.Fatalf("status = %+v, want dir/new.txt untracked and dir/sub/c.txt modified", status.Entries)
	}
}

func TestScanningWithoutUntrackedFilesKeepsNoListingsAndStatsSmallDirectoriesOneByOne(t *testing.T) {
	tr := newTestRepo(t)
	tr.stage("dir/b.txt", "b\n")
	tr.commit("tracked")
	w := tr.open()
	w.hideUntracked = true
	var entries []*index.Entry
	for entry := range w.currentIndex().Entries() {
		entries = append(entries, entry)
	}
	opens := countDirectoryOpens(t)

	scan, err := w.unstagedStatuses(t.Context(), entries, gitlinkRules{})

	if err != nil {
		t.Fatalf("unstagedStatuses returned error %v", err)
	}
	if scan.listings != nil {
		t.Fatalf("listings = %v, want none when untracked files are hidden", scan.listings)
	}
	if got := opens()["dir"]; got != 0 {
		t.Fatalf("a directory with one tracked file was listed %d times, want lstat only", got)
	}
}
