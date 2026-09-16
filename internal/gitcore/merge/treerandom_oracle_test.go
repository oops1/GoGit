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
)

func commitRandomSide(t *testing.T, dir, message string, files map[string][]byte) {
	t.Helper()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-q", "--allow-empty", "-m", message)
}

func randomTreeRepository(t *testing.T, cases []mergeCase) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "-q", "-b", "main")
	runGit(t, dir, "config", "core.autocrlf", "false")
	side := func(pick func(mergeCase) []byte) map[string][]byte {
		files := map[string][]byte{}
		for at, c := range cases {
			files[fmt.Sprintf("f%03d", at)] = pick(c)
		}
		return files
	}
	commitRandomSide(t, dir, "base", side(func(c mergeCase) []byte { return c.base }))
	runGit(t, dir, "branch", "theirs")
	runGit(t, dir, "checkout", "-q", "-b", "ours")
	commitRandomSide(t, dir, "ours", side(func(c mergeCase) []byte { return c.ours }))
	runGit(t, dir, "checkout", "-q", "theirs")
	commitRandomSide(t, dir, "theirs", side(func(c mergeCase) []byte { return c.theirs }))
	return dir
}

func ourRandomTreeMerge(t *testing.T, dir string, style Style) (string, []string) {
	t.Helper()
	db, err := odb.Open(filepath.Join(dir, ".git", "objects"), odb.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	snapshot := func(name string) (Snapshot, hash.ObjectID) {
		id, err := hash.Parse(strings.TrimSpace(runGit(t, dir, "rev-parse", name)))
		if err != nil {
			t.Fatal(err)
		}
		c, err := db.Commit(id)
		if err != nil {
			t.Fatal(err)
		}
		s, err := Read(db, c.Tree)
		if err != nil {
			t.Fatal(err)
		}
		return s, id
	}
	base, baseID := snapshot("main")
	ours, _ := snapshot("ours")
	theirs, _ := snapshot("theirs")
	result, err := Trees(base, ours, theirs, db, TreeOptions{File: Options{
		Style:  style,
		Labels: Labels{Ours: "ours", Theirs: "theirs", Base: baseID.String()[:7]},
	}})
	if err != nil {
		t.Fatal(err)
	}
	id, err := result.Tree.Write(db)
	if err != nil {
		t.Fatal(err)
	}
	return id.String(), stagesOf(result.Conflicts)
}

func TestRandomTreeMergesMatchGitMergeTree(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git is not available: %v", err)
	}
	cases := randomMergeCases(31337, 80)
	dir := randomTreeRepository(t, cases)
	for style, name := range map[Style]string{StyleMerge: "merge", StyleDiff3: "diff3", StyleZDiff3: "zdiff3"} {
		out := runGit(t, dir, "-c", "merge.conflictStyle="+name, "merge-tree", "--write-tree", "ours", "theirs")
		lines := strings.Split(strings.ReplaceAll(out, "\r\n", "\n"), "\n")
		var wantStages []string
		for _, line := range lines[1:] {
			if line == "" {
				break
			}
			wantStages = append(wantStages, line)
		}
		slices.Sort(wantStages)
		wantTree := strings.TrimSpace(lines[0])
		gotTree, gotStages := ourRandomTreeMerge(t, dir, style)
		if !slices.Equal(gotStages, wantStages) {
			t.Fatalf("%s: stages = %v, git %v", name, gotStages, wantStages)
		}
		if gotTree == wantTree {
			continue
		}
		for at, c := range cases {
			path := fmt.Sprintf("f%03d", at)
			want := runGit(t, dir, "cat-file", "-p", wantTree+":"+path)
			got := runGit(t, dir, "cat-file", "-p", gotTree+":"+path)
			if got != want {
				t.Fatalf("%s: %s differs\nbase %q\nours %q\ntheirs %q\n got %q\nwant %q", name, path, c.base, c.ours, c.theirs, got, want)
			}
		}
		t.Fatalf("%s: tree = %s, git wrote %s", name, gotTree, wantTree)
	}
}
