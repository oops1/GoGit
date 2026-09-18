//go:build oracle

package commitgraph

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/diff"
	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/odb"
)

func (o *oracle) objectsDir() string { return filepath.Join(o.repo, ".git", "objects") }

func (o *oracle) gitInput(input string, args ...string) string {
	o.t.Helper()
	cmd := exec.CommandContext(o.t.Context(), "git", args...)
	cmd.Dir = o.repo
	cmd.Env = o.env
	cmd.Stdin = strings.NewReader(input)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		o.t.Fatalf("git %s returned error %v: %s", strings.Join(args, " "), err, stderr.String())
	}
	return string(out)
}

func (o *oracle) writeFiles(when int64, message string, files map[string]string) {
	o.t.Helper()
	for name, content := range files {
		full := filepath.Join(o.repo, filepath.FromSlash(name))
		if content == "" {
			if err := os.RemoveAll(full); err != nil {
				o.t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			o.t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			o.t.Fatal(err)
		}
	}
	o.git(when, "add", "-A")
	o.git(when, "commit", "-q", "--allow-empty", "-m", message)
}

func buildPathHistory(o *oracle) {
	o.writeFiles(10, "root", map[string]string{"a.txt": "a\n", "dir/sub/deep.txt": "deep\n", "dir/top.txt": "top\n", "dir/ünïcødé.txt": "u\n", "é": "e\n"})
	o.writeFiles(20, "edit deep", map[string]string{"dir/sub/deep.txt": "deeper\n"})
	o.writeFiles(15, "older clock", map[string]string{"a.txt": "a2\n"})
	o.writeFiles(-1600000000, "far past", map[string]string{"b.txt": "b\n"})
	o.writeFiles(30, "empty", nil)
	o.git(30, "mv", "dir/top.txt", "moved.txt")
	o.git(30, "commit", "-q", "-m", "rename")
	o.writeFiles(40, "file becomes dir", map[string]string{"a.txt": ""})
	o.writeFiles(41, "dir in place", map[string]string{"a.txt/inner": "x\n"})
	o.git(50, "switch", "-q", "-c", "side", "HEAD~3")
	many := map[string]string{}
	for at := range 520 {
		many["bulk/f"+strconv.Itoa(at)+".txt"] = strconv.Itoa(at) + "\n"
	}
	o.writeFiles(2400000000, "far future bulk", many)
	o.writeFiles(60, "after future", map[string]string{"side.txt": "s\n"})
	o.git(70, "switch", "-q", "main")
	o.git(80, "merge", "-q", "--no-edit", "side")
	for i, branch := range []string{"x", "y", "z"} {
		o.git(90, "switch", "-q", "-c", branch, "main")
		o.writeFiles(int64(91+i), branch, map[string]string{branch + "/f.txt": branch + "\n"})
	}
	o.git(95, "switch", "-q", "main")
	o.git(96, "merge", "-q", "--no-edit", "x", "y", "z")
	o.writeFiles(100, "tip", map[string]string{"dir/sub/deep.txt": "tip\n"})
}

func changedPathsOf(t *testing.T, db *odb.DB, commits []Commit) []Commit {
	t.Helper()
	trees := map[hash.ObjectID]hash.ObjectID{}
	for _, c := range commits {
		trees[c.ID] = c.Tree
	}
	opts := diff.Defaults()
	opts.DetectRenames = false
	for at, c := range commits {
		parent := hash.Zero
		if len(c.Parents) > 0 {
			parent = trees[c.Parents[0]]
		}
		files, err := diff.TreeChanges(context.Background(), db, parent, c.Tree, opts)
		if err != nil {
			t.Fatal(err)
		}
		changed := &ChangedPaths{}
		for _, file := range files {
			changed.Paths = append(changed.Paths, file.NewPath)
		}
		commits[at].Changed = changed
	}
	return commits
}

func (o *oracle) openDB() *odb.DB {
	o.t.Helper()
	db, err := odb.Open(o.objectsDir(), odb.Options{})
	if err != nil {
		o.t.Fatal(err)
	}
	o.t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestOracleOurGraphWithCorrectedDatesAndChangedPathsIsTheOneGitWrites(t *testing.T) {
	o := newOracle(t)
	buildPathHistory(o)
	o.git(0, "commit-graph", "write", "--reachable", "--changed-paths")
	gitGraph, err := os.ReadFile(filepath.Join(o.objectsDir(), "info", FileName))
	if err != nil {
		t.Fatal(err)
	}
	commits := changedPathsOf(t, o.openDB(), o.history())
	ours, err := Encode(hash.SHA1, commits, EncodeOptions{ChangedPaths: true})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(ours, gitGraph) {
		t.Fatalf("graphs differ: ours %d bytes, git %d bytes", len(ours), len(gitGraph))
	}
	if chunkBody(t, gitGraph, chunkGenerationOverflow) == nil || !bytes.Contains(chunkBody(t, gitGraph, chunkBloomData), []byte{bloomLargeFilter}) {
		t.Fatal("the history does not exercise overflowing generations and large filters")
	}
	if err := WriteFile(filepath.Join(o.objectsDir(), "info"), hash.SHA1, commits, EncodeOptions{ChangedPaths: true}); err != nil {
		t.Fatal(err)
	}
	o.git(0, "commit-graph", "verify")
}

func checkGraphAgainstObjects(t *testing.T, g *Graph, commits []Commit) {
	t.Helper()
	if g.Len() != len(commits) {
		t.Fatalf("graph holds %d commits, the repository %d", g.Len(), len(commits))
	}
	generations := map[hash.ObjectID]uint64{}
	for _, c := range commits {
		pos, ok := g.Lookup(c.ID)
		if !ok || g.ID(pos) != c.ID {
			t.Fatalf("commit %s is missing from the graph", c.ID)
		}
		entry := g.Entry(pos)
		if entry.Tree != c.Tree || entry.Time != c.Time || len(entry.Parents) != len(c.Parents) {
			t.Fatalf("commit %s reads as %+v", c.ID, entry)
		}
		for at, parent := range entry.Parents {
			if g.ID(parent) != c.Parents[at] {
				t.Fatalf("parent %d of %s reads as %s", at, c.ID, g.ID(parent))
			}
		}
		generations[c.ID] = entry.Generation
	}
	for _, c := range commits {
		for _, parent := range c.Parents {
			if generations[parent] >= generations[c.ID] {
				t.Fatalf("parent %s has generation %d, child %s %d", parent, generations[parent], c.ID, generations[c.ID])
			}
		}
	}
}

func checkFiltersNeverMissAChange(t *testing.T, g *Graph, commits []Commit) {
	t.Helper()
	probes := []string{"a.txt", "a.txt/inner", "dir", "dir/sub", "dir/sub/deep.txt", "dir/top.txt", "moved.txt", "b.txt", "bulk", "bulk/f7.txt", "side.txt", "x", "x/f.txt", "nothing", "dir/nothing"}
	definitelyNot := 0
	for _, c := range commits {
		pos, _ := g.Lookup(c.ID)
		changed := map[string]bool{}
		for _, path := range c.Changed.Paths {
			for path != "" {
				changed[path] = true
				cut := strings.LastIndexByte(path, '/')
				if cut < 0 {
					break
				}
				path = path[:cut]
			}
		}
		for _, probe := range probes {
			maybe := g.MaybeChanged(pos, g.BloomKeys(probe))
			if changed[probe] && !maybe {
				t.Fatalf("the filter of %s misses %s", c.ID, probe)
			}
			if !maybe {
				definitelyNot++
			}
		}
	}
	if definitelyNot == 0 {
		t.Fatal("the filters never ruled out a path")
	}
}

func TestOracleWeReadTheGraphsGitWrites(t *testing.T) {
	for _, tt := range []struct {
		name   string
		layers int
		write  func(o *oracle)
	}{
		{"single file with changed paths", 1, func(o *oracle) {
			o.git(0, "commit-graph", "write", "--reachable", "--changed-paths")
		}},
		{"single file with topological levels only", 1, func(o *oracle) {
			o.git(0, "-c", "commitGraph.generationVersion=1", "commit-graph", "write", "--reachable", "--changed-paths")
		}},
		{"split chain", 3, func(o *oracle) {
			for _, rev := range []string{"HEAD~4", "HEAD~2", "HEAD"} {
				id := strings.TrimSpace(o.git(0, "rev-parse", rev))
				o.gitInput(id+"\n", "commit-graph", "write", "--split=no-merge", "--changed-paths", "--stdin-commits")
			}
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			o := newOracle(t)
			buildPathHistory(o)
			tt.write(o)
			g, err := Open([]string{o.objectsDir()}, OpenOptions{})
			if err != nil || g == nil {
				t.Fatalf("Open returned %v, %v", g, err)
			}
			if g.Layers() != tt.layers || !g.ChangedPaths() {
				t.Fatalf("graph has %d layers, changed paths %v", g.Layers(), g.ChangedPaths())
			}
			commits := changedPathsOf(t, o.openDB(), o.history())
			checkGraphAgainstObjects(t, g, commits)
			checkFiltersNeverMissAChange(t, g, commits)
		})
	}
}
