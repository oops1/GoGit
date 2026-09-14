package ops

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/index"
	"github.com/oops1/gogit/internal/gitcore/object"
)

func (r *testRepo) markSkipWorktree(paths ...string) {
	r.t.Helper()
	idx := r.index()
	for _, rel := range paths {
		entry, ok := idx.Get(rel, index.StageMerged)
		if !ok {
			r.t.Fatalf("%s is not staged", rel)
		}
		entry.SkipWorktree = true
		r.remove(rel)
	}
	r.saveIndex(idx)
}

func requireStatMatchesDisk(t *testing.T, r *testRepo, rel string) {
	t.Helper()
	entry, ok := entryOf(t, r.index(), rel)
	if !ok {
		t.Fatalf("%s is not staged", rel)
	}
	info, err := os.Lstat(r.path(rel))
	if err != nil {
		t.Fatalf("Lstat returned error %v", err)
	}
	if int64(entry.Stat.Size) != info.Size() || !entry.Matches(info, false, true) {
		t.Fatalf("%s: stat %+v does not describe the file of %d bytes changed at %v", rel, entry.Stat, info.Size(), info.ModTime())
	}
}

func requireSparseEntry(t *testing.T, r *testRepo, rel, content string) {
	t.Helper()
	entry, ok := entryOf(t, r.index(), rel)
	if !ok || !entry.SkipWorktree || entry.ID != storedBlob(t, r, content) {
		t.Fatalf("%s = %+v, %v; want a skip-worktree entry of %q", rel, entry, ok, content)
	}
	if r.exists(rel) {
		t.Fatalf("%s was written to the working tree", rel)
	}
}

func swapCaseInsensitiveLstat(t testing.TB, alias, actual string) {
	t.Helper()
	original := fsRootLstat
	fsRootLstat = func(root *os.Root, name string) (fs.FileInfo, error) {
		info, err := original(root, name)
		if err != nil && filepath.ToSlash(name) == alias {
			return original(root, filepath.FromSlash(actual))
		}
		return info, err
	}
	t.Cleanup(func() { fsRootLstat = original })
}

func caseInsensitiveFileSystem(t *testing.T) bool {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "probe"), nil, 0o666); err != nil {
		t.Fatalf("WriteFile returned error %v", err)
	}
	_, err := os.Lstat(filepath.Join(dir, "PROBE"))
	return err == nil
}

func newIgnoreCaseRepo(t *testing.T) *testRepo {
	t.Helper()
	r := newTestRepo(t)
	r.appendConfig("[core]\n\tignorecase = true\n")
	r.repo = r.reopen()
	return r
}

func TestSwitchKeepsAPopulatedSubmoduleDirectory(t *testing.T) {
	r := newTestRepo(t)
	main := r.initialCommit()
	tree := putTree(t, r,
		object.TreeEntry{Mode: object.ModeBlob, Name: "a.txt", ID: storedBlob(t, r, "hello\n")},
		object.TreeEntry{Mode: object.ModeSubmodule, Name: "sub", ID: main},
	)
	r.createBranch("withsub", putCommit(t, r, tree, main))
	r.switchTo("withsub")
	r.writeFile("sub/file", "inside\n")
	if err := Switch(t.Context(), r.repo, "main", SwitchOptions{}); err != nil {
		t.Fatalf("Switch returned error %v", err)
	}
	if got := r.readFile("sub/file"); got != "inside\n" {
		t.Fatalf("sub/file = %q", got)
	}
	if _, ok := entryOf(t, r.index(), "sub"); ok {
		t.Fatal("the submodule is still tracked")
	}
}

func TestSwitchFailsWhenATrackedFileCannotBeRemoved(t *testing.T) {
	r := newTestRepo(t)
	main := r.initialCommit()
	r.createBranch("feature", main)
	r.switchTo("feature")
	r.writeFile("b.txt", "b\n")
	mustStage(t, r, "b.txt")
	r.commitAll("b")
	swapRootRemoveFailForPath(t, "b.txt")
	if err := Switch(t.Context(), r.repo, "main", SwitchOptions{}); !errors.Is(err, errInjected) {
		t.Fatalf("Switch = %v, want errInjected", err)
	}
}

func (r *testRepo) branchWithDirectory(name string) {
	r.t.Helper()
	t := r.t.(*testing.T)
	inner := putTree(t, r,
		object.TreeEntry{Mode: object.ModeBlob, Name: "x", ID: storedBlob(t, r, "x\n")},
		object.TreeEntry{Mode: object.ModeBlob, Name: "y", ID: storedBlob(t, r, "y\n")},
	)
	tree := putTree(t, r,
		object.TreeEntry{Mode: object.ModeBlob, Name: "a.txt", ID: storedBlob(t, r, "feature\n")},
		object.TreeEntry{Mode: object.ModeTree, Name: name, ID: inner},
	)
	r.createBranch("feature", putCommit(t, r, tree, r.branchTarget("main")))
}

func TestSwitchRefusesAnUntrackedFileInTheLeadingPath(t *testing.T) {
	r := newTestRepo(t)
	r.initialCommit()
	r.branchWithDirectory("d")
	r.writeFile("d", "untracked\n")
	err := Switch(t.Context(), r.repo, "feature", SwitchOptions{})
	var overwrite *OverwriteError
	if !errors.As(err, &overwrite) || !slices.Equal(overwrite.Paths, []string{"d"}) {
		t.Fatalf("Switch = %v, want an overwrite of d", err)
	}
	if r.readFile("a.txt") != "hello\n" || r.readFile("d") != "untracked\n" {
		t.Fatal("the refused switch changed the working tree")
	}
	if err := Switch(t.Context(), r.repo, "feature", SwitchOptions{Force: true}); err != nil {
		t.Fatalf("Switch returned error %v", err)
	}
	if got := r.readFile("d/x"); got != "x\n" {
		t.Fatalf("d/x = %q", got)
	}
}

func TestSwitchReplacesATrackedFileWithADirectory(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	r.writeFile("d", "tracked\n")
	mustStage(t, r, "a.txt")
	mustStage(t, r, "d")
	r.commitAll("initial")
	r.branchWithDirectory("d")
	if err := Switch(t.Context(), r.repo, "feature", SwitchOptions{}); err != nil {
		t.Fatalf("Switch returned error %v", err)
	}
	if got := r.readFile("d/y"); got != "y\n" {
		t.Fatalf("d/y = %q", got)
	}
}

func TestSwitchAddsFilesToAnExistingDirectory(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	r.writeFile("dir/z", "z\n")
	mustStage(t, r, "a.txt")
	mustStage(t, r, "dir")
	main := r.commitAll("initial")
	inner := putTree(t, r,
		object.TreeEntry{Mode: object.ModeBlob, Name: "x", ID: storedBlob(t, r, "x\n")},
		object.TreeEntry{Mode: object.ModeBlob, Name: "z", ID: storedBlob(t, r, "z\n")},
	)
	r.createBranch("feature", putCommit(t, r, putTree(t, r, object.TreeEntry{Mode: object.ModeTree, Name: "dir", ID: inner}), main))
	if err := Switch(t.Context(), r.repo, "feature", SwitchOptions{}); err != nil {
		t.Fatalf("Switch returned error %v", err)
	}
	if got := r.readFile("dir/x"); got != "x\n" {
		t.Fatalf("dir/x = %q", got)
	}
}

func TestSwitchFailsWhenTheLeadingPathCannotBeInspected(t *testing.T) {
	r := newTestRepo(t)
	r.initialCommit()
	r.branchWithDirectory("d")
	swapRootLstatFailForPath(t, "d")
	if err := Switch(t.Context(), r.repo, "feature", SwitchOptions{}); !errors.Is(err, errInjected) {
		t.Fatalf("Switch = %v, want errInjected", err)
	}
}

func (r *testRepo) caseOnlyRename(content string) {
	r.t.Helper()
	t := r.t.(*testing.T)
	r.writeFile("readme", "hello\n")
	mustStage(t, r, "readme")
	main := r.commitAll("initial")
	r.createBranch("feature", putCommit(t, r, treeAt(t, r, "README", object.ModeBlob, storedBlob(t, r, "hello\n")), main))
	r.writeFile("readme", content)
}

func requireCaseOnlyRename(t *testing.T, r *testRepo) {
	t.Helper()
	if err := Switch(t.Context(), r.repo, "feature", SwitchOptions{}); err != nil {
		t.Fatalf("Switch returned error %v", err)
	}
	idx := r.index()
	_, upper := entryOf(t, idx, "README")
	_, lower := entryOf(t, idx, "readme")
	if !upper || lower {
		t.Fatalf("README staged %v, readme staged %v", upper, lower)
	}
	entries, err := os.ReadDir(r.dir)
	if err != nil {
		t.Fatalf("ReadDir returned error %v", err)
	}
	names := []string{}
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	if !slices.Contains(names, "README") || slices.Contains(names, "readme") {
		t.Fatalf("working tree holds %v", names)
	}
}

func TestSwitchRenamesAFileByCaseWhenCaseIsIgnored(t *testing.T) {
	r := newIgnoreCaseRepo(t)
	r.caseOnlyRename("hello\n")
	swapCaseInsensitiveLstat(t, "README", "readme")
	requireCaseOnlyRename(t, r)
}

func TestSwitchRenamesAFileByCaseOnACaseInsensitiveFileSystem(t *testing.T) {
	if !caseInsensitiveFileSystem(t) {
		t.Skip("the file system is case-sensitive")
	}
	r := newIgnoreCaseRepo(t)
	r.caseOnlyRename("hello\n")
	requireCaseOnlyRename(t, r)
}

func TestSwitchRefusesACaseOnlyRenameOverAModifiedFile(t *testing.T) {
	r := newIgnoreCaseRepo(t)
	r.caseOnlyRename("changed\n")
	swapCaseInsensitiveLstat(t, "README", "readme")
	err := Switch(t.Context(), r.repo, "feature", SwitchOptions{})
	var overwrite *OverwriteError
	if !errors.As(err, &overwrite) || !slices.Contains(overwrite.Paths, "README") {
		t.Fatalf("Switch = %v, want an overwrite of README", err)
	}
}

func TestSwitchReplacesAFileWithADirectoryOfAnotherCase(t *testing.T) {
	r := newIgnoreCaseRepo(t)
	r.writeFile("d", "tracked\n")
	mustStage(t, r, "d")
	main := r.commitAll("initial")
	r.createBranch("feature", putCommit(t, r, treeAt(t, r, "D/x", object.ModeBlob, storedBlob(t, r, "x\n")), main))
	swapCaseInsensitiveLstat(t, "D", "d")
	if err := Switch(t.Context(), r.repo, "feature", SwitchOptions{}); err != nil {
		t.Fatalf("Switch returned error %v", err)
	}
	if got := r.readFile("D/x"); got != "x\n" {
		t.Fatalf("D/x = %q", got)
	}
}

func (r *testRepo) sparseHistory() {
	r.t.Helper()
	t := r.t.(*testing.T)
	main := r.commitFiles("initial", map[string]string{"a.txt": "one\n", "sparse/b.txt": "one\n", "sparse/c.txt": "one\n"})
	tree := putTree(t, r,
		object.TreeEntry{Mode: object.ModeBlob, Name: "a.txt", ID: storedBlob(t, r, "two\n")},
		object.TreeEntry{Mode: object.ModeTree, Name: "sparse", ID: treeAt(t, r, "b.txt", object.ModeBlob, storedBlob(t, r, "two\n"))},
	)
	r.createBranch("feature", putCommit(t, r, tree, main))
	r.markSkipWorktree("sparse/b.txt", "sparse/c.txt")
}

func TestSwitchKeepsSkipWorktreeEntriesOutOfTheWorkingTree(t *testing.T) {
	for _, force := range []bool{false, true} {
		r := newTestRepo(t)
		r.sparseHistory()
		if err := Switch(t.Context(), r.repo, "feature", SwitchOptions{Force: force}); err != nil {
			t.Fatalf("Switch returned error %v", err)
		}
		if got := r.readFile("a.txt"); got != "two\n" {
			t.Fatalf("a.txt = %q", got)
		}
		requireSparseEntry(t, r, "sparse/b.txt", "two\n")
		if _, ok := entryOf(t, r.index(), "sparse/c.txt"); ok {
			t.Fatal("sparse/c.txt is still tracked")
		}
	}
}

func TestStageLeavesSkipWorktreeEntriesAlone(t *testing.T) {
	r := newTestRepo(t)
	r.commitFiles("initial", map[string]string{"a.txt": "one\n", "sparse/b.txt": "one\n", "sparse/c.txt": "one\n"})
	r.markSkipWorktree("sparse/b.txt", "sparse/c.txt")
	steps := []func(){
		func() { mustStage(t, r, "sparse/b.txt") },
		func() { mustStage(t, r, "sparse") },
		func() { r.writeFile("sparse/c.txt", "changed\n"); mustStage(t, r, "sparse"); r.remove("sparse/c.txt") },
		func() { r.remove("sparse"); mustStage(t, r, "sparse") },
	}
	for at, step := range steps {
		step()
		for _, rel := range []string{"sparse/b.txt", "sparse/c.txt"} {
			entry, ok := entryOf(t, r.index(), rel)
			if !ok || !entry.SkipWorktree || entry.ID != storedBlob(t, r, "one\n") {
				t.Fatalf("step %d: %s = %+v, %v", at, rel, entry, ok)
			}
		}
	}
}

func TestResetsKeepSkipWorktreeEntries(t *testing.T) {
	for _, mode := range []ResetMode{ResetMixed, ResetHard} {
		r := newTestRepo(t)
		first := r.commitFiles("one", map[string]string{"a.txt": "one\n", "s/b": "one\n"})
		r.commitFiles("two", map[string]string{"s/b": "two\n"})
		r.markSkipWorktree("s/b")
		if _, err := r.reset(first.String(), resetOptions(mode)); err != nil {
			t.Fatalf("reset returned error %v", err)
		}
		requireSparseEntry(t, r, "s/b", "one\n")
	}
}

func TestCheckedOutFilesKeepTheirStatInTheIndex(t *testing.T) {
	scenarios := map[string]func(t *testing.T, r *testRepo) string{
		"switch": func(t *testing.T, r *testRepo) string {
			r.initialCommit()
			r.branchWithDirectory("d")
			r.switchTo("feature")
			return "d/x"
		},
		"hard reset": func(t *testing.T, r *testRepo) string {
			first := r.commitFiles("one", map[string]string{"f": "one\n"})
			r.commitFiles("two", map[string]string{"f": "two\n"})
			if _, err := r.reset(first.String(), resetOptions(ResetHard)); err != nil {
				t.Fatalf("reset returned error %v", err)
			}
			return "f"
		},
		"mixed reset of a clean file": func(t *testing.T, r *testRepo) string {
			first := r.commitFiles("one", map[string]string{"f": "one\n"})
			r.commitFiles("two", map[string]string{"f": "two\n"})
			r.writeFile("f", "one\n")
			if _, err := r.reset(first.String(), resetOptions(ResetMixed)); err != nil {
				t.Fatalf("reset returned error %v", err)
			}
			return "f"
		},
		"reset of a clean path": func(t *testing.T, r *testRepo) string {
			first := r.commitFiles("one", map[string]string{"f": "one\n"})
			r.commitFiles("two", map[string]string{"f": "two\n"})
			r.writeFile("f", "one\n")
			if _, err := r.reset(first.String(), resetOptions(ResetMixed, "f")); err != nil {
				t.Fatalf("reset returned error %v", err)
			}
			return "f"
		},
		"unstage of a clean file": func(t *testing.T, r *testRepo) string {
			r.commitFiles("one", map[string]string{"f": "one\n"})
			r.writeFile("f", "two\n")
			mustStage(t, r, "f")
			r.writeFile("f", "one\n")
			if err := Unstage(t.Context(), r.repo, []string{"f"}); err != nil {
				t.Fatalf("Unstage returned error %v", err)
			}
			return "f"
		},
		"unstage of a directory": func(t *testing.T, r *testRepo) string {
			r.commitFiles("one", map[string]string{"dir/f": "one\n"})
			r.writeFile("dir/f", "two\n")
			mustStage(t, r, "dir/f")
			r.writeFile("dir/f", "one\n")
			if err := Unstage(t.Context(), r.repo, []string{"dir"}); err != nil {
				t.Fatalf("Unstage returned error %v", err)
			}
			return "dir/f"
		},
		"resolve": func(t *testing.T, r *testRepo) string {
			r.conflictingFork()
			if _, err := r.merge("feature", MergeOptions{}); err != nil {
				t.Fatalf("merge returned error %v", err)
			}
			if err := ResolveConflicts(t.Context(), r.repo, []string{"f"}, TakeTheirs); err != nil {
				t.Fatalf("ResolveConflicts returned error %v", err)
			}
			return "f"
		},
		"fast-forward merge": func(t *testing.T, r *testRepo) string {
			base := r.commitFiles("base", map[string]string{"f": "one\n"})
			r.createBranch("feature", base)
			r.switchTo("feature")
			r.commitFiles("next", map[string]string{"f": "two\n"})
			r.switchTo("main")
			if _, err := r.merge("feature", MergeOptions{}); err != nil {
				t.Fatalf("merge returned error %v", err)
			}
			return "f"
		},
		"stage with line ending conversion": func(t *testing.T, r *testRepo) string {
			r.appendConfig("[core]\n\tautocrlf = true\n")
			r.repo = r.reopen()
			r.writeFile("crlf.txt", "a\r\nb\r\n")
			mustStage(t, r, "crlf.txt")
			return "crlf.txt"
		},
	}
	for name, scenario := range scenarios {
		t.Run(name, func(t *testing.T) {
			r := newTestRepo(t)
			requireStatMatchesDisk(t, r, scenario(t, r))
		})
	}
}

func TestAMixedResetStoresNoStatForFilesItCannotVouchFor(t *testing.T) {
	worktrees := map[string]func(r *testRepo){
		"changed": func(*testRepo) {},
		"missing": func(r *testRepo) { r.remove("f") },
		"a directory": func(r *testRepo) {
			r.remove("f")
			r.writeFile("f/inner", "x\n")
		},
	}
	for name, prepare := range worktrees {
		t.Run(name, func(t *testing.T) {
			r := newTestRepo(t)
			first := r.commitFiles("one", map[string]string{"f": "one\n"})
			r.commitFiles("two", map[string]string{"f": "two\n"})
			prepare(r)
			if _, err := r.reset(first.String(), resetOptions(ResetMixed)); err != nil {
				t.Fatalf("reset returned error %v", err)
			}
			if entry, ok := entryOf(t, r.index(), "f"); !ok || entry.Stat.Size != 0 {
				t.Fatalf("f = %+v, %v", entry, ok)
			}
		})
	}
}

func TestAMixedResetStoresNoStatForASubmodule(t *testing.T) {
	r := newTestRepo(t)
	main := r.initialCommit()
	commit := putCommit(t, r, putTree(t, r, object.TreeEntry{Mode: object.ModeSubmodule, Name: "sub", ID: main}), main)
	if _, err := r.reset(commit.String(), resetOptions(ResetMixed)); err != nil {
		t.Fatalf("reset returned error %v", err)
	}
	if entry, ok := entryOf(t, r.index(), "sub"); !ok || entry.Stat.Size != 0 {
		t.Fatalf("sub = %+v, %v", entry, ok)
	}
}

func TestCheckoutFailsWhenTheWrittenFileCannotBeInspected(t *testing.T) {
	r := newTestRepo(t)
	r.appendConfig("[core]\n\tfilemode = false\n")
	r.repo = r.reopen()
	r.initialCommit()
	r.branchWithDirectory("d")
	armed := false
	originalOpen := fsRootOpenFile
	fsRootOpenFile = func(root *os.Root, name string, flag int, perm fs.FileMode) (*os.File, error) {
		if filepath.ToSlash(name) == "d/x" {
			armed = true
		}
		return originalOpen(root, name, flag, perm)
	}
	t.Cleanup(func() { fsRootOpenFile = originalOpen })
	originalLstat := fsRootLstat
	fsRootLstat = func(root *os.Root, name string) (fs.FileInfo, error) {
		if armed && filepath.ToSlash(name) == "d/x" {
			return nil, errInjected
		}
		return originalLstat(root, name)
	}
	t.Cleanup(func() { fsRootLstat = originalLstat })
	if err := Switch(t.Context(), r.repo, "feature", SwitchOptions{}); !errors.Is(err, errInjected) {
		t.Fatalf("Switch = %v, want errInjected", err)
	}
}

func TestIndexUpdatesFailWhenAFileCannotBeVouchedFor(t *testing.T) {
	faults := map[string]func(t *testing.T, rel string){
		"lstat": swapRootLstatFailForPathT,
		"read": func(t *testing.T, rel string) {
			swapRootReadFile(t, func(root *os.Root, name string) ([]byte, error) {
				if filepath.ToSlash(name) == rel {
					return nil, errInjected
				}
				return root.ReadFile(name)
			})
		},
	}
	operations := map[string]func(t *testing.T, r *testRepo) error{
		"mixed reset": func(t *testing.T, r *testRepo) error {
			_, err := r.reset(r.branchTarget("main").String()+"~1", resetOptions(ResetMixed))
			return err
		},
		"reset of paths": func(t *testing.T, r *testRepo) error {
			_, err := r.reset(r.branchTarget("main").String()+"~1", resetOptions(ResetMixed, "dir/f"))
			return err
		},
		"unstage of a file": func(t *testing.T, r *testRepo) error {
			return Unstage(t.Context(), r.repo, []string{"dir/f"})
		},
		"unstage of a directory": func(t *testing.T, r *testRepo) error {
			return Unstage(t.Context(), r.repo, []string{"dir"})
		},
	}
	for faultName, fault := range faults {
		for operationName, operation := range operations {
			t.Run(faultName+" "+operationName, func(t *testing.T) {
				r := newTestRepo(t)
				r.commitFiles("one", map[string]string{"dir/f": "one\n"})
				r.commitFiles("two", map[string]string{"dir/f": "two\n"})
				r.writeFile("dir/f", "three\n")
				mustStage(t, r, "dir/f")
				fault(t, "dir/f")
				if err := operation(t, r); !errors.Is(err, errInjected) {
					t.Fatalf("err = %v, want errInjected", err)
				}
			})
		}
	}
}

func swapRootLstatFailForPathT(t *testing.T, rel string) {
	t.Helper()
	swapRootLstatFailForPath(t, rel)
}

func TestUnstageNeedsAWorkingTree(t *testing.T) {
	r := newBareTestRepo(t)
	if err := Unstage(t.Context(), r.repo, []string{"a.txt"}); !errors.Is(err, ErrBareRepository) {
		t.Fatalf("Unstage = %v, want ErrBareRepository", err)
	}
}
