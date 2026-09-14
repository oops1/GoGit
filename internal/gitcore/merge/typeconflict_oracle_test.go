//go:build oracle

package merge

import (
	"os"
	"path/filepath"
	"testing"
)

const (
	gitlinkPrefix = "\x00gitlink "
	symlinkPrefix = "\x00symlink "
)

func gitlink(id string) string { return gitlinkPrefix + id }

func symlinkTo(target string) string { return symlinkPrefix + target }

func replaceIndexEntry(t *testing.T, dir, name, mode, id string) {
	t.Helper()
	runGit(t, dir, "rm", "-q", "-r", "--cached", "--ignore-unmatch", "--", name)
	if err := os.RemoveAll(filepath.Join(dir, filepath.FromSlash(name))); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "update-index", "--add", "--cacheinfo", mode+","+id+","+name)
}

func typeConflictCases() []treeCase {
	first := "1111111111111111111111111111111111111111"
	second := "2222222222222222222222222222222222222222"
	third := "3333333333333333333333333333333333333333"
	return []treeCase{
		{name: "file against a submodule", base: side{"k": text("k")}, ours: side{"p": text("file")}, theirs: side{"p": gitlink(first)}},
		{name: "submodule against a file", base: side{"k": text("k")}, ours: side{"p": gitlink(first)}, theirs: side{"p": text("file")}},
		{name: "edited file against a submodule", base: side{"q": text("base")}, ours: side{"q": text("edit")}, theirs: side{"q": gitlink(first)}},
		{name: "submodule against an edited file", base: side{"q": text("base")}, ours: side{"q": gitlink(first)}, theirs: side{"q": text("edit")}},
		{name: "moved submodule against a file", base: side{"g": gitlink(first)}, ours: side{"g": gitlink(second)}, theirs: side{"g": text("file")}},
		{name: "submodule against a link", base: side{"k": text("k")}, ours: side{"s": gitlink(first)}, theirs: side{"s": symlinkTo("target")}},
		{name: "file against a link", base: side{"k": text("k")}, ours: side{"r": text("file")}, theirs: side{"r": symlinkTo("target")}},
		{name: "link against a file", base: side{"r": text("base")}, ours: side{"r": symlinkTo("target")}, theirs: side{"r": text("edit")}},
		{name: "submodules moved apart", base: side{"g": gitlink(first)}, ours: side{"g": gitlink(second)}, theirs: side{"g": gitlink(third)}},
		{name: "submodule moved against its deletion", base: side{"g": gitlink(first), "k": text("k")}, ours: side{"g": gitlink(second)}, theirs: side{"g": deleted}},
		{name: "aside name already taken", base: side{"k": text("k"), "p~ours": text("taken")}, ours: side{"p": text("file")}, theirs: side{"p": gitlink(first)}},
		{name: "aside name is a directory", base: side{"k": text("k"), "p~ours/x": text("taken")}, ours: side{"p": text("file")}, theirs: side{"p": gitlink(first)}},
	}
}

func TestOurTypeConflictMergeIsTheOneGitWrites(t *testing.T) {
	for _, c := range typeConflictCases() {
		t.Run(c.name, func(t *testing.T) {
			dir := buildBranches(t, c)
			wantTree, wantConflicts := gitMergeTree(t, dir, false)
			gotTree, gotConflicts := ourMergeTree(t, dir, false)
			if gotTree != wantTree {
				t.Errorf("tree = %s, git wrote %s\n%s", gotTree, wantTree, runGit(t, dir, "ls-tree", "-r", wantTree))
			}
			if !equalLines(gotConflicts, wantConflicts) {
				t.Errorf("conflicts = %v, git reports %v", gotConflicts, wantConflicts)
			}
		})
	}
}

func equalLines(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
