package ops

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/diff"
	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/index"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/patch"
)

func (r *testRepo) writeWorking(rel, text string) {
	r.t.Helper()
	if err := os.WriteFile(r.path(rel), []byte(text), 0o666); err != nil {
		r.t.Fatal(err)
	}
}

func (r *testRepo) stagedText(rel string) string {
	r.t.Helper()
	entry, found := r.index().Get(rel, index.StageMerged)
	if !found {
		r.t.Fatalf("%s is not in the index", rel)
	}
	_, data, err := r.db().Get(entry.ID)
	if err != nil {
		r.t.Fatal(err)
	}
	return string(data)
}

func pickText(hunks []diff.Hunk, kind diff.Kind, text string) patch.Picked {
	return func(hunk, line int) bool {
		l := hunks[hunk].Lines[line]
		return l.Kind == kind && l.Text == text
	}
}

func stagingPatch(t *testing.T, base, working string, kind diff.Kind, text string) []diff.Hunk {
	t.Helper()
	hunks := diff.Blobs([]byte(base), []byte(working), diff.Defaults())
	selected, err := patch.Select(hunks, pickText(hunks, kind, text))
	if err != nil {
		t.Fatal(err)
	}
	return selected
}

func TestPatchingTheIndexStagesOnlyThePickedLines(t *testing.T) {
	tr := newTestRepo(t)
	tr.commitFiles("base", map[string]string{"f": "a\nb\nc\n"})
	tr.writeWorking("f", "a\nB\nc\nd\n")

	err := PatchIndex(t.Context(), tr.repo, "f", stagingPatch(t, "a\nb\nc\n", "a\nB\nc\nd\n", diff.KindAdd, "d"))

	if err != nil {
		t.Fatalf("PatchIndex returned error %v", err)
	}
	if got := tr.stagedText("f"); got != "a\nb\nc\nd\n" {
		t.Fatalf("staged = %q", got)
	}
	if got := tr.readFile("f"); got != "a\nB\nc\nd\n" {
		t.Fatalf("the working copy changed: %q", got)
	}
}

func TestPatchingTheIndexWithReversedLinesUnstagesThem(t *testing.T) {
	tr := newTestRepo(t)
	tr.commitFiles("base", map[string]string{"f": "a\nb\n"})
	tr.writeWorking("f", "a\nONE\nb\nTWO\n")
	if err := Stage(t.Context(), tr.repo, []string{"f"}, StageOptions{}); err != nil {
		t.Fatal(err)
	}
	hunks := diff.Blobs([]byte("a\nb\n"), []byte("a\nONE\nb\nTWO\n"), diff.Defaults())
	back, err := patch.Select(patch.Reverse(hunks), pickText(hunks, diff.KindAdd, "ONE"))
	if err != nil {
		t.Fatal(err)
	}

	if err := PatchIndex(t.Context(), tr.repo, "f", back); err != nil {
		t.Fatalf("PatchIndex returned error %v", err)
	}

	if got := tr.stagedText("f"); got != "a\nb\nTWO\n" {
		t.Fatalf("staged = %q", got)
	}
}

func TestPatchingTheIndexCanStartANewFile(t *testing.T) {
	tr := newTestRepo(t)
	tr.commitFiles("base", map[string]string{"keep": "keep\n"})
	tr.writeWorking("new", "one\ntwo\n")

	err := PatchIndex(t.Context(), tr.repo, "new", stagingPatch(t, "", "one\ntwo\n", diff.KindAdd, "one"))

	if err != nil {
		t.Fatalf("PatchIndex returned error %v", err)
	}
	entry, _ := tr.index().Get("new", index.StageMerged)
	if got := tr.stagedText("new"); got != "one\n" || entry.Mode != object.ModeBlob {
		t.Fatalf("staged = %q, mode = %v", got, entry.Mode)
	}
}

func TestPatchingTheIndexRefusesAStaleSelection(t *testing.T) {
	tr := newTestRepo(t)
	tr.commitFiles("base", map[string]string{"f": "a\nb\n"})
	stale := stagingPatch(t, "x\ny\n", "x\nY\n", diff.KindAdd, "Y")

	err := PatchIndex(t.Context(), tr.repo, "f", stale)

	if !errors.Is(err, diff.ErrApply) {
		t.Fatalf("err = %v, want diff.ErrApply", err)
	}
	if got := tr.stagedText("f"); got != "a\nb\n" {
		t.Fatalf("a refused patch changed the index: %q", got)
	}
}

func TestPatchingTheIndexRefusesAConflictedPath(t *testing.T) {
	tr := conflictedRepo(t)
	hunks := stagingPatch(t, "a\n", "a\nb\n", diff.KindAdd, "b")

	if err := PatchIndex(t.Context(), tr.repo, "f", hunks); !errors.Is(err, ErrPartialConflict) {
		t.Fatalf("err = %v, want ErrPartialConflict", err)
	}
}

func TestPatchingWithNothingPickedChangesNothing(t *testing.T) {
	tr := newTestRepo(t)
	tr.commitFiles("base", map[string]string{"f": "a\n"})
	breakIndexLock(t, tr)

	if err := PatchIndex(t.Context(), tr.repo, "f", nil); err != nil {
		t.Fatalf("PatchIndex returned error %v", err)
	}
	if err := PatchWorkingTree(t.Context(), tr.repo, "f", nil); err != nil {
		t.Fatalf("PatchWorkingTree returned error %v", err)
	}
}

func breakIndexLock(t *testing.T, tr *testRepo) {
	t.Helper()
	if err := os.WriteFile(tr.repo.IndexFile()+".lock", nil, 0o666); err != nil {
		t.Fatal(err)
	}
}

func TestPatchingTheIndexWaitsForNoOneElse(t *testing.T) {
	tr := newTestRepo(t)
	tr.commitFiles("base", map[string]string{"f": "a\n"})
	breakIndexLock(t, tr)

	err := PatchIndex(t.Context(), tr.repo, "f", stagingPatch(t, "a\n", "a\nb\n", diff.KindAdd, "b"))

	if !errors.Is(err, ErrIndexLocked) {
		t.Fatalf("err = %v, want ErrIndexLocked", err)
	}
}

func TestPatchingHonoursACancelledContext(t *testing.T) {
	tr := newTestRepo(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	hunks := stagingPatch(t, "a\n", "a\nb\n", diff.KindAdd, "b")

	if err := PatchIndex(ctx, tr.repo, "f", hunks); !errors.Is(err, context.Canceled) {
		t.Fatalf("PatchIndex err = %v", err)
	}
	if err := PatchWorkingTree(ctx, tr.repo, "f", hunks); !errors.Is(err, context.Canceled) {
		t.Fatalf("PatchWorkingTree err = %v", err)
	}
}

func TestPatchingRefusesAPathOutsideTheRepository(t *testing.T) {
	tr := newTestRepo(t)
	hunks := stagingPatch(t, "a\n", "a\nb\n", diff.KindAdd, "b")

	if err := PatchIndex(t.Context(), tr.repo, "../outside", hunks); err == nil {
		t.Fatal("PatchIndex accepted a path above the repository")
	}
	if err := PatchWorkingTree(t.Context(), tr.repo, "../outside", hunks); err == nil {
		t.Fatal("PatchWorkingTree accepted a path above the repository")
	}
}

func TestPatchingInABareRepositoryFails(t *testing.T) {
	tr := newBareTestRepo(t)
	hunks := stagingPatch(t, "a\n", "a\nb\n", diff.KindAdd, "b")

	if err := PatchIndex(t.Context(), tr.repo, "f", hunks); !errors.Is(err, ErrBareRepository) {
		t.Fatalf("PatchIndex err = %v", err)
	}
	if err := PatchWorkingTree(t.Context(), tr.repo, "f", hunks); !errors.Is(err, ErrBareRepository) {
		t.Fatalf("PatchWorkingTree err = %v", err)
	}
}

func TestPatchingTheIndexStopsWhenTheObjectDatabaseCannotOpen(t *testing.T) {
	tr := newTestRepo(t)
	tr.commitFiles("base", map[string]string{"f": "a\n"})
	swapOdbOpenFailOnCall(t, 1)

	err := PatchIndex(t.Context(), tr.repo, "f", stagingPatch(t, "a\n", "a\nb\n", diff.KindAdd, "b"))

	if !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want the injected failure", err)
	}
}

func TestPatchingTheIndexStopsWhenTheStagedBlobCannotBeRead(t *testing.T) {
	tr := newTestRepo(t)
	tr.commitFiles("base", map[string]string{"f": "a\n"})
	original := dbGet
	dbGet = func(*odb.DB, hash.ObjectID) (object.Type, []byte, error) { return 0, nil, errInjected }
	t.Cleanup(func() { dbGet = original })

	err := PatchIndex(t.Context(), tr.repo, "f", stagingPatch(t, "a\n", "a\nb\n", diff.KindAdd, "b"))

	if !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want the injected failure", err)
	}
}

func TestPatchingTheIndexStopsWhenTheNewBlobCannotBeWritten(t *testing.T) {
	tr := newTestRepo(t)
	tr.commitFiles("base", map[string]string{"f": "a\n"})
	original := dbPut
	dbPut = func(*odb.DB, object.Type, []byte) (hash.ObjectID, error) { return hash.Zero, errInjected }
	t.Cleanup(func() { dbPut = original })

	err := PatchIndex(t.Context(), tr.repo, "f", stagingPatch(t, "a\n", "a\nb\n", diff.KindAdd, "b"))

	if !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want the injected failure", err)
	}
}

func TestPatchingTheIndexNeedsANewFileToExist(t *testing.T) {
	tr := newTestRepo(t)
	tr.commitFiles("base", map[string]string{"keep": "keep\n"})

	err := PatchIndex(t.Context(), tr.repo, "ghost", stagingPatch(t, "", "one\n", diff.KindAdd, "one"))

	if err == nil {
		t.Fatal("a file that is neither tracked nor on disk was staged")
	}
}

func TestPatchingTheWorkingTreeDiscardsOnlyThePickedLines(t *testing.T) {
	tr := newTestRepo(t)
	tr.commitFiles("base", map[string]string{"f": "a\nb\nc\n"})
	tr.writeWorking("f", "a\nADDED\nb\nc\nMORE\n")
	hunks := diff.Blobs([]byte("a\nb\nc\n"), []byte("a\nADDED\nb\nc\nMORE\n"), diff.Defaults())
	discard, err := patch.Select(patch.Reverse(hunks), pickText(hunks, diff.KindAdd, "ADDED"))
	if err != nil {
		t.Fatal(err)
	}

	if err := PatchWorkingTree(t.Context(), tr.repo, "f", discard); err != nil {
		t.Fatalf("PatchWorkingTree returned error %v", err)
	}

	if got := tr.readFile("f"); got != "a\nb\nc\nMORE\n" {
		t.Fatalf("working copy = %q", got)
	}
	if got := tr.stagedText("f"); got != "a\nb\nc\n" {
		t.Fatalf("the index changed: %q", got)
	}
}

func TestPatchingTheWorkingTreeBringsBackADeletedFile(t *testing.T) {
	tr := newTestRepo(t)
	tr.commitFiles("base", map[string]string{"f": "a\nb\n"})
	tr.remove("f")
	hunks := diff.Blobs([]byte("a\nb\n"), nil, diff.Defaults())
	restore, err := patch.Select(patch.Reverse(hunks), pickText(hunks, diff.KindDel, "a"))
	if err != nil {
		t.Fatal(err)
	}

	if err := PatchWorkingTree(t.Context(), tr.repo, "f", restore); err != nil {
		t.Fatalf("PatchWorkingTree returned error %v", err)
	}

	if got := tr.readFile("f"); got != "a\n" {
		t.Fatalf("working copy = %q", got)
	}
}

func TestPatchingTheWorkingTreeRefusesAStaleSelection(t *testing.T) {
	tr := newTestRepo(t)
	tr.commitFiles("base", map[string]string{"f": "a\nb\n"})
	stale := stagingPatch(t, "x\ny\n", "x\nY\n", diff.KindAdd, "Y")

	err := PatchWorkingTree(t.Context(), tr.repo, "f", stale)

	if !errors.Is(err, diff.ErrApply) {
		t.Fatalf("err = %v, want diff.ErrApply", err)
	}
}

func TestPatchingTheWorkingTreeStopsWhenTheFileCannotBeRead(t *testing.T) {
	tr := newTestRepo(t)
	tr.commitFiles("base", map[string]string{"f": "a\n"})
	original := fsRootReadFile
	fsRootReadFile = func(*os.Root, string) ([]byte, error) { return nil, errInjected }
	t.Cleanup(func() { fsRootReadFile = original })

	err := PatchWorkingTree(t.Context(), tr.repo, "f", stagingPatch(t, "a\n", "a\nb\n", diff.KindAdd, "b"))

	if !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want the injected failure", err)
	}
}

func TestPatchingTheWorkingTreeStopsWhenTheFileCannotBeWritten(t *testing.T) {
	tr := newTestRepo(t)
	tr.commitFiles("base", map[string]string{"f": "a\n"})
	swapRootOpenFileFailForPath(t, "f")

	err := PatchWorkingTree(t.Context(), tr.repo, "f", stagingPatch(t, "a\n", "a\nb\n", diff.KindAdd, "b"))

	if !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want the injected failure", err)
	}
}
