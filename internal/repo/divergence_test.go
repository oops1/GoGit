package repo

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/refs"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
)

var errDivergenceInjected = errors.New("divergence: injected failure")

func divergenceTestSignature() object.Signature {
	return object.Signature{Name: "Go Git", Email: "gogit@example.com", When: time.Unix(1700000000, 0).UTC()}
}

func newDivergenceRepoDir(t *testing.T, branch string) string {
	t.Helper()
	dir := t.TempDir()
	r, err := gitrepo.Init(dir, gitrepo.InitOptions{InitialBranch: branch})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	return dir
}

func openDivergenceWriters(t *testing.T, dir string) (*odb.DB, *refs.Store) {
	t.Helper()
	r, err := gitrepo.Open(dir, gitrepo.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := r.Close(); err != nil {
			t.Errorf("Close returned error %v", err)
		}
	})
	db, err := odb.Open(r.ObjectsDir(), odb.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("Close returned error %v", err)
		}
	})
	store, err := refs.Open(refs.Options{GitDir: r.GitDir(), CommonDir: r.CommonDir(), Committer: divergenceTestSignature})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("Close returned error %v", err)
		}
	})
	return db, store
}

func openForDivergence(t *testing.T, dir string) *gitrepo.Repository {
	t.Helper()
	r, err := gitrepo.Open(dir, gitrepo.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := r.Close(); err != nil {
			t.Errorf("Close returned error %v", err)
		}
	})
	return r
}

func putDivergenceCommit(t *testing.T, db *odb.DB, message string, parents ...hash.ObjectID) hash.ObjectID {
	t.Helper()
	treeID, err := db.PutObject(&object.Tree{})
	if err != nil {
		t.Fatal(err)
	}
	sig := divergenceTestSignature()
	commit := &object.Commit{Tree: treeID, Author: sig, Committer: sig, Message: message + "\n", Parents: parents}
	id, err := db.PutObject(commit)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func setDivergenceRef(t *testing.T, store *refs.Store, name refs.Name, target hash.ObjectID) {
	t.Helper()
	tx := store.Begin()
	if err := tx.Set(name, target); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func setDivergenceUpstream(t *testing.T, dir, branch, remote, mergeRef string) {
	t.Helper()
	path := filepath.Join(dir, ".git", "config")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	text := "[branch \"" + branch + "\"]\n\tremote = " + remote + "\n\tmerge = " + mergeRef + "\n"
	if _, err := f.WriteString(text); err != nil {
		t.Fatal(err)
	}
}

func divergenceFakeObjectID(t *testing.T, seed string) hash.ObjectID {
	t.Helper()
	id, err := hash.Parse(seed + strings.Repeat("0", hash.HexSize-len(seed)))
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestAheadBehindReportsNoUpstreamWithoutBranchConfig(t *testing.T) {
	dir := newDivergenceRepoDir(t, "main")
	db, store := openDivergenceWriters(t, dir)
	commit := putDivergenceCommit(t, db, "first")
	setDivergenceRef(t, store, refs.BranchName("main"), commit)

	div, hasUpstream, err := AheadBehind(openForDivergence(t, dir))
	if err != nil {
		t.Fatalf("AheadBehind returned error %v", err)
	}
	if hasUpstream {
		t.Fatal("a branch without tracking config must report no upstream")
	}
	if div != (Divergence{}) {
		t.Fatalf("divergence = %+v, want zero", div)
	}
}

func TestAheadBehindReportsNoUpstreamForADetachedHead(t *testing.T) {
	dir := newDivergenceRepoDir(t, "main")
	db, store := openDivergenceWriters(t, dir)
	commit := putDivergenceCommit(t, db, "first")
	setDivergenceRef(t, store, refs.BranchName("main"), commit)
	tx := store.Begin()
	if err := tx.Detach(refs.HEAD, commit); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	div, hasUpstream, err := AheadBehind(openForDivergence(t, dir))
	if err != nil {
		t.Fatalf("AheadBehind returned error %v", err)
	}
	if hasUpstream {
		t.Fatal("a detached HEAD must report no upstream")
	}
	if div != (Divergence{}) {
		t.Fatalf("divergence = %+v, want zero", div)
	}
}

func TestAheadBehindReportsNoUpstreamWhenTheTrackingRefIsMissing(t *testing.T) {
	dir := newDivergenceRepoDir(t, "main")
	db, store := openDivergenceWriters(t, dir)
	commit := putDivergenceCommit(t, db, "first")
	setDivergenceRef(t, store, refs.BranchName("main"), commit)
	setDivergenceUpstream(t, dir, "main", "origin", "refs/heads/main")

	div, hasUpstream, err := AheadBehind(openForDivergence(t, dir))
	if err != nil {
		t.Fatalf("AheadBehind returned error %v", err)
	}
	if hasUpstream {
		t.Fatal("a missing remote-tracking ref must report no upstream")
	}
	if div != (Divergence{}) {
		t.Fatalf("divergence = %+v, want zero", div)
	}
}

func TestAheadBehindReportsEqualWhenLocalMatchesUpstream(t *testing.T) {
	dir := newDivergenceRepoDir(t, "main")
	db, store := openDivergenceWriters(t, dir)
	commit := putDivergenceCommit(t, db, "first")
	setDivergenceRef(t, store, refs.BranchName("main"), commit)
	setDivergenceRef(t, store, refs.RemoteBranchName("origin", "main"), commit)
	setDivergenceUpstream(t, dir, "main", "origin", "refs/heads/main")

	div, hasUpstream, err := AheadBehind(openForDivergence(t, dir))
	if err != nil {
		t.Fatalf("AheadBehind returned error %v", err)
	}
	if !hasUpstream {
		t.Fatal("a configured and resolvable upstream must be reported")
	}
	if div != (Divergence{}) {
		t.Fatalf("divergence = %+v, want zero", div)
	}
}

func TestAheadBehindReportsAheadWhenLocalHasExtraCommits(t *testing.T) {
	dir := newDivergenceRepoDir(t, "main")
	db, store := openDivergenceWriters(t, dir)
	base := putDivergenceCommit(t, db, "base")
	ahead1 := putDivergenceCommit(t, db, "ahead1", base)
	ahead2 := putDivergenceCommit(t, db, "ahead2", ahead1)
	setDivergenceRef(t, store, refs.BranchName("main"), ahead2)
	setDivergenceRef(t, store, refs.RemoteBranchName("origin", "main"), base)
	setDivergenceUpstream(t, dir, "main", "origin", "refs/heads/main")

	div, hasUpstream, err := AheadBehind(openForDivergence(t, dir))
	if err != nil {
		t.Fatalf("AheadBehind returned error %v", err)
	}
	if !hasUpstream {
		t.Fatal("must report upstream")
	}
	if div != (Divergence{Ahead: 2, Behind: 0}) {
		t.Fatalf("divergence = %+v, want {2 0}", div)
	}
}

func TestAheadBehindReportsBehindWhenUpstreamHasExtraCommits(t *testing.T) {
	dir := newDivergenceRepoDir(t, "main")
	db, store := openDivergenceWriters(t, dir)
	base := putDivergenceCommit(t, db, "base")
	behind1 := putDivergenceCommit(t, db, "behind1", base)
	behind2 := putDivergenceCommit(t, db, "behind2", behind1)
	setDivergenceRef(t, store, refs.BranchName("main"), base)
	setDivergenceRef(t, store, refs.RemoteBranchName("origin", "main"), behind2)
	setDivergenceUpstream(t, dir, "main", "origin", "refs/heads/main")

	div, hasUpstream, err := AheadBehind(openForDivergence(t, dir))
	if err != nil {
		t.Fatalf("AheadBehind returned error %v", err)
	}
	if !hasUpstream {
		t.Fatal("must report upstream")
	}
	if div != (Divergence{Ahead: 0, Behind: 2}) {
		t.Fatalf("divergence = %+v, want {0 2}", div)
	}
}

func TestAheadBehindReportsBothWhenHistoriesDiverge(t *testing.T) {
	dir := newDivergenceRepoDir(t, "main")
	db, store := openDivergenceWriters(t, dir)
	base := putDivergenceCommit(t, db, "base")
	local := putDivergenceCommit(t, db, "local", base)
	remote := putDivergenceCommit(t, db, "remote", base)
	setDivergenceRef(t, store, refs.BranchName("main"), local)
	setDivergenceRef(t, store, refs.RemoteBranchName("origin", "main"), remote)
	setDivergenceUpstream(t, dir, "main", "origin", "refs/heads/main")

	div, hasUpstream, err := AheadBehind(openForDivergence(t, dir))
	if err != nil {
		t.Fatalf("AheadBehind returned error %v", err)
	}
	if !hasUpstream {
		t.Fatal("must report upstream")
	}
	if div != (Divergence{Ahead: 1, Behind: 1}) {
		t.Fatalf("divergence = %+v, want {1 1}", div)
	}
}

func TestAheadBehindPropagatesRefsOpenFailure(t *testing.T) {
	dir := newDivergenceRepoDir(t, "main")
	prev := openDivergenceRefs
	openDivergenceRefs = func(refs.Options) (*refs.Store, error) { return nil, errDivergenceInjected }
	t.Cleanup(func() { openDivergenceRefs = prev })

	_, _, err := AheadBehind(openForDivergence(t, dir))
	if !errors.Is(err, errDivergenceInjected) {
		t.Fatalf("AheadBehind returned %v, want %v", err, errDivergenceInjected)
	}
}

func TestAheadBehindPropagatesHeadLookupFailure(t *testing.T) {
	dir := newDivergenceRepoDir(t, "main")
	headFile := filepath.Join(dir, ".git", "HEAD")
	if err := os.WriteFile(headFile, []byte("ref: refs/heads/.bad\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, _, err := AheadBehind(openForDivergence(t, dir))
	if err == nil {
		t.Fatal("a malformed HEAD must produce an error")
	}
}

func TestAheadBehindPropagatesShallowReadFailure(t *testing.T) {
	dir := newDivergenceRepoDir(t, "main")
	db, store := openDivergenceWriters(t, dir)
	commit := putDivergenceCommit(t, db, "first")
	setDivergenceRef(t, store, refs.BranchName("main"), commit)
	setDivergenceRef(t, store, refs.RemoteBranchName("origin", "main"), commit)
	setDivergenceUpstream(t, dir, "main", "origin", "refs/heads/main")
	if err := os.MkdirAll(filepath.Join(dir, ".git", "shallow"), 0o777); err != nil {
		t.Fatal(err)
	}

	_, _, err := AheadBehind(openForDivergence(t, dir))
	if err == nil {
		t.Fatal("a shallow file that is really a directory must produce an error")
	}
}

func TestAheadBehindPropagatesObjectsOpenFailure(t *testing.T) {
	dir := newDivergenceRepoDir(t, "main")
	db, store := openDivergenceWriters(t, dir)
	commit := putDivergenceCommit(t, db, "first")
	setDivergenceRef(t, store, refs.BranchName("main"), commit)
	setDivergenceRef(t, store, refs.RemoteBranchName("origin", "main"), commit)
	setDivergenceUpstream(t, dir, "main", "origin", "refs/heads/main")

	prev := openDivergenceObjects
	openDivergenceObjects = func(string, odb.Options) (*odb.DB, error) { return nil, errDivergenceInjected }
	t.Cleanup(func() { openDivergenceObjects = prev })

	_, _, err := AheadBehind(openForDivergence(t, dir))
	if !errors.Is(err, errDivergenceInjected) {
		t.Fatalf("AheadBehind returned %v, want %v", err, errDivergenceInjected)
	}
}

func TestAheadBehindPropagatesWalkFailureForTheLocalCommit(t *testing.T) {
	dir := newDivergenceRepoDir(t, "main")
	db, store := openDivergenceWriters(t, dir)
	remoteCommit := putDivergenceCommit(t, db, "remote")
	missing := divergenceFakeObjectID(t, "deadbeef")
	setDivergenceRef(t, store, refs.BranchName("main"), missing)
	setDivergenceRef(t, store, refs.RemoteBranchName("origin", "main"), remoteCommit)
	setDivergenceUpstream(t, dir, "main", "origin", "refs/heads/main")

	_, _, err := AheadBehind(openForDivergence(t, dir))
	if err == nil {
		t.Fatal("a local branch pointing at a missing commit must produce an error")
	}
}

func TestAheadBehindPropagatesWalkFailureForTheRemoteCommit(t *testing.T) {
	dir := newDivergenceRepoDir(t, "main")
	db, store := openDivergenceWriters(t, dir)
	localCommit := putDivergenceCommit(t, db, "local")
	missing := divergenceFakeObjectID(t, "deadbeef")
	setDivergenceRef(t, store, refs.BranchName("main"), localCommit)
	setDivergenceRef(t, store, refs.RemoteBranchName("origin", "main"), missing)
	setDivergenceUpstream(t, dir, "main", "origin", "refs/heads/main")

	_, _, err := AheadBehind(openForDivergence(t, dir))
	if err == nil {
		t.Fatal("a remote-tracking ref pointing at a missing commit must produce an error")
	}
}

func TestAheadBehindPropagatesTrackingRefLookupFailure(t *testing.T) {
	dir := newDivergenceRepoDir(t, "main")
	db, store := openDivergenceWriters(t, dir)
	commit := putDivergenceCommit(t, db, "first")
	setDivergenceRef(t, store, refs.BranchName("main"), commit)
	setDivergenceUpstream(t, dir, "main", "origin", "refs/heads/.bad")

	_, _, err := AheadBehind(openForDivergence(t, dir))
	if err == nil {
		t.Fatal("an invalid merge ref name must produce an error")
	}
}

func TestAheadBehindPropagatesLocalCommitLookupFailure(t *testing.T) {
	dir := newDivergenceRepoDir(t, "main")
	db, store := openDivergenceWriters(t, dir)
	commit := putDivergenceCommit(t, db, "first")
	setDivergenceRef(t, store, refs.BranchName("main"), commit)
	setDivergenceRef(t, store, refs.RemoteBranchName("origin", "main"), commit)
	setDivergenceUpstream(t, dir, "main", "origin", "refs/heads/main")

	branchFile := filepath.Join(dir, ".git", "refs", "heads", "main")
	if err := os.WriteFile(branchFile, []byte("not a valid ref\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, _, err := AheadBehind(openForDivergence(t, dir))
	if err == nil {
		t.Fatal("a malformed local branch ref must produce an error")
	}
}

func TestAheadBehindReportsAllRemoteCommitsAsBehindForAnUnbornBranch(t *testing.T) {
	dir := newDivergenceRepoDir(t, "main")
	db, store := openDivergenceWriters(t, dir)
	remoteCommit := putDivergenceCommit(t, db, "remote")
	setDivergenceRef(t, store, refs.RemoteBranchName("origin", "main"), remoteCommit)
	setDivergenceUpstream(t, dir, "main", "origin", "refs/heads/main")

	div, hasUpstream, err := AheadBehind(openForDivergence(t, dir))
	if err != nil {
		t.Fatalf("AheadBehind returned error %v", err)
	}
	if !hasUpstream {
		t.Fatal("an unborn branch with a configured and resolvable upstream must report it")
	}
	if div != (Divergence{Ahead: 0, Behind: 1}) {
		t.Fatalf("divergence = %+v, want {0 1}", div)
	}
}
