package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/oops1/gogit/internal/gitcore/commitgraph"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/refs"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/gitcore/revision"
	"github.com/oops1/gogit/internal/repo"
)

func countGraphOpens(t *testing.T) *int {
	t.Helper()
	opens := new(int)
	previous := openCommitGraph
	openCommitGraph = func(*gitrepo.Repository, *odb.DB) (*commitgraph.Graph, error) {
		*opens++
		return nil, nil
	}
	t.Cleanup(func() { openCommitGraph = previous })
	return opens
}

func writeGraphFile(t *testing.T, o *openedRepository, rel, content string, age time.Duration) {
	t.Helper()
	path := filepath.Join(o.repo.ObjectsDir(), "info", filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o777); err != nil {
		t.Fatalf("MkdirAll returned error %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o666); err != nil {
		t.Fatalf("WriteFile returned error %v", err)
	}
	when := time.Now().Add(-age)
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatalf("Chtimes returned error %v", err)
	}
}

func TestCommitGraphIsOpenedOnceWhileItsFilesAreUnchanged(t *testing.T) {
	a, _, _ := cloneForDivergence(t)
	o := a.opened()
	opens := countGraphOpens(t)
	writeGraphFile(t, o, "commit-graph", "graph", time.Hour)

	for range 5 {
		o.commitGraph()
	}

	if *opens != 1 {
		t.Fatalf("the commit graph was opened %d times, want once", *opens)
	}
}

func TestCommitGraphIsOpenedAgainWhenItsFileChanges(t *testing.T) {
	a, _, _ := cloneForDivergence(t)
	o := a.opened()
	opens := countGraphOpens(t)
	writeGraphFile(t, o, "commit-graph", "graph", time.Hour)
	o.commitGraph()

	writeGraphFile(t, o, "commit-graph", "a longer graph", time.Hour)
	writeGraphFile(t, o, "commit-graphs/commit-graph-chain", "chain", time.Hour)
	o.commitGraph()
	o.commitGraph()

	if *opens != 2 {
		t.Fatalf("the commit graph was opened %d times, want twice", *opens)
	}
}

func TestCommitGraphIsNotTrustedWhileItsFileIsFresh(t *testing.T) {
	a, _, _ := cloneForDivergence(t)
	o := a.opened()
	opens := countGraphOpens(t)
	writeGraphFile(t, o, "commit-graph", "graph", 0)

	for range 3 {
		o.commitGraph()
	}

	if *opens != 3 {
		t.Fatalf("the commit graph was opened %d times, want every time while the file is fresh", *opens)
	}
}

func TestCommitGraphIsTrustedOnceItsFileHasSettled(t *testing.T) {
	a, _, _ := cloneForDivergence(t)
	o := a.opened()
	opens := countGraphOpens(t)
	writeGraphFile(t, o, "commit-graph", "graph", 0)
	previous := graphClock
	graphClock = func() time.Time { return time.Now().Add(graphSettle) }
	t.Cleanup(func() { graphClock = previous })

	for range 3 {
		o.commitGraph()
	}

	if *opens != 1 {
		t.Fatalf("the commit graph was opened %d times, want once after the file settled", *opens)
	}
}

func TestCommitGraphIsNotOpenedAgainWhenTheRepositoryHasNone(t *testing.T) {
	a, _, _ := cloneForDivergence(t)
	o := a.opened()
	opens := countGraphOpens(t)

	for range 4 {
		o.commitGraph()
	}

	if *opens != 0 {
		t.Fatalf("the commit graph was opened %d times, want none: the absence is cached too", *opens)
	}
}

func TestRevisionContextCarriesTheOpenRepositoryAndItsCachedGraph(t *testing.T) {
	a, _, _ := cloneForDivergence(t)
	o := a.opened()

	source := o.revisionContext()

	want := revision.Context{Objects: o.db, Refs: o.store, Shallow: o.shallow, Graph: o.commitGraph()}
	if source.Objects != want.Objects || source.Refs != want.Refs || source.Graph != want.Graph || len(source.Shallow) != len(want.Shallow) {
		t.Fatalf("revisionContext returned %+v, want %+v", source, want)
	}
}

func TestRefreshDoesNotCountDivergenceAgainWhileBranchTipsAreUnchanged(t *testing.T) {
	a, _, local := cloneForDivergence(t)
	calls := 0
	previous := manyAheadBehind
	manyAheadBehind = func(source revision.Context, pairs []repo.BranchPair) (map[refs.Name]repo.Divergence, error) {
		calls++
		return previous(source, pairs)
	}
	t.Cleanup(func() { manyAheadBehind = previous })

	a.RefreshRepository()
	a.branchDivergenceWG.Wait()
	a.RefreshRepository()
	a.branchDivergenceWG.Wait()
	if calls != 0 {
		t.Fatalf("divergence was counted %d times while no branch moved, want none", calls)
	}

	addLocalCommitWithMessage(t, local, "main", "second.txt", "second\n", "second")
	a.RefreshRepository()
	a.branchDivergenceWG.Wait()

	if calls != 1 {
		t.Fatalf("divergence was counted %d times after one branch moved, want once", calls)
	}
}
