package ops

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/diff"
	"github.com/oops1/gogit/internal/gitcore/index"
	"github.com/oops1/gogit/internal/gitcore/object"
)

func (r *testRepo) setIndexMode(rel string, mode object.Mode, content string) {
	r.t.Helper()
	idx := r.index()
	entry, ok := idx.Get(rel, index.StageMerged)
	if !ok {
		r.t.Fatalf("%s is not in the index", rel)
	}
	changed := *entry
	changed.Mode = mode
	if content != "" {
		changed.ID = storedBlob(r.t.(*testing.T), r, content)
	}
	idx.Add(changed)
	r.saveIndex(idx)
}

func (r *testRepo) stageAll(paths ...string) {
	r.t.Helper()
	if err := Stage(r.t.Context(), r.repo, paths, StageOptions{}); err != nil {
		r.t.Fatalf("Stage returned error %v", err)
	}
}

func (r *testRepo) dropFromIndex(rel string) {
	r.t.Helper()
	idx := r.index()
	idx.Remove(rel)
	r.saveIndex(idx)
}

func TestStashPushRejectsStagedWithUntrackedAndInvalidPathspecs(t *testing.T) {
	tr := newTestRepo(t)
	stashChanges(tr)
	for _, tc := range []struct {
		opts StashOptions
		want error
	}{
		{StashOptions{Staged: true, IncludeUntracked: true}, ErrStashStagedUntracked},
		{StashOptions{Staged: true, IncludeIgnored: true}, ErrStashStagedUntracked},
		{StashOptions{Paths: []string{":(bogus)a"}}, ErrInvalidPath},
		{StashOptions{Paths: []string{"missing", "a"}}, ErrPathspecNoMatch},
	} {
		if _, err := StashPush(t.Context(), tr.repo, tc.opts); !errors.Is(err, tc.want) {
			t.Fatalf("StashPush(%+v) returned %v, want %v", tc.opts, err, tc.want)
		}
	}
	if len(tr.stashes()) != 0 || tr.readFile("a") != "a2\n" {
		t.Fatal("a refused push changed the repository")
	}
}

func TestStashPushLeavesNestedRepositoriesAlone(t *testing.T) {
	tr := newTestRepo(t)
	stashChanges(tr)
	tr.writeFile("vendor/lib/.git/HEAD", "ref: refs/heads/main\n")
	tr.writeFile("vendor/lib/code.go", "package lib\n")
	tr.writeFile("loose/u.txt", "u\n")

	id := tr.stash(StashOptions{IncludeUntracked: true})

	stash, err := tr.db().Commit(id)
	if err != nil || len(stash.Parents) != 3 {
		t.Fatalf("stash commit = %+v, %v", stash, err)
	}
	stashed, err := commitTreeEntries(tr.db(), stash.Parents[2])
	if err != nil {
		t.Fatalf("commitTreeEntries returned error %v", err)
	}
	if _, ok := stashed["loose/u.txt"]; !ok || len(stashed) != 1 {
		t.Fatalf("untracked tree = %v", stashed)
	}
	if !tr.exists("vendor/lib/code.go") || tr.exists("loose") {
		t.Fatal("the push removed a nested repository or kept an untracked directory")
	}
}

func TestStashPushReportsInvalidPathRulesForUntrackedFiles(t *testing.T) {
	tr := newTestRepo(t)
	tr.commitFiles("base", map[string]string{"a": "a\n"})
	tr.writeFile("u", "u\n")
	tr.appendConfig("[core]\n\tprotectntfs = maybe\n")
	tr.repo = tr.reopen()
	if _, err := StashPush(t.Context(), tr.repo, StashOptions{IncludeUntracked: true, When: mergeTime}); err == nil {
		t.Fatal("StashPush accepted an invalid core.protectntfs")
	}
}

func TestStashShowWithoutUntrackedFilesAndWithABadDiffAlgorithm(t *testing.T) {
	tr := newTestRepo(t)
	stashChanges(tr)
	tr.stash(StashOptions{})

	changes, err := StashShow(t.Context(), tr.repo, 0, diff.Options{})
	if err != nil || changes.Untracked != nil || len(changes.WorkTree) == 0 || len(changes.Index) == 0 {
		t.Fatalf("StashShow = %+v, %v", changes, err)
	}
	if _, err := StashShow(t.Context(), tr.repo, 4, diff.Options{}); !errors.Is(err, ErrStashNotFound) {
		t.Fatalf("StashShow of a missing entry returned %v", err)
	}

	tr.appendConfig("[diff]\n\talgorithm = sideways\n")
	tr.repo = tr.reopen()
	if _, err := StashShow(t.Context(), tr.repo, 0, diff.Options{}); err == nil {
		t.Fatal("StashShow accepted an unknown diff algorithm")
	}
	bare := newBareTestRepo(t)
	bare.appendConfig("[user]\n\tname = ann\n")
	if _, err := StashShow(t.Context(), bare.repo, 0, diff.Options{}); !errors.Is(err, ErrStashNotFound) {
		t.Fatalf("StashShow in a repository without stashes returned %v", err)
	}
}

func TestStashApplyIndexRefusesAnIndexThatDoesNotFit(t *testing.T) {
	binary := func(tag string) string { return "bin\x00" + tag + "\n" + tenLines("x") }
	for _, tc := range []struct {
		name  string
		build func(tr *testRepo)
	}{
		{"a staged file that is staged again", func(tr *testRepo) {
			tr.commitFiles("base", map[string]string{"a": "a\n"})
			tr.writeFile("n", "stash\n")
			tr.stageAll("n")
			tr.stash(StashOptions{})
			tr.writeFile("n", "other\n")
			tr.stageAll("n")
		}},
		{"a staged edit of a file no longer in the index", func(tr *testRepo) {
			tr.commitFiles("base", map[string]string{"a": "a\n", "b": "b\n"})
			tr.writeFile("b", "b2\n")
			tr.stageAll("b")
			tr.stash(StashOptions{})
			tr.dropFromIndex("b")
		}},
		{"a staged deletion of a file changed since", func(tr *testRepo) {
			tr.commitFiles("base", map[string]string{"a": "a\n", "c": "c\n"})
			tr.remove("c")
			tr.stageAll("c")
			tr.stash(StashOptions{})
			tr.commitFiles("change", map[string]string{"c": "changed\n"})
		}},
		{"a staged edit over a symbolic link", func(tr *testRepo) {
			tr.commitFiles("base", map[string]string{"x": tenLines("x")})
			tr.writeFile("x", changeLine(tenLines("x"), 5, "STAGED"))
			tr.stageAll("x")
			tr.stash(StashOptions{})
			tr.commitFiles("change", map[string]string{"x": changeLine(tenLines("x"), 0, "HEAD")})
			tr.setIndexMode("x", object.ModeSymlink, "")
		}},
		{"a staged link that replaced a file changed since", func(tr *testRepo) {
			tr.commitFiles("base", map[string]string{"a": "a\n", "x": tenLines("x")})
			tr.setIndexMode("x", object.ModeSymlink, "target")
			tr.writeFile("a", "a2\n")
			tr.stash(StashOptions{})
			tr.commitFiles("change", map[string]string{"x": changeLine(tenLines("x"), 0, "HEAD")})
		}},
		{"a staged edit of binary content", func(tr *testRepo) {
			tr.commitFiles("base", map[string]string{"bin": binary("base")})
			tr.writeFile("bin", binary("staged"))
			tr.stageAll("bin")
			tr.stash(StashOptions{})
			tr.commitFiles("change", map[string]string{"bin": binary("head")})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tr := newTestRepo(t)
			tc.build(tr)
			before := tr.index()
			result, err := StashApply(t.Context(), tr.repo, 0, StashApplyOptions{Index: true})
			if !errors.Is(err, ErrStashIndexConflicts) || result.IndexRestored {
				t.Fatalf("StashApply = %+v, %v", result, err)
			}
			if after := tr.index(); after.Len() != before.Len() {
				t.Fatal("a refused apply changed the index")
			}
		})
	}
}

func TestStashApplyIndexCarriesAStagedModeChange(t *testing.T) {
	tr := newTestRepo(t)
	tr.commitFiles("base", map[string]string{"x": "x\n", "a": "a\n"})
	tr.setIndexMode("x", object.ModeExecutable, "")
	tr.writeFile("a", "a2\n")
	tr.stash(StashOptions{})
	tr.commitFiles("other", map[string]string{"z": "z\n"})

	result, err := StashApply(t.Context(), tr.repo, 0, StashApplyOptions{Index: true})

	if err != nil || !result.IndexRestored {
		t.Fatalf("StashApply = %+v, %v", result, err)
	}
	if entry, ok := entryOf(t, tr.index(), "x"); !ok || entry.Mode != object.ModeExecutable {
		t.Fatalf("x index entry = %+v", entry)
	}
}

func TestStashPushStagedRefusesWorkTreesItCannotReverse(t *testing.T) {
	binary := func(tag string) string { return "bin\x00" + tag + "\n" + tenLines("x") }
	for _, tc := range []struct {
		name  string
		build func(tr *testRepo)
	}{
		{"a staged deletion recreated on disk", func(tr *testRepo) {
			tr.commitFiles("base", map[string]string{"a": "a\n", "c": "c\n"})
			tr.remove("c")
			tr.stageAll("c")
			tr.writeFile("c", "again\n")
		}},
		{"a staged edit deleted on disk", func(tr *testRepo) {
			tr.commitFiles("base", map[string]string{"a": "a\n", "b": "b\n"})
			tr.writeFile("b", "b2\n")
			tr.stageAll("b")
			tr.remove("b")
		}},
		{"a staged new file edited on disk", func(tr *testRepo) {
			tr.commitFiles("base", map[string]string{"a": "a\n"})
			tr.writeFile("n", "n\n")
			tr.stageAll("n")
			tr.writeFile("n", "other\n")
		}},
		{"a staged symbolic link edited on disk", func(tr *testRepo) {
			tr.commitFiles("base", map[string]string{"x": tenLines("x")})
			tr.setIndexMode("x", object.ModeSymlink, "target\n")
			tr.writeFile("x", "elsewhere\n")
			tr.appendConfig("[core]\n\tsymlinks = false\n")
			tr.repo = tr.reopen()
		}},
		{"a staged binary edit changed on disk", func(tr *testRepo) {
			tr.commitFiles("base", map[string]string{"bin": binary("base")})
			tr.writeFile("bin", binary("staged"))
			tr.stageAll("bin")
			tr.writeFile("bin", binary("later"))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tr := newTestRepo(t)
			tc.build(tr)
			before := tr.index()
			_, err := StashPush(t.Context(), tr.repo, StashOptions{Staged: true, When: mergeTime})
			if !errors.Is(err, ErrStashWorktreeKept) || len(tr.stashes()) != 1 {
				t.Fatalf("StashPush returned %v with %d stashes", err, len(tr.stashes()))
			}
			if after := tr.index(); after.Len() != before.Len() {
				t.Fatal("a refused staged push reset the index")
			}
		})
	}
}

func TestStashPushStagedRefusesALinkThatIsAFileOnDisk(t *testing.T) {
	tr := newTestRepo(t)
	tr.commitFiles("base", map[string]string{"a": "a\n", "link": "a"})
	tr.setIndexMode("link", object.ModeSymlink, "b")
	tr.writeFile("link", "b")
	tr.appendConfig("[core]\n\tsymlinks = true\n")
	tr.repo = tr.reopen()

	_, err := StashPush(t.Context(), tr.repo, StashOptions{Staged: true, When: mergeTime})

	if !errors.Is(err, ErrStashWorktreeKept) || tr.readFile("link") != "b" {
		t.Fatalf("StashPush returned %v", err)
	}
}

func TestStashBinaryChecksReportAnUnknownDiffAlgorithm(t *testing.T) {
	staged := newTestRepo(t)
	staged.commitFiles("base", map[string]string{"a": "a\n"})
	staged.writeFile("a", "a2\n")
	staged.stageAll("a")
	staged.appendConfig("[diff]\n\talgorithm = sideways\n")
	staged.repo = staged.reopen()
	if _, err := StashPush(t.Context(), staged.repo, StashOptions{Staged: true, When: mergeTime}); err == nil {
		t.Fatal("a staged push accepted an unknown diff algorithm")
	}

	index := newTestRepo(t)
	index.commitFiles("base", map[string]string{"m": tenLines("m")})
	index.writeFile("m", changeLine(tenLines("m"), 5, "STAGED"))
	index.stageAll("m")
	index.stash(StashOptions{})
	index.commitFiles("shift", map[string]string{"m": "top\n" + tenLines("m")})
	index.appendConfig("[diff]\n\talgorithm = sideways\n")
	index.repo = index.reopen()
	if _, err := StashApply(t.Context(), index.repo, 0, StashApplyOptions{Index: true}); err == nil || errors.Is(err, ErrStashIndexConflicts) {
		t.Fatalf("applying the index with an unknown diff algorithm returned %v", err)
	}
}

func TestStashPushStagedKeepsTheContentOfAModeOnlyChange(t *testing.T) {
	tr := newTestRepo(t)
	tr.commitFiles("base", map[string]string{"x": "x\n", "a": "a\n"})
	tr.setIndexMode("x", object.ModeExecutable, "")

	tr.stash(StashOptions{Staged: true})

	if entry, ok := entryOf(t, tr.index(), "x"); !ok || entry.Mode != object.ModeBlob || tr.readFile("x") != "x\n" {
		t.Fatalf("x = %+v, %q", entry, tr.readFile("x"))
	}
}

func TestStashHunksFindShiftedAndAnchoredImages(t *testing.T) {
	hunks := func(oldText, newText string) []diff.Hunk {
		return diff.Blobs([]byte(oldText), []byte(newText), diff.Options{Context: 1})
	}
	for _, tc := range []struct {
		name    string
		patch   []diff.Hunk
		target  string
		want    string
		refused bool
	}{
		{"insert into an empty file", hunks("", "one\n"), "", "one\n", false},
		{"a last line without a newline", hunks("a\nb", "a\nB"), "a\nb", "a\nB", false},
		{"a hunk moved up", hunks("x1\nx2\nx3\nx4\na\nb\nc\ny\n", "x1\nx2\nx3\nx4\na\nB\nc\ny\n"), "a\nb\nc\ny\n", "a\nB\nc\ny\n", false},
		{"a hunk moved down", hunks("a\nb\nc\nd\ne\nf\n", "a\nb\nc\nD\ne\nf\n"), "0\na\nb\nc\nd\ne\nf\n", "0\na\nb\nc\nD\ne\nf\n", false},
		{"a first-line hunk below new lines", hunks("a\nb\nc\n", "A\nb\nc\n"), "top\na\nb\nc\n", "", true},
		{"a last-line hunk above new lines", hunks("a\nb\nc\n", "a\nb\nC\n"), "a\nb\nc\ntail\n", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := applyHunks([]byte(tc.target), tc.patch)
			if tc.refused {
				if !errors.Is(err, diff.ErrApply) {
					t.Fatalf("applyHunks = %q, %v", got, err)
				}
				return
			}
			if err != nil || string(got) != tc.want {
				t.Fatalf("applyHunks = %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}

func TestStashErrorsNameTheirPaths(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want []string
	}{
		{&PathspecError{Specs: []string{"a", "b"}}, []string{ErrPathspecNoMatch.Error(), "a, b"}},
		{&UntrackedRestoreError{Existing: []string{"u", "v"}}, []string{ErrUntrackedNotRestored.Error(), "u, v"}},
		{&UntrackedRestoreError{Existing: []string{"u"}, Blocked: "dir"}, []string{ErrUntrackedNotRestored.Error(), "dir"}},
	} {
		message := tc.err.Error()
		if !slices.ContainsFunc(tc.want, func(part string) bool { return !strings.Contains(message, part) }) {
			continue
		}
		t.Fatalf("%T message %q misses %q", tc.err, message, tc.want)
	}
}
