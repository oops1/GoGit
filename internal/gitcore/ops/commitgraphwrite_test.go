package ops

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/commitgraph"
	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/refs"
)

func commitGraphPath(r *testRepo) string {
	return filepath.Join(r.repo.ObjectsDir(), objectsInfoDir, commitgraph.FileName)
}

func TestWritingTheCommitGraphCoversEveryReachableCommit(t *testing.T) {
	r := newTestRepo(t)
	buildMaintHistory(t, r)

	written, err := WriteCommitGraph(t.Context(), r.repo)
	if err != nil {
		t.Fatal(err)
	}

	if written != 2 {
		t.Fatalf("written = %d, want both commits", written)
	}
	if _, err := os.Stat(commitGraphPath(r)); err != nil {
		t.Fatalf("graph missing: %v", err)
	}
}

func TestNoGraphIsWrittenForAnEmptyOrShallowRepository(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		r := newTestRepo(t)
		if written, err := WriteCommitGraph(t.Context(), r.repo); err != nil || written != 0 {
			t.Fatalf("written = %d, err = %v", written, err)
		}
		if _, err := os.Stat(commitGraphPath(r)); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("graph written for an empty repository: %v", err)
		}
	})
	t.Run("shallow", func(t *testing.T) {
		r := newTestRepo(t)
		h := buildMaintHistory(t, r)
		writeGitFile(t, r.repo.CommonDir(), "shallow", h.second.String()+"\n")
		if written, err := WriteCommitGraph(t.Context(), r.repo); err != nil || written != 0 {
			t.Fatalf("written = %d, err = %v", written, err)
		}
		if _, err := os.Stat(commitGraphPath(r)); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("graph written for a shallow repository: %v", err)
		}
	})
}

func TestWritingTheCommitGraphFollowsTheGraphSettings(t *testing.T) {
	read := func(t *testing.T, r *testRepo) *commitgraph.Graph {
		t.Helper()
		graph, err := commitgraph.Open([]string{r.repo.ObjectsDir()}, commitgraph.OpenOptions{})
		if err != nil || graph == nil {
			t.Fatalf("Open returned %v, %v", graph, err)
		}
		return graph
	}
	t.Run("corrected dates by default", func(t *testing.T) {
		r := newTestRepo(t)
		buildMaintHistory(t, r)
		if _, err := WriteCommitGraph(t.Context(), r.repo); err != nil {
			t.Fatal(err)
		}
		if graph := read(t, r); !graph.CorrectedDates() || graph.ChangedPaths() {
			t.Fatalf("corrected %v, changed paths %v", graph.CorrectedDates(), graph.ChangedPaths())
		}
	})
	t.Run("topological levels and kept changed paths", func(t *testing.T) {
		r := newTestRepo(t)
		h := buildMaintHistory(t, r)
		db := r.db()
		first, err := db.Commit(h.first)
		if err != nil {
			t.Fatal(err)
		}
		seed := []commitgraph.Commit{{ID: h.first, Tree: first.Tree, Changed: &commitgraph.ChangedPaths{}}}
		if err := commitgraph.WriteFile(filepath.Join(r.repo.ObjectsDir(), objectsInfoDir), hash.SHA1, seed, commitgraph.EncodeOptions{ChangedPaths: true}); err != nil {
			t.Fatal(err)
		}
		r.appendConfig("[commitGraph]\n\tgenerationVersion = 1\n")
		r.repo = r.reopen()
		if written, err := WriteCommitGraph(t.Context(), r.repo); err != nil || written != 2 {
			t.Fatalf("written = %d, err = %v", written, err)
		}
		graph := read(t, r)
		second, _ := graph.Lookup(h.second)
		if graph.CorrectedDates() || !graph.ChangedPaths() || graph.MaybeChanged(second, graph.BloomKeys("nowhere/at/all")) {
			t.Fatalf("corrected %v, changed paths %v", graph.CorrectedDates(), graph.ChangedPaths())
		}
	})
	t.Run("switched off", func(t *testing.T) {
		r := newTestRepo(t)
		buildMaintHistory(t, r)
		r.appendConfig("[core]\n\tcommitGraph = false\n")
		r.repo = r.reopen()
		if written, err := WriteCommitGraph(t.Context(), r.repo); err != nil || written != 0 {
			t.Fatalf("written = %d, err = %v", written, err)
		}
	})
}

func TestChangedPathsReportAnUnreadableTree(t *testing.T) {
	r := newTestRepo(t)
	commits := []commitgraph.Commit{{ID: bogusObjectID(t, hash.SHA1), Tree: bogusObjectID(t, hash.SHA1)}}
	if err := fillChangedPaths(t.Context(), r.db(), commits); err == nil {
		t.Fatal("a missing tree was accepted")
	}
}

func TestWritingTheCommitGraphReportsEveryFailure(t *testing.T) {
	boom := errors.New("boom")
	for _, tt := range []struct {
		name  string
		setup func(t *testing.T, r *testRepo)
	}{
		{"reading the shallow file", func(t *testing.T, r *testRepo) {
			if err := os.MkdirAll(filepath.Join(r.repo.CommonDir(), "shallow"), 0o777); err != nil {
				t.Fatal(err)
			}
		}},
		{"opening the database", func(t *testing.T, r *testRepo) {
			swapMaint(t, &odbOpen, func(string, odb.Options) (*odb.DB, error) { return nil, boom })
		}},
		{"walking the history", func(t *testing.T, r *testRepo) {
			swapMaint(t, &refsOpen, func(refs.Options) (*refs.Store, error) { return nil, boom })
		}},
		{"reading the graph settings", func(t *testing.T, r *testRepo) {
			r.appendConfig("[core]\n\tcommitGraph = maybe\n")
			r.repo = r.reopen()
		}},
		{"writing the file", func(t *testing.T, r *testRepo) {
			swapMaint(t, &writeCommitGraphFile, func(string, hash.Format, []commitgraph.Commit, commitgraph.EncodeOptions) error { return boom })
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := newTestRepo(t)
			buildMaintHistory(t, r)
			tt.setup(t, r)

			if _, err := WriteCommitGraph(t.Context(), r.repo); err == nil {
				t.Fatal("the failure was not reported")
			}
		})
	}
}
