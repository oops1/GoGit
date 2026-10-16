package worktree

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"sync/atomic"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/index"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/odb"
)

func headTreeOf(t *testing.T, w *Worktree, tr *testRepo) hash.ObjectID {
	t.Helper()
	commit, err := w.db.Commit(tr.headCommit())
	if err != nil {
		t.Fatalf("Commit returned error %v", err)
	}
	return commit.Tree
}

func newComparisonRepo(t *testing.T) *testRepo {
	t.Helper()
	tr := newTestRepo(t)
	for _, rel := range []string{
		"README.md", "main.go", "go.mod", "a.txt", "a.txt.bak",
		"src/one.go", "src/two.go", "src/deep/three.go", "src/deep/four.go", "src/deep/er/five.go",
		"docs/guide.md", "docs/api/index.md", "docs/api/types.md",
		"lib/x.c", "lib/y.c", "lib.d/z.c", "z/last.txt",
	} {
		tr.stage(rel, "content of "+rel+"\n")
	}
	tr.commit("initial")
	return tr
}

type sequence struct{ state uint64 }

func (s *sequence) Int() int {
	s.state ^= s.state << 13
	s.state ^= s.state >> 7
	s.state ^= s.state << 17
	return int(s.state >> 1)
}

func (s *sequence) IntN(limit int) int { return s.Int() % limit }

func randomMutation(rng *sequence, idx *index.Index, step int) {
	var merged []*index.Entry
	for entry := range idx.Entries() {
		if entry.Stage == index.StageMerged {
			merged = append(merged, entry)
		}
	}
	if len(merged) == 0 {
		return
	}
	pick := *merged[rng.IntN(len(merged))]
	freshID := func() hash.ObjectID {
		id, _ := hash.Sum(hash.SHA1, "blob", fmt.Appendf(nil, "mutation %d %d", step, rng.Int()))
		return id
	}
	switch rng.IntN(10) {
	case 0:
		pick.ID = freshID()
		idx.Add(pick)
	case 1:
		idx.Add(index.Entry{Path: fmt.Sprintf("new%d/file%d.txt", rng.IntN(3), step), Mode: object.ModeBlob, ID: freshID()})
	case 2:
		idx.Remove(pick.Path)
	case 3:
		idx.Remove(pick.Path)
		idx.Add(index.Entry{Path: fmt.Sprintf("moved/%d/%s", step, pick.Path), Mode: pick.Mode, ID: pick.ID})
	case 4:
		if pick.Mode == object.ModeExecutable {
			pick.Mode = object.ModeBlob
		} else {
			pick.Mode = object.ModeExecutable
		}
		idx.Add(pick)
	case 5:
		pick.Mode = object.ModeSymlink
		idx.Add(pick)
	case 6:
		idx.Remove(pick.Path)
		idx.Add(index.Entry{Path: pick.Path + "/inner.txt", Mode: object.ModeBlob, ID: freshID()})
	case 7:
		dir := pick.Path
		for at := len(dir) - 1; at >= 0; at-- {
			if dir[at] == '/' {
				dir = dir[:at]
				break
			}
		}
		if dir == pick.Path {
			return
		}
		for _, entry := range merged {
			if len(entry.Path) > len(dir) && entry.Path[:len(dir)+1] == dir+"/" {
				idx.Remove(entry.Path)
			}
		}
		idx.Add(index.Entry{Path: dir, Mode: object.ModeBlob, ID: freshID()})
	case 8:
		idx.Remove(pick.Path)
		for _, stage := range []index.Stage{index.StageAncestor, index.StageOurs, index.StageTheirs} {
			idx.Add(index.Entry{Path: pick.Path, Mode: object.ModeBlob, ID: freshID(), Stage: stage})
		}
	default:
		idx.Add(index.Entry{Path: pick.Path + ".copy", Mode: pick.Mode, ID: pick.ID})
	}
}

func TestStagedStatusAgreesWithTheFullComparisonOnRandomChanges(t *testing.T) {
	for seed := range uint64(120) {
		t.Run(fmt.Sprintf("seed%03d", seed), func(t *testing.T) {
			rng := &sequence{state: seed*7919 + 1}
			tr := newComparisonRepo(t)
			for step := range 1 + rng.IntN(6) {
				randomMutation(rng, tr.idx, step)
			}
			if seed%3 == 0 {
				tr.idx.CacheTree = nil
			}
			w := tr.open()
			treeID := headTreeOf(t, w, tr)

			got, err := w.stagedStatus(t.Context(), treeID)
			if err != nil {
				t.Fatalf("stagedStatus returned error %v", err)
			}
			head, err := w.referenceHeadTree(t.Context(), treeID)
			if err != nil {
				t.Fatalf("referenceHeadTree returned error %v", err)
			}
			want := w.referenceStagedStatus(head)

			if !reflect.DeepEqual(got, want) {
				t.Fatalf("stagedStatus = %v\nthe full comparison gives %v", sortedStaged(got), sortedStaged(want))
			}
		})
	}
}

func sortedStaged(entries map[string]Entry) []string {
	lines := make([]string, 0, len(entries))
	for path, entry := range entries {
		lines = append(lines, fmt.Sprintf("%s staged=%c from=%q", path, entry.Staged, entry.OrigPath))
	}
	slices.Sort(lines)
	return lines
}

func TestAnIndexIdenticalToHeadReadsOnlyTheRootTree(t *testing.T) {
	tr := newComparisonRepo(t)
	w := tr.open()
	treeID := headTreeOf(t, w, tr)
	if cache := w.currentIndex().CacheTree; cache == nil || !cache.Valid() || cache.ID != treeID {
		t.Fatalf("the saved index has no valid cache tree for %s", treeID)
	}
	reads := countTreeReads(t)

	staged, err := w.stagedStatus(t.Context(), treeID)

	if err != nil || len(staged) != 0 {
		t.Fatalf("stagedStatus = %v, %v, want nothing staged", staged, err)
	}
	if got := reads.Load(); got != 0 {
		t.Fatalf("stagedStatus read %d trees of an unchanged HEAD, want none", got)
	}
}

func TestOneChangedFileReadsOnlyTheTreesOnItsPath(t *testing.T) {
	tr := newComparisonRepo(t)
	tr.stage("src/deep/er/five.go", "changed\n")
	w := tr.open()
	treeID := headTreeOf(t, w, tr)
	reads := countTreeReads(t)

	staged, err := w.stagedStatus(t.Context(), treeID)

	if err != nil || len(staged) != 1 || staged["src/deep/er/five.go"].Staged != StatusModified {
		t.Fatalf("stagedStatus = %v, %v, want the one modified file", staged, err)
	}
	if got := reads.Load(); got != 4 {
		t.Fatalf("stagedStatus read %d trees, want the four on the path root, src, deep and er", got)
	}
}

func TestStagedStatusReportsAnUnreadableTreeBelowTheRoot(t *testing.T) {
	tr := newComparisonRepo(t)
	tr.stage("docs/api/types.md", "changed\n")
	tr.idx.CacheTree = nil
	w := tr.open()
	treeID := headTreeOf(t, w, tr)
	root, err := w.db.Tree(treeID)
	if err != nil {
		t.Fatalf("Tree returned error %v", err)
	}
	brokenID := hash.Zero
	for _, entry := range root.Entries {
		if entry.Name == "lib" {
			brokenID = entry.ID
		}
	}
	failing := failTreeReads(t, brokenID)

	if _, err := w.stagedStatus(t.Context(), treeID); err == nil || failing.Load() == 0 {
		t.Fatalf("stagedStatus returned %v after %d failing reads, want an error", err, failing.Load())
	}
}

func countTreeReads(t *testing.T) *atomic.Int64 {
	t.Helper()
	reads := new(atomic.Int64)
	previous := loadTree
	loadTree = func(db *odb.DB, id hash.ObjectID) (*object.Tree, error) {
		reads.Add(1)
		return previous(db, id)
	}
	t.Cleanup(func() { loadTree = previous })
	return reads
}

func failTreeReads(t *testing.T, broken hash.ObjectID) *atomic.Int64 {
	t.Helper()
	failures := new(atomic.Int64)
	previous := loadTree
	loadTree = func(db *odb.DB, id hash.ObjectID) (*object.Tree, error) {
		if id == broken {
			failures.Add(1)
			return nil, errors.New("tree unreadable")
		}
		return previous(db, id)
	}
	t.Cleanup(func() { loadTree = previous })
	return failures
}

func TestStagedStatusReportsAnUnreadableTreeOfADirectoryTheIndexLacks(t *testing.T) {
	tr := newComparisonRepo(t)
	tr.idx.Remove("lib/x.c")
	tr.idx.Remove("lib/y.c")
	w := tr.open()
	treeID := headTreeOf(t, w, tr)
	root, err := w.db.Tree(treeID)
	if err != nil {
		t.Fatalf("Tree returned error %v", err)
	}
	var lib hash.ObjectID
	for _, entry := range root.Entries {
		if entry.Name == "lib" {
			lib = entry.ID
		}
	}
	failing := failTreeReads(t, lib)

	if _, err := w.stagedStatus(t.Context(), treeID); !errors.Is(err, ErrReadHeadTree) || failing.Load() == 0 {
		t.Fatalf("stagedStatus returned %v after %d failing reads, want ErrReadHeadTree", err, failing.Load())
	}
}

func TestStagedStatusReportsEverythingUnderADirectoryRemovedFromTheIndex(t *testing.T) {
	tr := newComparisonRepo(t)
	for _, rel := range []string{"src/one.go", "src/two.go", "src/deep/three.go", "src/deep/four.go", "src/deep/er/five.go"} {
		tr.idx.Remove(rel)
	}
	w := tr.open()

	staged, err := w.stagedStatus(t.Context(), headTreeOf(t, w, tr))

	if err != nil || len(staged) != 5 {
		t.Fatalf("stagedStatus = %v, %v, want five deletions", sortedStaged(staged), err)
	}
	for path, entry := range staged {
		if entry.Staged != StatusDeleted {
			t.Fatalf("%s is %c, want deleted", path, entry.Staged)
		}
	}
}

func TestRemovingATreeStopsWhenTheCallerGivesUp(t *testing.T) {
	tr := newComparisonRepo(t)
	w := tr.open()

	err := w.removeTree(newStepContext(0), &headDifference{changed: map[string]Entry{}}, headTreeOf(t, w, tr), "")

	if err == nil {
		t.Fatal("removeTree ignored a cancelled context")
	}
}

func TestTheHeadGitmodulesBlobIsFoundOnlyAsAFileOfTheRootTree(t *testing.T) {
	tr := newTestRepo(t)
	tr.stage(".gitmodules", "[submodule \"x\"]\n\tpath = x\n\turl = ../x\n")
	tr.stage("dir/.gitmodules", "nested\n")
	tr.commit("with modules")
	w := tr.open()
	treeID := headTreeOf(t, w, tr)
	want, err := hash.Sum(hash.SHA1, "blob", []byte("[submodule \"x\"]\n\tpath = x\n\turl = ../x\n"))
	if err != nil {
		t.Fatal(err)
	}

	got, err := w.headGitmodules(treeID)
	if err != nil || got != want {
		t.Fatalf("headGitmodules = %s, %v, want %s", got, err, want)
	}
	if got, err := w.headGitmodules(hash.Zero); err != nil || !got.IsZero() {
		t.Fatalf("headGitmodules of an unborn HEAD = %s, %v, want nothing", got, err)
	}
}

func TestTheHeadGitmodulesBlobIsAbsentWhenTheNameIsADirectoryOrMissing(t *testing.T) {
	tr := newTestRepo(t)
	tr.stage(".gitmodules/inner.txt", "x\n")
	tr.commit("a directory with that name")
	w := tr.open()

	if got, err := w.headGitmodules(headTreeOf(t, w, tr)); err != nil || !got.IsZero() {
		t.Fatalf("headGitmodules = %s, %v, want nothing for a directory", got, err)
	}
	other := newTestRepo(t)
	other.stage("plain.txt", "x\n")
	other.commit("no modules")
	ow := other.open()
	if got, err := ow.headGitmodules(headTreeOf(t, ow, other)); err != nil || !got.IsZero() {
		t.Fatalf("headGitmodules = %s, %v, want nothing without the file", got, err)
	}
}

func TestTheHeadGitmodulesBlobReportsAnUnreadableRootTree(t *testing.T) {
	tr := newComparisonRepo(t)
	w := tr.open()
	treeID := headTreeOf(t, w, tr)
	failTreeReads(t, treeID)

	if _, err := w.headGitmodules(treeID); !errors.Is(err, ErrReadHeadTree) {
		t.Fatalf("headGitmodules returned %v, want ErrReadHeadTree", err)
	}
}

func TestSubmoduleRulesReportAnUnreadableHeadTree(t *testing.T) {
	tr := newComparisonRepo(t)
	tr.idx.Add(index.Entry{Path: "vendor/sub", Mode: object.ModeSubmodule, ID: hash.ObjectID{7}})
	w := tr.open()
	treeID := headTreeOf(t, w, tr)
	failTreeReads(t, treeID)
	var entries []*index.Entry
	for entry := range w.currentIndex().Entries() {
		entries = append(entries, entry)
	}

	if _, err := w.gitlinkRulesFor(entries, treeID); !errors.Is(err, ErrReadHeadTree) {
		t.Fatalf("gitlinkRulesFor returned %v, want ErrReadHeadTree", err)
	}
}

func TestStagedStatusReportsAnUnreadableNestedTreeOfARemovedDirectory(t *testing.T) {
	tr := newComparisonRepo(t)
	tr.idx.Remove("docs/guide.md")
	tr.idx.Remove("docs/api/index.md")
	tr.idx.Remove("docs/api/types.md")
	w := tr.open()
	treeID := headTreeOf(t, w, tr)
	root, err := w.db.Tree(treeID)
	if err != nil {
		t.Fatalf("Tree returned error %v", err)
	}
	var docs, api hash.ObjectID
	for _, entry := range root.Entries {
		if entry.Name == "docs" {
			docs = entry.ID
		}
	}
	docsTree, err := w.db.Tree(docs)
	if err != nil {
		t.Fatalf("Tree returned error %v", err)
	}
	for _, entry := range docsTree.Entries {
		if entry.Name == "api" {
			api = entry.ID
		}
	}
	failing := failTreeReads(t, api)

	if _, err := w.stagedStatus(t.Context(), treeID); !errors.Is(err, ErrReadHeadTree) || failing.Load() == 0 {
		t.Fatalf("stagedStatus returned %v after %d failing reads, want ErrReadHeadTree", err, failing.Load())
	}
}
