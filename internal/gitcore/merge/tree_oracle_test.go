//go:build oracle

package merge

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/revision"
)

const deleted = "\x00deleted"

type side map[string]string

type treeCase struct {
	name       string
	base       side
	ours       side
	theirs     side
	executable map[string]string
	renames    bool
	moves      map[string][2]string
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+filepath.Join(dir, ".no-global"),
		"GIT_AUTHOR_NAME=oracle", "GIT_AUTHOR_EMAIL=oracle@example.com",
		"GIT_COMMITTER_NAME=oracle", "GIT_COMMITTER_EMAIL=oracle@example.com",
		"GIT_AUTHOR_DATE=1700000000 +0000", "GIT_COMMITTER_DATE=1700000000 +0000",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if _, isExit := err.(*exec.ExitError); !isExit || !slices.Contains(args, "merge-tree") {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return string(out)
}

func apply(t *testing.T, dir string, files side) {
	t.Helper()
	for name, content := range files {
		full := filepath.Join(dir, filepath.FromSlash(name))
		if content == deleted {
			runGit(t, dir, "rm", "-q", "-r", "--", name)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		runGit(t, dir, "add", "--", name)
	}
}

func move(t *testing.T, dir string, moves map[string][2]string, side int) {
	t.Helper()
	for from, to := range moves {
		if to[side] == "" {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, filepath.FromSlash(to[side]))), 0o755); err != nil {
			t.Fatal(err)
		}
		runGit(t, dir, "mv", "--", from, to[side])
	}
}

func buildBranches(t *testing.T, c treeCase) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "-q", "-b", "main")
	runGit(t, dir, "config", "core.autocrlf", "false")
	apply(t, dir, c.base)
	runGit(t, dir, "commit", "-q", "--allow-empty", "-m", "base")
	runGit(t, dir, "branch", "theirs")
	runGit(t, dir, "checkout", "-q", "-b", "ours")
	move(t, dir, c.moves, 0)
	apply(t, dir, c.ours)
	for name, who := range c.executable {
		if who == "ours" {
			runGit(t, dir, "update-index", "--chmod=+x", "--", name)
		}
	}
	runGit(t, dir, "commit", "-q", "--allow-empty", "-m", "ours")
	runGit(t, dir, "checkout", "-q", "theirs")
	move(t, dir, c.moves, 1)
	apply(t, dir, c.theirs)
	for name, who := range c.executable {
		if who == "theirs" {
			runGit(t, dir, "update-index", "--chmod=+x", "--", name)
		}
	}
	runGit(t, dir, "commit", "-q", "--allow-empty", "-m", "theirs")
	return dir
}

func gitMergeTree(t *testing.T, dir string, renames bool) (string, []string) {
	t.Helper()
	args := []string{"-c", "merge.directoryRenames=false", "merge-tree", "--write-tree", "-X", "no-renames", "ours", "theirs"}
	if renames {
		args = []string{"-c", "merge.directoryRenames=false", "merge-tree", "--write-tree", "ours", "theirs"}
	}
	out := runGit(t, dir, args...)
	lines := strings.Split(strings.ReplaceAll(out, "\r\n", "\n"), "\n")
	var stages []string
	for _, line := range lines[1:] {
		if line == "" {
			break
		}
		stages = append(stages, line)
	}
	slices.Sort(stages)
	return strings.TrimSpace(lines[0]), stages
}

func stagesOf(conflicts []Conflict) []string {
	var stages []string
	for _, c := range conflicts {
		for stage, entry := range []*Entry{c.Base, c.Ours, c.Theirs} {
			if entry == nil {
				continue
			}
			line := fmt.Sprintf("%06s %s %d\t%s", entry.Mode, entry.ID, stage+1, c.Path)
			if !slices.Contains(stages, line) {
				stages = append(stages, line)
			}
		}
	}
	slices.Sort(stages)
	return stages
}

func ourMergeTree(t *testing.T, dir string, renames bool) (string, []string) {
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
	snapshot := func(commit hash.ObjectID) Snapshot {
		c, err := db.Commit(commit)
		if err != nil {
			t.Fatal(err)
		}
		s, err := Read(db, c.Tree)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	opts := TreeOptions{File: Options{Labels: Labels{Ours: "ours", Theirs: "theirs"}}}
	if renames {
		treeOf := func(commit hash.ObjectID) hash.ObjectID {
			c, err := db.Commit(commit)
			if err != nil {
				t.Fatal(err)
			}
			return c.Tree
		}
		if opts.OurRenames, err = DetectRenames(t.Context(), db, treeOf(bases[0]), treeOf(ours)); err != nil {
			t.Fatal(err)
		}
		if opts.TheirRenames, err = DetectRenames(t.Context(), db, treeOf(bases[0]), treeOf(theirs)); err != nil {
			t.Fatal(err)
		}
	}
	result, err := Trees(snapshot(bases[0]), snapshot(ours), snapshot(theirs), db, opts)
	if err != nil {
		t.Fatal(err)
	}
	id, err := result.Tree.Write(db)
	if err != nil {
		t.Fatal(err)
	}
	return id.String(), stagesOf(result.Conflicts)
}

func treeCases() []treeCase {
	return []treeCase{
		{name: "different files", base: side{"a": text("a"), "b": text("b")}, ours: side{"a": text("A")}, theirs: side{"b": text("B")}},
		{name: "one file, far apart", base: side{"f": text("1", "2", "3", "4", "5", "6", "7")}, ours: side{"f": text("ONE", "2", "3", "4", "5", "6", "7")}, theirs: side{"f": text("1", "2", "3", "4", "5", "6", "SEVEN")}},
		{name: "one file, same line", base: side{"f": text("a", "b", "c")}, ours: side{"f": text("a", "OURS", "c")}, theirs: side{"f": text("a", "THEIRS", "c")}},
		{name: "same change", base: side{"f": text("a")}, ours: side{"f": text("b")}, theirs: side{"f": text("b")}},
		{name: "added alike", base: side{"keep": text("k")}, ours: side{"new": text("same")}, theirs: side{"new": text("same")}},
		{name: "added differently", base: side{"keep": text("k")}, ours: side{"new": text("ours")}, theirs: side{"new": text("theirs")}},
		{name: "modify against delete", base: side{"f": text("a"), "k": text("k")}, ours: side{"f": text("changed")}, theirs: side{"f": deleted}},
		{name: "delete against modify", base: side{"f": text("a"), "k": text("k")}, ours: side{"f": deleted}, theirs: side{"f": text("changed")}},
		{name: "both deleted", base: side{"f": text("a"), "k": text("k")}, ours: side{"f": deleted}, theirs: side{"f": deleted}},
		{name: "one side adds", base: side{"k": text("k")}, ours: side{}, theirs: side{"dir/new": text("n")}},
		{name: "nested edits", base: side{"a/b/c": text("1", "2", "3", "4", "5", "6"), "a/x": text("x")}, ours: side{"a/b/c": text("1", "2", "3", "4", "5", "SIX")}, theirs: side{"a/b/c": text("ONE", "2", "3", "4", "5", "6"), "a/y": text("y")}},
		{name: "mode on one side, content on the other", base: side{"run.sh": text("echo 1")}, ours: side{"run.sh": text("echo 1")}, theirs: side{"run.sh": text("echo 2")}, executable: map[string]string{"run.sh": "ours"}},
		{name: "mode on both sides", base: side{"run.sh": text("echo 1")}, ours: side{"run.sh": text("echo 1")}, theirs: side{"run.sh": text("echo 1")}, executable: map[string]string{"run.sh": "ours"}},
		{name: "binary", base: side{"bin": "a\x00b"}, ours: side{"bin": "a\x00c"}, theirs: side{"bin": "a\x00d"}},
		{name: "missing newline", base: side{"f": "a\nb"}, ours: side{"f": "a\nOURS"}, theirs: side{"f": "a\nTHEIRS"}},
		{name: "file in the way of a directory", base: side{"k": text("k")}, ours: side{"d": text("file")}, theirs: side{"d/x": text("inside")}},
		{name: "directory in the way of a file", base: side{"k": text("k")}, ours: side{"d/x": text("inside")}, theirs: side{"d": text("file")}},
		{name: "everything at once", base: side{"a": text("a"), "b": text("1", "2", "3"), "c": text("c"), "d/e": text("e")}, ours: side{"a": text("A"), "b": text("1", "X", "3"), "c": deleted}, theirs: side{"b": text("1", "Y", "3"), "d/e": text("E"), "f": text("f")}},
		{name: "rename only", renames: true, base: side{"a": long("a")}, moves: map[string][2]string{"a": {"b", ""}}},
		{name: "rename against an edit far away", renames: true, base: side{"a": long("a")}, moves: map[string][2]string{"a": {"b", ""}}, theirs: side{"a": edited(long("a"), 9, "THEIRS")}},
		{name: "rename against a conflicting edit", renames: true, base: side{"a": long("a")}, moves: map[string][2]string{"a": {"b", ""}}, ours: side{"b": edited(long("a"), 2, "OURS")}, theirs: side{"a": edited(long("a"), 2, "THEIRS")}},
		{name: "their rename against our edit", renames: true, base: side{"a": long("a")}, moves: map[string][2]string{"a": {"", "dir/b"}}, ours: side{"a": edited(long("a"), 0, "OURS")}},
		{name: "both rename alike", renames: true, base: side{"a": long("a")}, moves: map[string][2]string{"a": {"b", "b"}}, ours: side{"b": edited(long("a"), 0, "OURS")}, theirs: side{"b": edited(long("a"), 9, "THEIRS")}},
		{name: "renamed apart", renames: true, base: side{"a": long("a"), "k": text("k")}, moves: map[string][2]string{"a": {"b", "c"}}},
		{name: "rename against a delete", renames: true, base: side{"a": long("a"), "k": text("k")}, moves: map[string][2]string{"a": {"b", ""}}, theirs: side{"a": deleted}},
		{name: "rename onto a path they added", renames: true, base: side{"a": long("a")}, moves: map[string][2]string{"a": {"b", ""}}, theirs: side{"b": long("b")}},
		{name: "renamed apart with edits", renames: true, base: side{"a": long("a")}, moves: map[string][2]string{"a": {"b", "c"}}, ours: side{"b": edited(long("a"), 0, "OURS")}, theirs: side{"c": edited(long("a"), 0, "THEIRS")}},
		{name: "two renames at once", renames: true, base: side{"a": long("a"), "x": long("x")}, moves: map[string][2]string{"a": {"b", ""}, "x": {"", "y"}}, ours: side{"x": edited(long("x"), 5, "OURS")}, theirs: side{"a": edited(long("a"), 5, "THEIRS")}},
		{name: "delete against a rename", renames: true, base: side{"a": long("a"), "k": text("k")}, moves: map[string][2]string{"a": {"", "b"}}, ours: side{"a": deleted}},
	}
}

func text(lines ...string) string { return strings.Join(lines, "\n") + "\n" }

func long(seed string) string {
	var lines []string
	for i := range 10 {
		lines = append(lines, seed+" line "+string(rune('0'+i))+" with enough text to be recognised")
	}
	return text(lines...)
}

func edited(content string, line int, replacement string) string {
	lines := strings.Split(strings.TrimSuffix(content, "\n"), "\n")
	lines[line] = replacement
	return text(lines...)
}

func TestOurTreeMergeIsTheOneGitWrites(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git is not available: %v", err)
	}
	for _, c := range treeCases() {
		t.Run(c.name, func(t *testing.T) {
			dir := buildBranches(t, c)
			wantTree, wantConflicts := gitMergeTree(t, dir, c.renames)
			gotTree, gotConflicts := ourMergeTree(t, dir, c.renames)
			if gotTree != wantTree {
				t.Errorf("tree = %s, git wrote %s\n%s", gotTree, wantTree, runGit(t, dir, "ls-tree", "-r", wantTree))
			}
			if !slices.Equal(gotConflicts, wantConflicts) {
				t.Errorf("conflicts = %v, git reports %v", gotConflicts, wantConflicts)
			}
		})
	}
}
