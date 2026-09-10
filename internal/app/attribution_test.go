package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/refs"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
)

func addLocalCommitWithMessage(t *testing.T, dir, branch, name, content, message string) hash.ObjectID {
	t.Helper()
	r, err := gitrepo.Open(dir, gitrepo.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	db, err := odb.Open(r.ObjectsDir(), odb.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	store, err := refs.Open(refs.Options{GitDir: r.GitDir(), CommonDir: r.CommonDir(), Committer: testCommitter})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()

	var parents []hash.ObjectID
	if head, err := store.Lookup(refs.BranchName(branch)); err == nil {
		parents = append(parents, head.Target)
	}
	sig := testCommitter()
	commit := &object.Commit{
		Tree:      putChangesTree(t, db, map[string]string{name: content}),
		Parents:   parents,
		Author:    sig,
		Committer: sig,
		Message:   message,
	}
	id, err := db.PutObject(commit)
	if err != nil {
		t.Fatal(err)
	}
	setRef(t, store, refs.BranchName(branch), id)
	return id
}

func messageOfCommit(t *testing.T, dir string, id hash.ObjectID) string {
	t.Helper()
	r, err := gitrepo.Open(dir, gitrepo.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	db, err := odb.Open(r.ObjectsDir(), odb.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	commit, err := db.Commit(id)
	if err != nil {
		t.Fatal(err)
	}
	return commit.Message
}

func TestAPushLeavesTheAttributionOfOtherPeopleBehind(t *testing.T) {
	dir := t.TempDir()
	server := filepath.Join(dir, "server")
	local := filepath.Join(dir, "local")
	initRemoteServerRepo(t, server, "main")

	a := newRemoteTestApp(t)
	views := captureOperationViews(t)
	cloneIntoRegistry(t, a, server, local)
	addLocalCommitWithMessage(t, local, "main", "mine.txt", "mine\n", "mine\n\nCo-authored-by: Bob <bob@example.com>\n")

	a.startPush()
	view := lastOperationView(t, views)
	waitForFinishedOperation(t, a, view)

	pushed, ok := readRemoteRef(t, server, refs.BranchName("main"))
	if !ok {
		t.Fatal("the branch must reach the server")
	}
	if got := messageOfCommit(t, local, pushed); got != "mine\n" {
		t.Fatalf("message on the server = %q, want the trailer gone", got)
	}
	local_, ok := readRemoteRef(t, filepath.Join(local, ".git"), refs.BranchName("main"))
	if !ok || local_ != pushed {
		t.Fatalf("local branch = %v, want the rewritten commit %s", local_, pushed)
	}
}

func TestWithTheBanOffTheCommitIsPushedAsItWasWritten(t *testing.T) {
	dir := t.TempDir()
	server := filepath.Join(dir, "server")
	local := filepath.Join(dir, "local")
	initRemoteServerRepo(t, server, "main")

	a := newRemoteTestApp(t)
	a.cfg.Git.BanAttribution = false
	views := captureOperationViews(t)
	cloneIntoRegistry(t, a, server, local)
	written := addLocalCommitWithMessage(t, local, "main", "mine.txt", "mine\n", "mine\n\nCo-authored-by: Bob <bob@example.com>\n")

	a.startPush()
	view := lastOperationView(t, views)
	waitForFinishedOperation(t, a, view)

	pushed, ok := readRemoteRef(t, server, refs.BranchName("main"))
	if !ok || pushed != written {
		t.Fatalf("commit on the server = %v, want the one that was written (%s)", pushed, written)
	}
}

func TestACommitWithoutAnAuthorIsNotSentToTheServer(t *testing.T) {
	dir := t.TempDir()
	server := filepath.Join(dir, "server")
	local := filepath.Join(dir, "local")
	before := initRemoteServerRepo(t, server, "main")

	a := newRemoteTestApp(t)
	views := captureOperationViews(t)
	cloneIntoRegistry(t, a, server, local)
	addNamelessCommit(t, local, "main")

	a.startPush()
	view := lastOperationView(t, views)
	waitForFinishedOperation(t, a, view)

	got, ok := readRemoteRef(t, server, refs.BranchName("main"))
	if !ok || got != before {
		t.Fatalf("server branch = %v, want it left at %s", got, before)
	}
}

func addNamelessCommit(t *testing.T, dir, branch string) hash.ObjectID {
	t.Helper()
	r, err := gitrepo.Open(dir, gitrepo.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	db, err := odb.Open(r.ObjectsDir(), odb.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	store, err := refs.Open(refs.Options{GitDir: r.GitDir(), CommonDir: r.CommonDir(), Committer: testCommitter})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()

	head, err := store.Lookup(refs.BranchName(branch))
	if err != nil {
		t.Fatal(err)
	}
	signed := testCommitter()
	commit := &object.Commit{
		Tree:      putChangesTree(t, db, map[string]string{"nameless.txt": "nameless\n"}),
		Parents:   []hash.ObjectID{head.Target},
		Author:    object.Signature{When: signed.When},
		Committer: signed,
		Message:   "nameless\n",
	}
	id, err := db.PutObject(commit)
	if err != nil {
		t.Fatal(err)
	}
	setRef(t, store, refs.BranchName(branch), id)
	return id
}

func TestAPushStopsWhenTheHistoryCannotBeCheckedForAttribution(t *testing.T) {
	dir := t.TempDir()
	server := filepath.Join(dir, "server")
	local := filepath.Join(dir, "local")
	before := initRemoteServerRepo(t, server, "main")

	a := newRemoteTestApp(t)
	views := captureOperationViews(t)
	cloneIntoRegistry(t, a, server, local)
	addLocalCommitWithMessage(t, local, "main", "mine.txt", "mine\n", "mine\n")
	broken := filepath.Join(local, ".git", "refs", "remotes", "origin", "broken")
	if err := os.MkdirAll(filepath.Dir(broken), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(broken, []byte("not a hash\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	a.startPush()
	view := lastOperationView(t, views)
	waitForFinishedOperation(t, a, view)

	got, ok := readRemoteRef(t, server, refs.BranchName("main"))
	if !ok || got != before {
		t.Fatalf("server branch = %v, want it left at %s", got, before)
	}
}
