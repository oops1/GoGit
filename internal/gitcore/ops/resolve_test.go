package ops

import (
	"errors"
	"slices"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/index"
)

func TestResolveConflictsTakesTheChosenSide(t *testing.T) {
	for side, want := range map[ConflictSide]string{TakeOurs: "OURS", TakeTheirs: "THEIRS"} {
		tr := newTestRepo(t)
		tr.conflictingFork()
		if _, err := tr.merge("feature", MergeOptions{}); err != nil {
			t.Fatal(err)
		}

		if err := ResolveConflicts(t.Context(), tr.repo, []string{"f"}, side); err != nil {
			t.Fatal(err)
		}

		if got := tr.readFile("f"); got != changeLine(tenLines("f"), 4, want) {
			t.Fatalf("side %d: file:\n%s", side, got)
		}
		if got := tr.stageEntries("f"); !slices.Equal(got, []index.Stage{index.StageMerged}) || tr.index().HasConflicts() {
			t.Fatalf("side %d: stages = %v", side, got)
		}
	}
}

func TestResolveConflictsFollowsADeletion(t *testing.T) {
	tr := newTestRepo(t)
	tr.fork(map[string]string{"f": changeLine(tenLines("f"), 4, "OURS")}, map[string]string{"f": ""})
	if _, err := tr.merge("feature", MergeOptions{}); err != nil {
		t.Fatal(err)
	}

	if err := ResolveConflicts(t.Context(), tr.repo, []string{"f"}, TakeTheirs); err != nil {
		t.Fatal(err)
	}

	if tr.exists("f") || len(tr.stageEntries("f")) != 0 {
		t.Fatal("the deleted side did not remove the file")
	}
}

func TestResolveConflictsRefusesPathsWithoutAConflict(t *testing.T) {
	tr := newTestRepo(t)
	tr.conflictingFork()
	if _, err := tr.merge("feature", MergeOptions{}); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{"keep", "../outside"} {
		if err := ResolveConflicts(t.Context(), tr.repo, []string{path}, TakeOurs); err == nil {
			t.Fatalf("%s was resolved", path)
		}
	}
	if err := ResolveConflicts(t.Context(), tr.repo, []string{"keep"}, TakeOurs); !errors.Is(err, ErrNotConflicted) {
		t.Fatalf("err = %v, want ErrNotConflicted", err)
	}
	if !tr.index().HasConflicts() {
		t.Fatal("a refused resolution still changed the index")
	}
}

func TestResolveConflictsInABareRepositoryFails(t *testing.T) {
	tr := newBareTestRepo(t)

	if err := ResolveConflicts(t.Context(), tr.repo, []string{"f"}, TakeOurs); !errors.Is(err, ErrBareRepository) {
		t.Fatalf("err = %v, want ErrBareRepository", err)
	}
}

func TestResolveConflictsStopsWhenTheObjectDatabaseCannotOpen(t *testing.T) {
	tr := newTestRepo(t)
	swapOdbOpenFailOnCall(t, 1)

	if err := ResolveConflicts(t.Context(), tr.repo, []string{"f"}, TakeOurs); !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want the injected failure", err)
	}
}
