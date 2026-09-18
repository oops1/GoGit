package ops

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func sparseWorkRepo(t *testing.T) *testRepo {
	t.Helper()
	r := newTestRepo(t)
	r.commitFiles("base", map[string]string{
		"a/x.txt":      "1\n",
		"a/sub/z.txt":  "1\n",
		"b/k.md":       "1\n",
		"c/v.md":       "1\n",
		"c/deep/w.txt": "1\n",
		"top.md":       "1\n",
	})
	return r
}

func (r *testRepo) sparseFileText() string {
	r.t.Helper()
	data, err := os.ReadFile(filepath.Join(r.repo.GitDir(), "info", "sparse-checkout"))
	if err != nil {
		r.t.Fatalf("ReadFile returned error %v", err)
	}
	return string(data)
}

func (r *testRepo) worktreeConfigText() string {
	r.t.Helper()
	data, err := os.ReadFile(filepath.Join(r.repo.GitDir(), "config.worktree"))
	if err != nil {
		r.t.Fatalf("ReadFile returned error %v", err)
	}
	return string(data)
}

func (r *testRepo) presentPaths() []string {
	r.t.Helper()
	var present []string
	err := filepath.WalkDir(r.dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(r.dir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		switch {
		case rel == ".git":
			return filepath.SkipDir
		case rel == ".", entry.IsDir():
			return nil
		}
		present = append(present, rel)
		return nil
	})
	if err != nil {
		r.t.Fatalf("WalkDir returned error %v", err)
	}
	slices.Sort(present)
	return present
}

func requirePresent(t *testing.T, r *testRepo, want ...string) {
	t.Helper()
	slices.Sort(want)
	if got := r.presentPaths(); !slices.Equal(got, want) {
		t.Fatalf("working tree holds %v, want %v", got, want)
	}
}

func TestSparseCheckoutSetInConeModeWritesPatternsAndNarrowsTheWorkingTree(t *testing.T) {
	r := sparseWorkRepo(t)
	if err := SparseCheckoutSet(t.Context(), r.repo, []string{"a", "c/deep/"}, SparseCheckoutOptions{Cone: true}); err != nil {
		t.Fatalf("SparseCheckoutSet returned error %v", err)
	}
	if got, want := r.sparseFileText(), "/*\n!/*/\n/c/\n!/c/*/\n/a/\n/c/deep/\n"; got != want {
		t.Fatalf("sparse-checkout file = %q, want %q", got, want)
	}
	requirePresent(t, r, "a/sub/z.txt", "a/x.txt", "c/deep/w.txt", "c/v.md", "top.md")
	r.repo = r.reopen()
	listed, err := SparseCheckoutList(r.repo)
	if err != nil {
		t.Fatalf("SparseCheckoutList returned error %v", err)
	}
	if want := []string{"a", "c/deep"}; !slices.Equal(listed, want) {
		t.Fatalf("SparseCheckoutList = %v, want %v", listed, want)
	}
	for _, rel := range []string{"b/k.md"} {
		if entry, ok := entryOf(t, r.index(), rel); !ok || !entry.SkipWorktree {
			t.Fatalf("%s = %+v, want a skip-worktree entry", rel, entry)
		}
	}
}

func TestSparseCheckoutSetEnablesWorktreeConfig(t *testing.T) {
	r := sparseWorkRepo(t)
	if err := SparseCheckoutSet(t.Context(), r.repo, []string{"a"}, SparseCheckoutOptions{Cone: true}); err != nil {
		t.Fatalf("SparseCheckoutSet returned error %v", err)
	}
	local, err := os.ReadFile(filepath.Join(r.repo.GitDir(), "config"))
	if err != nil {
		t.Fatalf("ReadFile returned error %v", err)
	}
	if !strings.Contains(strings.ToLower(string(local)), "worktreeconfig = true") {
		t.Fatalf("local config = %q, want extensions.worktreeConfig", local)
	}
	worktree := r.worktreeConfigText()
	for _, want := range []string{"sparsecheckout = true", "sparsecheckoutcone = true"} {
		if !strings.Contains(strings.ToLower(worktree), want) {
			t.Fatalf("config.worktree = %q, want %q", worktree, want)
		}
	}
}

func TestSparseCheckoutAddWidensTheCone(t *testing.T) {
	r := sparseWorkRepo(t)
	if err := SparseCheckoutSet(t.Context(), r.repo, []string{"a"}, SparseCheckoutOptions{Cone: true}); err != nil {
		t.Fatalf("SparseCheckoutSet returned error %v", err)
	}
	r.repo = r.reopen()
	if err := SparseCheckoutAdd(t.Context(), r.repo, []string{"c/deep"}, SparseCheckoutOptions{}); err != nil {
		t.Fatalf("SparseCheckoutAdd returned error %v", err)
	}
	if got, want := r.sparseFileText(), "/*\n!/*/\n/c/\n!/c/*/\n/a/\n/c/deep/\n"; got != want {
		t.Fatalf("sparse-checkout file = %q, want %q", got, want)
	}
	requirePresent(t, r, "a/sub/z.txt", "a/x.txt", "c/deep/w.txt", "c/v.md", "top.md")
}

func TestSparseCheckoutAddOutsideConeModeAppendsPatterns(t *testing.T) {
	r := sparseWorkRepo(t)
	if err := SparseCheckoutSet(t.Context(), r.repo, []string{"/a/"}, SparseCheckoutOptions{}); err != nil {
		t.Fatalf("SparseCheckoutSet returned error %v", err)
	}
	r.repo = r.reopen()
	if err := SparseCheckoutAdd(t.Context(), r.repo, []string{"/top.md"}, SparseCheckoutOptions{}); err != nil {
		t.Fatalf("SparseCheckoutAdd returned error %v", err)
	}
	if got, want := r.sparseFileText(), "/a/\n/top.md\n"; got != want {
		t.Fatalf("sparse-checkout file = %q, want %q", got, want)
	}
	requirePresent(t, r, "a/sub/z.txt", "a/x.txt", "top.md")
	r.repo = r.reopen()
	listed, err := SparseCheckoutList(r.repo)
	if err != nil {
		t.Fatalf("SparseCheckoutList returned error %v", err)
	}
	if want := []string{"/a/", "/top.md"}; !slices.Equal(listed, want) {
		t.Fatalf("SparseCheckoutList = %v, want %v", listed, want)
	}
}

func TestSparseCheckoutInitKeepsAnExistingPatternFile(t *testing.T) {
	r := sparseWorkRepo(t)
	r.writeSparsePatterns("/*\n!/*/\n/b/\n")
	if err := SparseCheckoutInit(t.Context(), r.repo, SparseCheckoutOptions{Cone: true}); err != nil {
		t.Fatalf("SparseCheckoutInit returned error %v", err)
	}
	if got, want := r.sparseFileText(), "/*\n!/*/\n/b/\n"; got != want {
		t.Fatalf("sparse-checkout file = %q, want %q", got, want)
	}
	requirePresent(t, r, "b/k.md", "top.md")
}

func TestSparseCheckoutInitWithoutPatternsKeepsOnlyRootFiles(t *testing.T) {
	r := sparseWorkRepo(t)
	if err := SparseCheckoutInit(t.Context(), r.repo, SparseCheckoutOptions{Cone: true}); err != nil {
		t.Fatalf("SparseCheckoutInit returned error %v", err)
	}
	if got, want := r.sparseFileText(), "/*\n!/*/\n"; got != want {
		t.Fatalf("sparse-checkout file = %q, want %q", got, want)
	}
	requirePresent(t, r, "top.md")
}

func TestSparseCheckoutReapplyFollowsAnEditedPatternFile(t *testing.T) {
	r := sparseWorkRepo(t)
	if err := SparseCheckoutSet(t.Context(), r.repo, []string{"a"}, SparseCheckoutOptions{Cone: true}); err != nil {
		t.Fatalf("SparseCheckoutSet returned error %v", err)
	}
	r.writeSparsePatterns("/*\n!/*/\n/b/\n")
	r.repo = r.reopen()
	if err := SparseCheckoutReapply(t.Context(), r.repo); err != nil {
		t.Fatalf("SparseCheckoutReapply returned error %v", err)
	}
	requirePresent(t, r, "b/k.md", "top.md")
}

func TestSparseCheckoutDisableRestoresEveryFile(t *testing.T) {
	r := sparseWorkRepo(t)
	if err := SparseCheckoutSet(t.Context(), r.repo, []string{"a"}, SparseCheckoutOptions{Cone: true}); err != nil {
		t.Fatalf("SparseCheckoutSet returned error %v", err)
	}
	r.repo = r.reopen()
	if err := SparseCheckoutDisable(t.Context(), r.repo); err != nil {
		t.Fatalf("SparseCheckoutDisable returned error %v", err)
	}
	requirePresent(t, r, "a/sub/z.txt", "a/x.txt", "b/k.md", "c/deep/w.txt", "c/v.md", "top.md")
	for entry := range r.index().Entries() {
		if entry.SkipWorktree {
			t.Fatalf("%s is still marked skip-worktree", entry.Path)
		}
	}
	worktree := r.worktreeConfigText()
	for _, want := range []string{"sparsecheckout = false", "sparsecheckoutcone = false", "sparse = false"} {
		if !strings.Contains(strings.ToLower(worktree), want) {
			t.Fatalf("config.worktree = %q, want %q", worktree, want)
		}
	}
}

func TestSparseCheckoutCommandsNeedAnEnabledSparseCheckout(t *testing.T) {
	r := sparseWorkRepo(t)
	if _, err := SparseCheckoutList(r.repo); !errors.Is(err, ErrNotSparse) {
		t.Fatalf("SparseCheckoutList returned %v, want ErrNotSparse", err)
	}
	if err := SparseCheckoutReapply(t.Context(), r.repo); !errors.Is(err, ErrNotSparse) {
		t.Fatalf("SparseCheckoutReapply returned %v, want ErrNotSparse", err)
	}
	if err := SparseCheckoutAdd(t.Context(), r.repo, []string{"a"}, SparseCheckoutOptions{}); !errors.Is(err, ErrNotSparse) {
		t.Fatalf("SparseCheckoutAdd returned %v, want ErrNotSparse", err)
	}
}

func TestSparseCheckoutConeModeRejectsGlobPatterns(t *testing.T) {
	r := sparseWorkRepo(t)
	err := SparseCheckoutSet(t.Context(), r.repo, []string{"a/*"}, SparseCheckoutOptions{Cone: true})
	if !errors.Is(err, ErrConePatternNotPath) {
		t.Fatalf("SparseCheckoutSet returned %v, want ErrConePatternNotPath", err)
	}
	if err := SparseCheckoutSet(t.Context(), r.repo, []string{"a/*"}, SparseCheckoutOptions{Cone: true, SkipChecks: true}); err != nil {
		t.Fatalf("SparseCheckoutSet with SkipChecks returned error %v", err)
	}
}

func TestSparseConeDirectoryNormalizesInput(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		want    string
		wantErr error
	}{
		{"plain", "a", "/a", nil},
		{"trailingSlash", "a/b/", "/a/b", nil},
		{"leadingSlash", "/a/b", "/a/b", nil},
		{"backslashes", `a\b`, "/a/b", nil},
		{"dotSegments", "a/./b/../c", "/a/c", nil},
		{"root", "/", "", nil},
		{"negation", "!a", "", ErrConePatternNotPath},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := sparseConeDirectory(tc.pattern, false)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("sparseConeDirectory returned %v, want %v", err, tc.wantErr)
			}
			if err == nil && got != tc.want {
				t.Fatalf("sparseConeDirectory = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSparseConeLinesDropsDirectoriesCoveredByAParent(t *testing.T) {
	got := sparseConeLines([]string{"/a/b", "/a", "/c/d/e"})
	want := []string{"/*", "!/*/", "/c/", "!/c/*/", "/c/d/", "!/c/d/*/", "/a/", "/c/d/e/"}
	if !slices.Equal(got, want) {
		t.Fatalf("sparseConeLines = %v, want %v", got, want)
	}
}

func TestSparseCheckoutReportsWriteFailures(t *testing.T) {
	r := sparseWorkRepo(t)
	failure := errors.New("cannot write the pattern file")
	original := sparseWriteFile
	sparseWriteFile = func(string, []byte, os.FileMode) error { return failure }
	t.Cleanup(func() { sparseWriteFile = original })
	if err := SparseCheckoutSet(t.Context(), r.repo, []string{"a"}, SparseCheckoutOptions{Cone: true}); !errors.Is(err, failure) {
		t.Fatalf("SparseCheckoutSet returned %v, want %v", err, failure)
	}
}

func TestSparseCheckoutStopsOnACancelledContext(t *testing.T) {
	r := sparseWorkRepo(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := SparseCheckoutSet(ctx, r.repo, []string{"a"}, SparseCheckoutOptions{Cone: true}); err == nil {
		t.Fatalf("SparseCheckoutSet returned nil, want a cancellation error")
	}
	if err := SparseCheckoutDisable(ctx, r.repo); err == nil {
		t.Fatalf("SparseCheckoutDisable returned nil, want a cancellation error")
	}
}
