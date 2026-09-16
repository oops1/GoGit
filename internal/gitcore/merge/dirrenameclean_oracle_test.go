//go:build oracle

package merge

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/revision"
)

func gitDirectoryRenameMessage(w Warning) string {
	switch w.Kind {
	case WarningDirectoryRenameSplit:
		return fmt.Sprintf("CONFLICT (directory rename split): Unclear where to rename %s to; it was renamed to multiple other directories, with no destination getting a majority of the files.", w.Path)
	case WarningDirectoryRenameCollision:
		return fmt.Sprintf("CONFLICT (implicit dir rename): Cannot map more than one path to %s; implicit directory renames tried to put these paths there: %s", w.Path, w.Sources)
	}
	return fmt.Sprintf("CONFLICT (implicit dir rename): Existing file/dir at %s in the way of implicit directory rename(s) putting the following path(s) there: %s.", w.Path, w.Sources)
}

func ourUncleanTreeMerge(t *testing.T, dir, setting string) (string, []string, TreeResult) {
	t.Helper()
	db, err := odb.Open(filepath.Join(dir, ".git", "objects"), odb.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	commitOf := func(name string) hash.ObjectID {
		id, err := hash.Parse(strings.TrimSpace(runGit(t, dir, "rev-parse", name)))
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	ours, theirs := commitOf("ours"), commitOf("theirs")
	bases, err := revision.MergeBase(revision.Context{Objects: db}, ours, theirs)
	if err != nil || len(bases) != 1 {
		t.Fatalf("merge base = %v, %v", bases, err)
	}
	trees := make([]hash.ObjectID, 3)
	snapshots := make([]Snapshot, 3)
	for at, commit := range []hash.ObjectID{bases[0], ours, theirs} {
		c, err := db.Commit(commit)
		if err != nil {
			t.Fatal(err)
		}
		trees[at] = c.Tree
		if snapshots[at], err = Read(db, c.Tree); err != nil {
			t.Fatal(err)
		}
	}
	modes := map[string]DirectoryRenames{"false": DirectoryRenamesOff, "true": DirectoryRenamesApply, "conflict": DirectoryRenamesConflict}
	opts := TreeOptions{File: Options{Labels: Labels{Ours: "ours", Theirs: "theirs"}}, DirectoryRenames: modes[setting]}
	detected, err := DetectSideRenames(t.Context(), db, trees[0], trees[1], trees[2], RenameOptions{Limit: DefaultRenameLimit, DirectoryRenames: setting != "false"})
	if err != nil {
		t.Fatal(err)
	}
	opts.OurRenames, opts.TheirRenames = detected.Ours, detected.Theirs
	result, err := Trees(snapshots[0], snapshots[1], snapshots[2], db, opts)
	if err != nil {
		t.Fatal(err)
	}
	id, err := result.Tree.Write(db)
	if err != nil {
		t.Fatal(err)
	}
	return id.String(), stagesOf(result.Conflicts), result
}

func TestUncleanDirectoryRenamesMatchGitMergeTree(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git is not available: %v", err)
	}
	for _, c := range []treeCase{
		{name: "split", renames: true, base: side{"lib/a.go": long("a"), "lib/b.go": long("b"), "k": text("k")}, moves: map[string][2]string{"lib/a.go": {"x/a.go", ""}, "lib/b.go": {"y/b.go", ""}}, theirs: side{"lib/new.go": text("new")}},
		{name: "collision", renames: true, base: side{"lib/a.go": long("a"), "lib2/c.go": long("c"), "k": text("k")}, moves: map[string][2]string{"lib/a.go": {"src/a.go", ""}, "lib2/c.go": {"src/c.go", ""}}, theirs: side{"lib/new.go": text("new"), "lib2/new.go": text("other")}},
		{name: "in the way", renames: true, base: side{"lib/a.go": long("a"), "lib/b.go": long("b"), "k": text("k")}, moves: map[string][2]string{"lib/a.go": {"src/a.go", ""}, "lib/b.go": {"src/b.go", ""}}, theirs: side{"lib/new.go": text("new"), "src/new.go": text("other")}},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := buildBranches(t, c)
			for _, setting := range []string{"conflict", "true", "false"} {
				cmd := exec.Command("git", "-c", "merge.directoryRenames="+setting, "merge-tree", "--write-tree", "--messages", "ours", "theirs")
				cmd.Dir = dir
				out, err := cmd.Output()
				var exit *exec.ExitError
				if err != nil && !errors.As(err, &exit) {
					t.Fatal(err)
				}
				lines := strings.Split(strings.ReplaceAll(string(out), "\r\n", "\n"), "\n")
				var wantStages, wantMessages []string
				at := 1
				for ; at < len(lines) && lines[at] != ""; at++ {
					wantStages = append(wantStages, lines[at])
				}
				for _, line := range lines[at:] {
					if strings.HasPrefix(line, "CONFLICT (directory rename split)") || strings.HasPrefix(line, "CONFLICT (implicit dir rename)") {
						wantMessages = append(wantMessages, line)
					}
				}
				slices.Sort(wantStages)
				gotTree, gotStages, result := ourUncleanTreeMerge(t, dir, setting)
				var gotMessages []string
				for _, w := range result.Warnings {
					if w.Unclean() {
						gotMessages = append(gotMessages, gitDirectoryRenameMessage(w))
					}
				}
				if gotTree != strings.TrimSpace(lines[0]) || !slices.Equal(gotStages, wantStages) {
					t.Errorf("%s: tree %s stages %v, git %s %v", setting, gotTree, gotStages, lines[0], wantStages)
				}
				if result.Clean() != (err == nil) || !slices.Equal(gotMessages, wantMessages) {
					t.Errorf("%s: clean %v messages %q, git clean %v messages %q", setting, result.Clean(), gotMessages, err == nil, wantMessages)
				}
			}
		})
	}
}
