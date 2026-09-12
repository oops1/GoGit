package ops

import (
	"context"
	"errors"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/diff"
)

func (r *testRepo) comparableFork() {
	r.t.Helper()
	f := tenLines("f")
	base := r.commitFiles("base", map[string]string{"f": f, "keep": "keep\n"})
	r.createBranch("feature", base)
	r.commitFiles("ours", map[string]string{"f": changeLine(f, 0, "OURS")})
	r.switchTo("feature")
	r.commitFiles("theirs", map[string]string{"g": "g\n"})
	r.switchTo("main")
}

func TestComparingTwoBranchesCountsBothSides(t *testing.T) {
	tr := newTestRepo(t)
	tr.comparableFork()

	result, err := Compare(t.Context(), tr.repo, "main", "feature", CompareOptions{})

	if err != nil {
		t.Fatalf("Compare returned error %v", err)
	}
	if result.Ahead != 1 || result.Behind != 1 || result.Same() {
		t.Fatalf("result = %+v", result)
	}
	if result.Base != tr.branchTarget("feature") && result.Base.IsZero() {
		t.Fatalf("base = %s", result.Base)
	}
	var names []string
	for _, file := range result.Changes {
		names = append(names, file.NewPath)
	}
	if len(names) != 2 {
		t.Fatalf("changes = %v", names)
	}
}

func TestComparingAPlaceWithItselfFindsNothing(t *testing.T) {
	tr := newTestRepo(t)
	tr.commitFiles("base", map[string]string{"f": "f\n"})

	result, err := Compare(t.Context(), tr.repo, "HEAD", "main", CompareOptions{})

	if err != nil {
		t.Fatalf("Compare returned error %v", err)
	}
	if !result.Same() || result.Ahead != 0 || result.Behind != 0 || len(result.Changes) != 0 {
		t.Fatalf("result = %+v", result)
	}
}

func TestComparingUnrelatedHistoriesHasNoBase(t *testing.T) {
	tr := newTestRepo(t)
	first := tr.commitFiles("base", map[string]string{"f": "f\n"})
	tr.writeRawHead("ref: refs/heads/other\n")
	tr.repo = tr.reopen()
	tr.remove("f")
	second := tr.commitFiles("unrelated", map[string]string{"g": "g\n"})

	result, err := Compare(t.Context(), tr.repo, first.String(), second.String(), CompareOptions{})

	if err != nil {
		t.Fatalf("Compare returned error %v", err)
	}
	if !result.Base.IsZero() || result.Ahead != 1 || result.Behind != 1 {
		t.Fatalf("result = %+v", result)
	}
}

func TestComparingHonoursTheDiffOptionsItIsGiven(t *testing.T) {
	tr := newTestRepo(t)
	f := tenLines("f")
	base := tr.commitFiles("base", map[string]string{"f": f})
	tr.createBranch("feature", base)
	tr.switchTo("feature")
	tr.remove("f")
	tr.commitFiles("move", map[string]string{"f": "", "moved": f})
	tr.switchTo("main")

	renames, err := Compare(t.Context(), tr.repo, "main", "feature", CompareOptions{Diff: diff.Defaults()})
	if err != nil {
		t.Fatalf("Compare returned error %v", err)
	}
	plain, err := Compare(t.Context(), tr.repo, "main", "feature", CompareOptions{Diff: diff.Options{RenameThreshold: 100}})
	if err != nil {
		t.Fatalf("Compare returned error %v", err)
	}

	if len(renames.Changes) != 1 || renames.Changes[0].Status != diff.StatusRenamed {
		t.Fatalf("with renames = %+v", renames.Changes)
	}
	if len(plain.Changes) != 2 {
		t.Fatalf("without renames = %+v", plain.Changes)
	}
}

func TestComparingAnUnknownPlaceFails(t *testing.T) {
	tr := newTestRepo(t)
	tr.commitFiles("base", map[string]string{"f": "f\n"})

	if _, err := Compare(t.Context(), tr.repo, "nope", "main", CompareOptions{}); !errors.Is(err, ErrTargetNotFound) {
		t.Fatalf("left: err = %v", err)
	}
	if _, err := Compare(t.Context(), tr.repo, "main", "nope", CompareOptions{}); !errors.Is(err, ErrTargetNotFound) {
		t.Fatalf("right: err = %v", err)
	}
}

func TestComparingHonoursACancelledContext(t *testing.T) {
	tr := newTestRepo(t)
	tr.comparableFork()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, err := Compare(ctx, tr.repo, "main", "feature", CompareOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}
