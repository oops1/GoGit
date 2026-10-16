package worktree

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/index"
)

const batchedDirectory = "batched"

func newBatchedRepo(t *testing.T, files int) *testRepo {
	t.Helper()
	tr := newTestRepo(t)
	for i := range files {
		tr.stage(fmt.Sprintf("%s/file%02d.txt", batchedDirectory, i), fmt.Sprintf("content %d\n", i))
	}
	tr.stage("small/a.txt", "a\n")
	tr.stage("small/b.txt", "b\n")
	tr.commit("initial")
	return tr
}

func countLstatCalls(t *testing.T, prefix string) *atomic.Int64 {
	t.Helper()
	calls := new(atomic.Int64)
	previous := fsLstatFile
	fsLstatFile = func(root *os.Root, name string) (fs.FileInfo, error) {
		if strings.HasPrefix(filepath.ToSlash(name), prefix) {
			calls.Add(1)
		}
		return previous(root, name)
	}
	t.Cleanup(func() { fsLstatFile = previous })
	return calls
}

func TestStatusReadsABigDirectoryOnceInsteadOfStatingEachFileInIt(t *testing.T) {
	tr := newBatchedRepo(t, batchedStatMinimum+4)
	w := tr.openWith(Options{IncludeUnmodified: true})
	inBatched := countLstatCalls(t, batchedDirectory+"/")

	status, err := w.Status(t.Context())

	if err != nil {
		t.Fatalf("Status returned error %v", err)
	}
	if calls := inBatched.Load(); calls != 0 {
		t.Fatalf("Status stated %d files of a directory it had already listed, want none", calls)
	}
	var seen int
	for _, entry := range status.Entries {
		if strings.HasPrefix(entry.Path, batchedDirectory+"/") {
			seen++
			if entry.Size == 0 || entry.ModTime.IsZero() {
				t.Fatalf("%s has no size or time although the scan had them", entry.Path)
			}
		}
	}
	if seen != batchedStatMinimum+4 {
		t.Fatalf("Status listed %d files of the directory, want %d", seen, batchedStatMinimum+4)
	}
}

func TestStatusStatesFilesOfSmallDirectoriesOneByOne(t *testing.T) {
	tr := newBatchedRepo(t, batchedStatMinimum)
	w := tr.open()
	inSmall := countLstatCalls(t, "small/")

	if _, err := w.Status(t.Context()); err != nil {
		t.Fatalf("Status returned error %v", err)
	}

	if calls := inSmall.Load(); calls != 2 {
		t.Fatalf("Status stated %d files of a two-file directory, want one each", calls)
	}
}

func TestStatusReportsAFileDeletedFromABatchedDirectory(t *testing.T) {
	tr := newBatchedRepo(t, batchedStatMinimum+2)
	tr.remove(batchedDirectory + "/file03.txt")
	tr.writeFile(batchedDirectory+"/file04.txt", "changed in the working tree\n")
	w := tr.open()

	status, err := w.Status(t.Context())

	if err != nil {
		t.Fatalf("Status returned error %v", err)
	}
	got := map[string]StatusCode{}
	for _, entry := range status.Entries {
		got[entry.Path] = entry.Unstaged
	}
	if got[batchedDirectory+"/file03.txt"] != StatusDeleted || got[batchedDirectory+"/file04.txt"] != StatusModified || len(got) != 2 {
		t.Fatalf("Status reported %v, want one deleted and one modified file", got)
	}
}

func TestStatusStatesAFileTheDirectoryListingLacks(t *testing.T) {
	tr := newBatchedRepo(t, 1)
	w := tr.open()
	entry, ok := w.currentIndex().Get(batchedDirectory+"/file00.txt", index.StageMerged)
	if !ok {
		t.Fatal("the file is not in the index")
	}

	code, info, err := w.compareToWorktree(entry, directoryListing{})

	if err != nil || code != StatusUnmodified || info == nil {
		t.Fatalf("compareToWorktree = %v, %v, %v, want the file found by stating it", code, info, err)
	}
}

func TestStatusFallsBackToStatingWhenADirectoryCannotBeListed(t *testing.T) {
	tr := newBatchedRepo(t, batchedStatMinimum)
	w := tr.open()
	previous := fsOpenDir
	refused := false
	fsOpenDir = func(root *os.Root, name string) (*os.File, error) {
		if name == batchedDirectory && !refused {
			refused = true
			return nil, errors.New("listing refused")
		}
		return previous(root, name)
	}
	t.Cleanup(func() { fsOpenDir = previous })
	statted := countLstatCalls(t, batchedDirectory+"/")

	status, err := w.Status(t.Context())

	if err != nil || len(status.Entries) != 0 {
		t.Fatalf("Status = %v, %v, want a clean tree", status.Entries, err)
	}
	if calls := statted.Load(); calls != batchedStatMinimum {
		t.Fatalf("Status stated %d files, want %d one by one", calls, batchedStatMinimum)
	}
}

type brokenDirEntry struct{ name string }

func (e brokenDirEntry) Name() string               { return e.name }
func (e brokenDirEntry) IsDir() bool                { return false }
func (e brokenDirEntry) Type() fs.FileMode          { return 0 }
func (e brokenDirEntry) Info() (fs.FileInfo, error) { return nil, errors.New("gone") }

func TestDirectoryListingIgnoresAnEntryItCannotDescribe(t *testing.T) {
	listing := directoryListing{"x": brokenDirEntry{name: "x"}}

	if info, ok := listing.info("x"); ok || info != nil {
		t.Fatalf("info = %v, %v, want nothing for an entry whose Info fails", info, ok)
	}
	if _, ok := listing.info("absent"); ok {
		t.Fatal("info found a name that is not listed")
	}
	if _, ok := directoryListing(nil).info("x"); ok {
		t.Fatal("info found a name in a missing listing")
	}
}

func TestGroupingByDirectoryKeepsEachDirectoryTogether(t *testing.T) {
	entries := []*index.Entry{
		{Path: "a.txt"}, {Path: "dir/one.txt"}, {Path: "dir/two.txt"}, {Path: "dir/sub/three.txt"}, {Path: "b.txt"},
	}

	groups := groupByDirectory(entries)

	sizes := map[int]int{}
	for _, group := range groups {
		sizes[len(group)]++
		dir := ""
		for _, at := range group {
			current := entries[at].Path[:strings.LastIndex(entries[at].Path, "/")+1]
			if dir != "" && current != dir {
				t.Fatalf("a group mixes %q and %q", dir, current)
			}
			dir = current
		}
	}
	if len(groups) != 3 || sizes[2] != 2 || sizes[1] != 1 {
		t.Fatalf("groups = %v, want the root pair, the dir pair and the nested single", groups)
	}
}

func TestStatusStopsWhenTheCallerGivesUpInTheMiddleOfAGroup(t *testing.T) {
	tr := newBatchedRepo(t, batchedStatMinimum)
	w := tr.open()
	entries := make([]*index.Entry, 0, batchedStatMinimum)
	for entry := range w.currentIndex().Entries() {
		if strings.HasPrefix(entry.Path, batchedDirectory+"/") {
			entries = append(entries, entry)
		}
	}
	group := make([]int, len(entries))
	for at := range group {
		group[at] = at
	}

	err := w.scanGroup(newStepContext(0), entries, group, gitlinkRules{}, make([]unstagedChange, len(entries)))

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("scanGroup returned %v, want the cancellation", err)
	}
}

func TestStatusReportsACancelledContextAfterTheScan(t *testing.T) {
	w := newBatchedRepo(t, 3).open()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, err := w.unstagedStatuses(ctx, nil, gitlinkRules{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("unstagedStatuses returned %v, want the cancellation", err)
	}
}
