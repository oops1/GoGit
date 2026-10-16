package ops

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/commitgraph"
	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/refs"
)

func TestEnsuringTheCommitGraphWritesOneWhenTheRepositoryHasNone(t *testing.T) {
	r := newTestRepo(t)
	buildMaintHistory(t, r)

	written, err := EnsureCommitGraph(t.Context(), r.repo)

	if err != nil || written != 2 {
		t.Fatalf("EnsureCommitGraph returned %d, %v, want both commits", written, err)
	}
	graph, err := commitgraph.Open([]string{r.repo.ObjectsDir()}, commitgraph.OpenOptions{})
	if err != nil || graph == nil || graph.Len() != 2 {
		t.Fatalf("the written graph is %v, %v, want two commits", graph, err)
	}
}

func TestEnsuringTheCommitGraphLeavesAnExistingGraphAlone(t *testing.T) {
	r := newTestRepo(t)
	buildMaintHistory(t, r)
	if _, err := EnsureCommitGraph(t.Context(), r.repo); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(commitGraphPath(r))
	if err != nil {
		t.Fatal(err)
	}

	written, err := EnsureCommitGraph(t.Context(), r.repo)

	if err != nil || written != 0 {
		t.Fatalf("EnsureCommitGraph returned %d, %v, want nothing written", written, err)
	}
	after, err := os.Stat(commitGraphPath(r))
	if err != nil || !after.ModTime().Equal(before.ModTime()) {
		t.Fatalf("the graph was rewritten: %v, %v", after, err)
	}
}

func TestEnsuringTheCommitGraphRecognisesAGraphChain(t *testing.T) {
	r := newTestRepo(t)
	buildMaintHistory(t, r)
	chain := filepath.Join(r.repo.ObjectsDir(), commitgraph.InfoDir, commitgraph.GraphsDir, commitgraph.ChainFileName)
	if err := os.MkdirAll(filepath.Dir(chain), 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(chain, []byte("chain\n"), 0o666); err != nil {
		t.Fatal(err)
	}

	written, err := EnsureCommitGraph(t.Context(), r.repo)

	if err != nil || written != 0 {
		t.Fatalf("EnsureCommitGraph returned %d, %v, want nothing written next to a chain", written, err)
	}
}

func TestEnsuringTheCommitGraphHonoursTheSettingsAndTheRepositoryKind(t *testing.T) {
	for _, tt := range []struct {
		name  string
		setup func(t *testing.T, r *testRepo)
	}{
		{"an empty repository", func(*testing.T, *testRepo) {}},
		{"a shallow repository", func(t *testing.T, r *testRepo) {
			h := buildMaintHistory(t, r)
			writeGitFile(t, r.repo.CommonDir(), "shallow", h.second.String()+"\n")
		}},
		{"core.commitGraph switched off", func(t *testing.T, r *testRepo) {
			buildMaintHistory(t, r)
			r.appendConfig("[core]\n\tcommitGraph = false\n")
			r.repo = r.reopen()
		}},
		{"gc.writeCommitGraph switched off", func(t *testing.T, r *testRepo) {
			buildMaintHistory(t, r)
			r.appendConfig("[gc]\n\twriteCommitGraph = false\n")
			r.repo = r.reopen()
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := newTestRepo(t)
			tt.setup(t, r)

			written, err := EnsureCommitGraph(t.Context(), r.repo)

			if err != nil || written != 0 {
				t.Fatalf("EnsureCommitGraph returned %d, %v, want nothing", written, err)
			}
			if _, err := os.Stat(commitGraphPath(r)); !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("a graph was written: %v", err)
			}
		})
	}
}

func TestEnsuringTheCommitGraphCountsEachCommitOnceEvenWhenAnnotatedTagsPointAtIt(t *testing.T) {
	r := newTestRepo(t)
	h := buildMaintHistory(t, r)
	second, err := r.db().PutObject(&object.Tag{Object: h.second, ObjectType: object.TypeCommit, Name: "v2", Message: "v2\n"})
	if err != nil {
		t.Fatal(err)
	}
	setMaintRef(t, r, refs.TagName("v2"), second)
	setMaintRef(t, r, refs.TagName("v2-again"), second)
	setMaintRef(t, r, refs.TagName("v2-plain"), h.second)
	lonely := putMaintCommit(t, r, h.tree1)
	lonelyTag, err := r.db().PutObject(&object.Tag{Object: lonely, ObjectType: object.TypeCommit, Name: "lonely", Message: "lonely\n"})
	if err != nil {
		t.Fatal(err)
	}
	setMaintRef(t, r, refs.TagName("lonely"), lonelyTag)
	treeRef := putTree(t, r, object.TreeEntry{Mode: object.ModeBlob, Name: "a.txt", ID: h.blobA})
	setMaintRef(t, r, refs.Name("refs/odd/tree"), treeRef)

	written, err := EnsureCommitGraph(t.Context(), r.repo)

	if err != nil || written != 3 {
		t.Fatalf("EnsureCommitGraph returned %d, %v, want three commits", written, err)
	}
}

func TestEnsuringTheCommitGraphStartsFromADetachedHead(t *testing.T) {
	r := newTestRepo(t)
	h := buildMaintHistory(t, r)
	if err := os.WriteFile(filepath.Join(r.repo.GitDir(), "HEAD"), []byte(h.first.String()+"\n"), 0o666); err != nil {
		t.Fatal(err)
	}
	setMaintRef(t, r, refs.BranchName("main"), h.first)
	if err := os.Remove(filepath.Join(r.repo.CommonDir(), "refs", "tags", "v1")); err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}

	written, err := EnsureCommitGraph(t.Context(), r.repo)

	if err != nil || written < 1 {
		t.Fatalf("EnsureCommitGraph returned %d, %v, want the commits behind a detached HEAD", written, err)
	}
}

func TestEnsuringTheCommitGraphStopsWhenTheCallerGivesUp(t *testing.T) {
	r := newTestRepo(t)
	buildMaintHistory(t, r)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, err := EnsureCommitGraph(ctx, r.repo); !errors.Is(err, context.Canceled) {
		t.Fatalf("EnsureCommitGraph returned %v, want context.Canceled", err)
	}
}

func TestEnsuringTheCommitGraphReportsEveryFailure(t *testing.T) {
	boom := errors.New("boom")
	for _, tt := range []struct {
		name  string
		setup func(t *testing.T, r *testRepo)
	}{
		{"reading the graph settings", func(t *testing.T, r *testRepo) {
			r.appendConfig("[core]\n\tcommitGraph = maybe\n")
			r.repo = r.reopen()
		}},
		{"reading gc.writeCommitGraph", func(t *testing.T, r *testRepo) {
			r.appendConfig("[gc]\n\twriteCommitGraph = maybe\n")
			r.repo = r.reopen()
		}},
		{"reading the shallow file", func(t *testing.T, r *testRepo) {
			if err := os.MkdirAll(filepath.Join(r.repo.CommonDir(), "shallow"), 0o777); err != nil {
				t.Fatal(err)
			}
		}},
		{"opening the database", func(t *testing.T, r *testRepo) {
			swapMaint(t, &odbOpen, func(string, odb.Options) (*odb.DB, error) { return nil, boom })
		}},
		{"opening the references", func(t *testing.T, r *testRepo) {
			swapMaint(t, &refsOpen, func(refs.Options) (*refs.Store, error) { return nil, boom })
		}},
		{"listing the references", func(t *testing.T, r *testRepo) {
			if err := os.WriteFile(filepath.Join(r.repo.CommonDir(), "packed-refs"), []byte("garbage\n"), 0o666); err != nil {
				t.Fatal(err)
			}
		}},
		{"resolving HEAD", func(t *testing.T, r *testRepo) {
			if err := os.WriteFile(filepath.Join(r.repo.GitDir(), "HEAD"), []byte("garbage\n"), 0o666); err != nil {
				t.Fatal(err)
			}
		}},
		{"reading a commit that does not parse", func(t *testing.T, r *testRepo) {
			broken, err := r.db().Put(object.TypeCommit, []byte("not a commit"))
			if err != nil {
				t.Fatal(err)
			}
			setMaintRef(t, r, refs.BranchName("broken"), broken)
		}},
		{"reading a commit", func(t *testing.T, r *testRepo) {
			setMaintRef(t, r, refs.BranchName("lost"), bogusObjectID(t, hash.SHA1))
		}},
		{"writing the file", func(t *testing.T, r *testRepo) {
			swapMaint(t, &writeCommitGraphFile, func(string, hash.Format, []commitgraph.Commit, commitgraph.EncodeOptions) error { return boom })
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := newTestRepo(t)
			buildMaintHistory(t, r)
			tt.setup(t, r)

			if _, err := EnsureCommitGraph(t.Context(), r.repo); err == nil {
				t.Fatal("the failure was not reported")
			}
		})
	}
}
