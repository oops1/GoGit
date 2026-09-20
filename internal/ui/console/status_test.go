package console

import (
	"errors"
	"slices"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/worktree"
)

func TestStatusPrintsThePorcelainFormat(t *testing.T) {
	r := newTestRepo(t)
	r.commit("initial", map[string]string{"kept.txt": "kept\n", "gone.txt": "gone\n"})
	r.write("kept.txt", "changed\n")
	r.write("fresh.txt", "fresh\n")
	r.remove("gone.txt")
	r.run("add kept.txt")

	got := lines(r.run("status --porcelain"))

	want := []string{"M  kept.txt", " D gone.txt", "?? fresh.txt"}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("status = %#v, want %#v", got, want)
	}
}

func TestStatusShortAndBareOutputMatch(t *testing.T) {
	r := newTestRepo(t)
	r.commit("initial", map[string]string{"a.txt": "a\n"})
	r.write("a.txt", "b\n")

	if bare, short := r.run("status"), r.run("status -s"); bare != short || bare != " M a.txt" {
		t.Fatalf("bare = %q, short = %q", bare, short)
	}
}

func TestStatusAddsTheBranchHeaderOnRequest(t *testing.T) {
	r := newTestRepo(t)
	r.commit("initial", map[string]string{"a.txt": "a\n"})

	if got := r.run("status -b"); got != "## main" {
		t.Fatalf("status = %q", got)
	}
}

func TestStatusShowsIgnoredFilesOnlyWhenAsked(t *testing.T) {
	r := newTestRepo(t)
	r.commit("initial", map[string]string{".gitignore": "hidden.txt\n"})
	r.write("hidden.txt", "hidden\n")

	if got := r.run("status --porcelain"); got != "" {
		t.Fatalf("status = %q", got)
	}
	if got := r.run("status --porcelain --ignored"); got != "!! hidden.txt" {
		t.Fatalf("status = %q", got)
	}
}

func TestStatusRefusesExtraWords(t *testing.T) {
	r := newTestRepo(t)
	if err := r.runFails("status extra"); !errors.Is(err, ErrUsage) {
		t.Fatalf("err = %v", err)
	}
}

func TestStatusFailsWithoutAWorkingTree(t *testing.T) {
	r := newBareTestRepo(t)
	if err := r.runFails("status"); !errors.Is(err, worktree.ErrBareRepository) {
		t.Fatalf("err = %v", err)
	}
}

func TestTheBranchHeaderNamesADetachedHead(t *testing.T) {
	r := newTestRepo(t)
	first := r.commit("initial", map[string]string{"a.txt": "a\n"})
	r.run("checkout " + first.String())

	if got := r.run("status -b"); got != "## "+detachedHeadLabel {
		t.Fatalf("status = %q", got)
	}
}

func TestTheBranchHeaderCountsAheadAndBehind(t *testing.T) {
	tests := []struct {
		name   string
		status worktree.Status
		want   string
	}{
		{"even", worktree.Status{HeadBranch: "main"}, "main"},
		{"ahead", worktree.Status{HeadBranch: "main", Ahead: 2}, "main [ahead 2]"},
		{"behind", worktree.Status{HeadBranch: "main", Behind: 3}, "main [behind 3]"},
		{"both", worktree.Status{HeadBranch: "main", Ahead: 1, Behind: 4}, "main [ahead 1, behind 4]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := statusBranchLabel(tt.status); got != tt.want {
				t.Fatalf("label = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestARenamedPathKeepsBothNames(t *testing.T) {
	line, ok := statusEntryLine(worktree.Entry{
		Path:     "new.txt",
		OrigPath: "old.txt",
		Staged:   worktree.StatusRenamed,
		Unstaged: worktree.StatusUnmodified,
	}, false)
	if !ok || line != "R  old.txt -> new.txt" {
		t.Fatalf("line = %q, ok = %v", line, ok)
	}
}

func TestAnUnchangedPathIsLeftOut(t *testing.T) {
	unchanged := worktree.Entry{Path: "a.txt", Staged: worktree.StatusUnmodified, Unstaged: worktree.StatusUnmodified}
	if _, ok := statusEntryLine(unchanged, false); ok {
		t.Fatal("an unchanged entry must not print")
	}
}
