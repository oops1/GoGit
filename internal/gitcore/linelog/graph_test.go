package linelog

import (
	"errors"
	"math/rand/v2"
	"path/filepath"
	"slices"
	"strconv"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/commitgraph"
	"github.com/oops1/gogit/internal/gitcore/diff"
	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
)

func graphFor(t *testing.T, m *memory, ids ...hash.ObjectID) *commitgraph.Graph {
	t.Helper()
	opts := diff.Defaults()
	opts.DetectRenames = false
	var commits []commitgraph.Commit
	for _, id := range ids {
		parsed, err := object.ParseCommit(m.data[id])
		if err != nil {
			t.Fatal(err)
		}
		parent := hash.Zero
		if len(parsed.Parents) > 0 {
			first, err := object.ParseCommit(m.data[parsed.Parents[0]])
			if err != nil {
				t.Fatal(err)
			}
			parent = first.Tree
		}
		files, err := diff.TreeChanges(t.Context(), m, parent, parsed.Tree, opts)
		if err != nil {
			t.Fatal(err)
		}
		changed := &commitgraph.ChangedPaths{}
		for _, file := range files {
			changed.Paths = append(changed.Paths, file.NewPath)
		}
		commits = append(commits, commitgraph.Commit{ID: id, Tree: parsed.Tree, Parents: parsed.Parents, Time: parsed.Committer.When.Unix(), Changed: changed})
	}
	dir := t.TempDir()
	if err := commitgraph.WriteFile(filepath.Join(dir, commitgraph.InfoDir), hash.SHA1, commits, commitgraph.EncodeOptions{ChangedPaths: true}); err != nil {
		t.Fatal(err)
	}
	graph, err := commitgraph.Open([]string{dir}, commitgraph.OpenOptions{})
	if err != nil || graph == nil {
		t.Fatalf("commitgraph.Open returned %v, %v", graph, err)
	}
	return graph
}

func randomLineHistory(t *testing.T, seed uint64) (*memory, []hash.ObjectID) {
	t.Helper()
	m := newMemory()
	rng := rand.New(rand.NewPCG(seed, seed+3))
	var ids []hash.ObjectID
	var trees []map[string]any
	for at := range 40 {
		files := map[string]any{"f": lined("1", "2", "3", "4", "5", "6", "7", "8"), "g": lined("x", "y")}
		var parents []hash.ObjectID
		if at > 0 {
			first := rng.IntN(at)
			parents = append(parents, ids[first])
			for path, content := range trees[first] {
				files[path] = content
			}
			if rng.IntN(4) == 0 {
				if second := rng.IntN(at); second != first {
					parents = append(parents, ids[second])
				}
			}
		}
		switch rng.IntN(4) {
		case 0:
			lines := []string{"1", "2", "3", "4", "5", "6", "7", "8"}
			lines[rng.IntN(len(lines))] = "edit " + strconv.Itoa(at)
			files["f"] = lined(lines...)
		case 1:
			files["g"] = lined("x", strconv.Itoa(at))
		case 2:
			files["dir/h"] = strconv.Itoa(at) + "\n"
		}
		m.clock += int64(rng.IntN(120)) - 50
		ids = append(ids, m.commit("c"+strconv.Itoa(at), files, parents...))
		trees = append(trees, files)
	}
	return m, ids
}

func TestLogThroughTheCommitGraphMatchesTheObjectWalk(t *testing.T) {
	for _, seed := range []uint64{1, 2, 3, 4, 5} {
		m, ids := randomLineHistory(t, seed)
		graphs := map[string]*commitgraph.Graph{"full": graphFor(t, m, ids...), "older half": graphFor(t, m, ids[:20]...)}
		for _, spec := range [][]Spec{{{Range: "2,5", Path: "f"}}, {{Range: "1,8", Path: "f"}, {Range: "2", Path: "g"}}, {{Range: ",", Path: "g"}}} {
			want, err := collect(t, m, ids[len(ids)-1], spec, Options{})
			if err != nil {
				t.Fatal(err)
			}
			for name, graph := range graphs {
				var progress []Progress
				got, err := collect(t, m, ids[len(ids)-1], spec, Options{Graph: graph, Progress: func(p Progress) { progress = append(progress, p) }})
				if err != nil || !slices.Equal(got.subjects, want.subjects) || got.patch != want.patch {
					t.Errorf("seed %d, %s, %v: %v, %v\nwant %v", seed, name, spec, got.subjects, err, want.subjects)
				}
				if len(progress) > 0 && (progress[0].Done != 0 || progress[0].Total == 0 || progress[len(progress)-1].Total != progress[0].Total) {
					t.Errorf("seed %d, %s: progress %v", seed, name, progress)
				}
			}
		}
	}
}

func TestLogThroughTheCommitGraphReportsUnreadableCommits(t *testing.T) {
	m := newMemory()
	root := m.commit("root", map[string]any{"f": lined("a", "b")})
	middle := m.commit("middle", map[string]any{"f": lined("a", "B")}, root)
	graph := graphFor(t, m, root)
	head := m.commit("head", map[string]any{"f": lined("A", "B")}, middle)
	spec := []Spec{{Range: "1,2", Path: "f"}}

	for _, failAt := range []int{1, 2} {
		m.reads = map[hash.ObjectID]int{}
		m.failAt[middle] = failAt
		if _, err := collect(t, m, head, spec, Options{Graph: graph}); !errors.Is(err, errMissing) {
			t.Errorf("reading a missing commit for the %d time returned %v", failAt, err)
		}
	}
	delete(m.failAt, middle)
	m.reads = map[hash.ObjectID]int{}
	m.failAt[middle] = 3
	if _, err := collect(t, m, head, spec, Options{Graph: graph}); !errors.Is(err, errMissing) {
		t.Errorf("reading the parent tree returned %v", err)
	}
	merge := m.commit("merge", map[string]any{"f": lined("A", "b")}, head, middle)
	m.reads = map[hash.ObjectID]int{}
	m.failAt[middle] = 3
	if _, err := collect(t, m, merge, spec, Options{Graph: graph}); !errors.Is(err, errMissing) {
		t.Errorf("reading a merge parent tree returned %v", err)
	}
	delete(m.failAt, middle)
	orphan := m.commit("orphan", map[string]any{"f": lined("a")}, hash.ObjectID{8})
	if _, err := collect(t, m, orphan, []Spec{{Range: "1", Path: "f"}}, Options{Graph: graph}); !errors.Is(err, errMissing) {
		t.Errorf("a missing parent returned %v", err)
	}
}
