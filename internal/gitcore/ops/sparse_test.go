package ops

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/index"
)

const sparseConeOfA = "/*\n!/*/\n/a/\n"

func (r *testRepo) writeSparsePatterns(patterns string) {
	r.t.Helper()
	path := filepath.Join(r.repo.GitDir(), "info", "sparse-checkout")
	if err := os.MkdirAll(filepath.Dir(path), 0o777); err != nil {
		r.t.Fatalf("MkdirAll returned error %v", err)
	}
	if err := os.WriteFile(path, []byte(patterns), 0o666); err != nil {
		r.t.Fatalf("WriteFile returned error %v", err)
	}
}

func (r *testRepo) enableSparse(cone bool, patterns string) {
	r.t.Helper()
	r.appendConfig(fmt.Sprintf("[core]\n\tsparseCheckout = true\n\tsparseCheckoutCone = %t\n", cone))
	r.writeSparsePatterns(patterns)
	r.repo = r.reopen()
}

func (r *testRepo) sparseTopic() {
	r.t.Helper()
	base := r.commitFiles("base", map[string]string{"a/x": "1\n", "b/x": "1\n", "b/y": "1\n", "c/z": "1\n", "top": "1\n", "gone": "1\n"})
	r.createBranch("topic", base)
	r.switchTo("topic")
	r.commitFiles("topic", map[string]string{"a/x": "2\n", "b/y": "2\n", "b/new": "n\n", "c/z": "2\n", "c/deep/w": "n\n", "top": "2\n", "gone": ""})
	r.switchTo("main")
}

func requireSparseLayout(t *testing.T, r *testRepo, want map[string]string) {
	t.Helper()
	idx := r.index()
	var tracked []string
	for entry := range idx.Entries() {
		tracked = append(tracked, entry.Path)
	}
	wanted := make([]string, 0, len(want))
	for rel := range want {
		wanted = append(wanted, rel)
	}
	slices.Sort(wanted)
	if !slices.Equal(tracked, wanted) {
		t.Fatalf("tracked %v, want %v", tracked, wanted)
	}
	for rel, content := range want {
		entry, _ := entryOf(t, idx, rel)
		switch {
		case content == "" && (!entry.SkipWorktree || r.exists(rel)):
			t.Errorf("%s: skip-worktree %v, on disk %v; want it outside the working tree", rel, entry.SkipWorktree, r.exists(rel))
		case content != "" && entry.SkipWorktree:
			t.Errorf("%s is marked skip-worktree", rel)
		case content != "" && r.readFile(rel) != content:
			t.Errorf("%s = %q, want %q", rel, r.readFile(rel), content)
		}
	}
}

func TestSwitchChecksOutOnlyPathsInsideTheSparseCone(t *testing.T) {
	r := newTestRepo(t)
	r.sparseTopic()
	r.enableSparse(true, sparseConeOfA)

	r.switchTo("topic")
	requireSparseLayout(t, r, map[string]string{"a/x": "2\n", "b/x": "", "b/y": "", "b/new": "", "c/z": "", "c/deep/w": "", "top": "2\n"})
	for _, dir := range []string{"b", "c"} {
		if r.exists(dir) {
			t.Errorf("the directory %s was left behind", dir)
		}
	}

	r.switchTo("main")
	requireSparseLayout(t, r, map[string]string{"a/x": "1\n", "b/x": "", "b/y": "", "c/z": "", "top": "1\n", "gone": "1\n"})

	r.writeSparsePatterns(sparseConeOfA + "/b/\n")
	r.switchTo("topic")
	requireSparseLayout(t, r, map[string]string{"a/x": "2\n", "b/x": "1\n", "b/y": "2\n", "b/new": "n\n", "c/z": "", "c/deep/w": "", "top": "2\n"})
}

func TestSwitchFollowsSparsePatternsOutsideConeMode(t *testing.T) {
	r := newTestRepo(t)
	r.sparseTopic()
	r.enableSparse(false, "/a/\n/c/deep/\n/top\n")
	r.switchTo("topic")
	requireSparseLayout(t, r, map[string]string{"a/x": "2\n", "b/x": "", "b/y": "", "b/new": "", "c/z": "", "c/deep/w": "n\n", "top": "2\n"})
}

func TestSwitchKeepsLocalChangesOutsideTheSparsePatterns(t *testing.T) {
	r := newTestRepo(t)
	r.sparseTopic()
	r.writeFile("b/x", "local\n")
	r.writeFile("b/new", "untracked\n")
	r.enableSparse(true, sparseConeOfA)
	r.switchTo("topic")
	if entry, _ := entryOf(t, r.index(), "b/x"); entry.SkipWorktree || r.readFile("b/x") != "local\n" {
		t.Fatalf("b/x = %+v with %q, want the local change kept in the working tree", entry, r.readFile("b/x"))
	}
	if entry, _ := entryOf(t, r.index(), "b/new"); !entry.SkipWorktree || r.readFile("b/new") != "untracked\n" {
		t.Fatalf("b/new = %+v with %q, want the untracked file left alone", entry, r.readFile("b/new"))
	}

	changed := newTestRepo(t)
	changed.sparseTopic()
	changed.writeFile("b/y", "local\n")
	changed.enableSparse(true, sparseConeOfA)
	var overwrite *OverwriteError
	if err := Switch(t.Context(), changed.repo, "topic", SwitchOptions{}); !errors.As(err, &overwrite) || !slices.Equal(overwrite.Paths, []string{"b/y"}) {
		t.Fatalf("Switch returned %v, want an OverwriteError for b/y", err)
	}
}

func TestSwitchLeavesKeptEntriesThatAreMissingOrReplacedByDirectories(t *testing.T) {
	r := newTestRepo(t)
	r.sparseTopic()
	r.remove("b/x")
	r.enableSparse(true, sparseConeOfA)
	r.switchTo("topic")
	if entry, _ := entryOf(t, r.index(), "b/x"); !entry.SkipWorktree {
		t.Fatal("a deleted entry outside the cone is not skip-worktree")
	}

	dir := newTestRepo(t)
	dir.sparseTopic()
	dir.remove("b/x")
	dir.writeFile("b/x/inner", "keep\n")
	dir.enableSparse(true, sparseConeOfA)
	dir.switchTo("topic")
	if entry, _ := entryOf(t, dir.index(), "b/x"); entry.SkipWorktree || dir.readFile("b/x/inner") != "keep\n" {
		t.Fatalf("b/x = %+v, want the directory in its place kept", entry)
	}
}

func TestSparseCheckoutClearsSkipWorktreeOfFilesOnDisk(t *testing.T) {
	for _, expectOutside := range []bool{false, true} {
		r := newTestRepo(t)
		r.sparseTopic()
		r.markSkipWorktree("b/x")
		r.writeFile("b/x", "1\n")
		if expectOutside {
			r.appendConfig("[sparse]\n\texpectFilesOutsideOfPatterns = true\n")
		}
		r.enableSparse(true, sparseConeOfA)
		r.switchTo("topic")
		entry, _ := entryOf(t, r.index(), "b/x")
		if !entry.SkipWorktree || r.exists("b/x") != expectOutside {
			t.Fatalf("expectFilesOutsideOfPatterns=%v: b/x skip-worktree %v, on disk %v", expectOutside, entry.SkipWorktree, r.exists("b/x"))
		}
	}
}

func TestSwitchBringsBackSkipWorktreeEntriesWithoutOverwritingFiles(t *testing.T) {
	r := newTestRepo(t)
	r.sparseTopic()
	r.enableSparse(true, sparseConeOfA)
	r.switchTo("topic")
	r.switchTo("main")
	r.appendConfig("[sparse]\n\texpectFilesOutsideOfPatterns = true\n")
	r.repo = r.reopen()
	r.writeFile("b/x", "mine\n")
	r.writeSparsePatterns(sparseConeOfA + "/b/\n")
	r.switchTo("topic")
	if entry, _ := entryOf(t, r.index(), "b/x"); entry.SkipWorktree || r.readFile("b/x") != "mine\n" {
		t.Fatalf("b/x = %+v with %q, want the file on disk kept", entry, r.readFile("b/x"))
	}
}

func TestSparseCheckoutWithoutPatternsKeepsWritingNewPaths(t *testing.T) {
	r := newTestRepo(t)
	r.sparseTopic()
	r.appendConfig("[core]\n\tsparseCheckout = true\n")
	r.repo = r.reopen()
	r.switchTo("topic")
	if got := r.readFile("b/new"); got != "n\n" {
		t.Fatalf("b/new = %q", got)
	}
}

func TestSparseCheckoutRejectsInvalidConfiguration(t *testing.T) {
	for _, text := range []string{
		"[core]\n\tsparseCheckout = maybe\n",
		"[core]\n\tsparseCheckout = true\n[sparse]\n\texpectFilesOutsideOfPatterns = maybe\n",
		"[core]\n\tsparseCheckout = true\n\tsparseCheckoutCone = maybe\n",
	} {
		r := newTestRepo(t)
		r.initialCommit()
		r.appendConfig(text)
		r.repo = r.reopen()
		if err := Switch(t.Context(), r.repo, "main", SwitchOptions{}); err == nil || !strings.Contains(err.Error(), "sparse") {
			t.Fatalf("Switch with %q returned %v", text, err)
		}
	}
}

func failRemoveFor(path string) func(*os.Root, string) error {
	return func(root *os.Root, name string) error {
		if filepath.ToSlash(name) == path {
			return errInjected
		}
		return root.Remove(name)
	}
}

func TestSparseTransitionsStopOnWorkingTreeFailures(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(t *testing.T, r *testRepo)
	}{
		{"lstatLeaving", func(t *testing.T, _ *testRepo) { swapRootLstatFailForPath(t, "b/x") }},
		{"readLeaving", func(t *testing.T, _ *testRepo) {
			swapRootReadFile(t, func(root *os.Root, name string) ([]byte, error) {
				if filepath.ToSlash(name) == "b/x" {
					return nil, errInjected
				}
				return root.ReadFile(name)
			})
		}},
		{"removeLeaving", func(t *testing.T, _ *testRepo) { swapRootRemove(t, failRemoveFor("b/x")) }},
		{"removeUpdated", func(t *testing.T, _ *testRepo) { swapRootRemove(t, failRemoveFor("b/y")) }},
		{"writeEntering", func(t *testing.T, r *testRepo) {
			r.switchTo("topic")
			r.switchTo("main")
			r.writeSparsePatterns(sparseConeOfA + "/b/\n")
			swapRootOpenFileFailForPath(t, "b/x")
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newTestRepo(t)
			r.sparseTopic()
			r.enableSparse(true, sparseConeOfA)
			tc.prepare(t, r)
			if err := Switch(t.Context(), r.repo, "topic", SwitchOptions{}); !errors.Is(err, errInjected) {
				t.Fatalf("Switch returned %v, want %v", err, errInjected)
			}
		})
	}
}

func TestCheckoutTreeLeavesPathsOutsideTheSparsePatterns(t *testing.T) {
	r := newTestRepo(t)
	commit := r.commitFiles("base", map[string]string{"a/x": "1\n", "b/x": "1\n", "top": "1\n"})
	r.enableSparse(true, sparseConeOfA)
	idx := index.New(index.Version2)
	r.saveIndex(idx)
	for _, rel := range []string{"a", "b", "top"} {
		if err := os.RemoveAll(r.path(rel)); err != nil {
			t.Fatalf("RemoveAll returned error %v", err)
		}
	}
	if err := CheckoutTree(t.Context(), r.repo, commit, CheckoutOptions{}); err != nil {
		t.Fatalf("CheckoutTree returned error %v", err)
	}
	requireSparseLayout(t, r, map[string]string{"a/x": "1\n", "b/x": "", "top": "1\n"})
}
