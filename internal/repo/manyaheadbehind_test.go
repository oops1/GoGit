package repo

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/refs"
)

func TestManyAheadBehindReturnsEmptyMapForNoPairs(t *testing.T) {
	dir := newDivergenceRepoDir(t, "main")

	result, err := ManyAheadBehind(openForDivergence(t, dir), nil)
	if err != nil {
		t.Fatalf("ManyAheadBehind returned error %v", err)
	}
	if len(result) != 0 {
		t.Fatalf("result = %+v, want empty", result)
	}
}

func TestManyAheadBehindOmitsBranchesThatMatchUpstream(t *testing.T) {
	dir := newDivergenceRepoDir(t, "main")
	db, _ := openDivergenceWriters(t, dir)
	commit := putDivergenceCommit(t, db, "first")

	result, err := ManyAheadBehind(openForDivergence(t, dir), []BranchPair{
		{Name: refs.BranchName("main"), Local: commit, Remote: commit},
	})
	if err != nil {
		t.Fatalf("ManyAheadBehind returned error %v", err)
	}
	if div, ok := result[refs.BranchName("main")]; ok {
		t.Fatalf("a branch matching upstream must be omitted, got %+v", div)
	}
}

func TestManyAheadBehindReportsEachBranchIndependently(t *testing.T) {
	dir := newDivergenceRepoDir(t, "main")
	db, _ := openDivergenceWriters(t, dir)
	base := putDivergenceCommit(t, db, "base")
	ahead1 := putDivergenceCommit(t, db, "ahead1", base)
	ahead2 := putDivergenceCommit(t, db, "ahead2", ahead1)
	behind1 := putDivergenceCommit(t, db, "behind1", base)
	behind2 := putDivergenceCommit(t, db, "behind2", behind1)
	divergedLocal := putDivergenceCommit(t, db, "divergedLocal", base)
	divergedRemote := putDivergenceCommit(t, db, "divergedRemote", base)

	result, err := ManyAheadBehind(openForDivergence(t, dir), []BranchPair{
		{Name: refs.BranchName("ahead"), Local: ahead2, Remote: base},
		{Name: refs.BranchName("behind"), Local: base, Remote: behind2},
		{Name: refs.BranchName("diverged"), Local: divergedLocal, Remote: divergedRemote},
	})
	if err != nil {
		t.Fatalf("ManyAheadBehind returned error %v", err)
	}
	if div := result[refs.BranchName("ahead")]; div != (Divergence{Ahead: 2, Behind: 0}) {
		t.Fatalf("ahead branch = %+v, want {2 0}", div)
	}
	if div := result[refs.BranchName("behind")]; div != (Divergence{Ahead: 0, Behind: 2}) {
		t.Fatalf("behind branch = %+v, want {0 2}", div)
	}
	if div := result[refs.BranchName("diverged")]; div != (Divergence{Ahead: 1, Behind: 1}) {
		t.Fatalf("diverged branch = %+v, want {1 1}", div)
	}
	if len(result) != 3 {
		t.Fatalf("result = %+v, want exactly 3 entries", result)
	}
}

func TestManyAheadBehindPropagatesShallowReadFailure(t *testing.T) {
	dir := newDivergenceRepoDir(t, "main")
	db, _ := openDivergenceWriters(t, dir)
	commit := putDivergenceCommit(t, db, "first")
	if err := os.MkdirAll(filepath.Join(dir, ".git", "shallow"), 0o777); err != nil {
		t.Fatal(err)
	}

	_, err := ManyAheadBehind(openForDivergence(t, dir), []BranchPair{
		{Name: refs.BranchName("main"), Local: commit, Remote: hash.Zero},
	})
	if err == nil {
		t.Fatal("a shallow file that is really a directory must produce an error")
	}
}

func TestManyAheadBehindPropagatesObjectsOpenFailure(t *testing.T) {
	dir := newDivergenceRepoDir(t, "main")
	db, _ := openDivergenceWriters(t, dir)
	commit := putDivergenceCommit(t, db, "first")

	prev := openDivergenceObjects
	openDivergenceObjects = func(string, odb.Options) (*odb.DB, error) { return nil, errDivergenceInjected }
	t.Cleanup(func() { openDivergenceObjects = prev })

	_, err := ManyAheadBehind(openForDivergence(t, dir), []BranchPair{
		{Name: refs.BranchName("main"), Local: commit, Remote: hash.Zero},
	})
	if err == nil {
		t.Fatal("an objects-open failure must propagate")
	}
}

func TestManyAheadBehindPropagatesWalkFailure(t *testing.T) {
	dir := newDivergenceRepoDir(t, "main")
	missing := divergenceFakeObjectID(t, "deadbeef")

	_, err := ManyAheadBehind(openForDivergence(t, dir), []BranchPair{
		{Name: refs.BranchName("main"), Local: missing, Remote: hash.Zero},
	})
	if err == nil {
		t.Fatal("a pair pointing at a missing commit must produce an error")
	}
}
