package ops

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/refs"
)

func messageOf(t testing.TB, r *testRepo, id hash.ObjectID) string {
	t.Helper()
	commit, err := r.db().Commit(id)
	if err != nil {
		t.Fatalf("Commit(%s) returned error %v", id, err)
	}
	return commit.Message
}

func sameSignature(a, b object.Signature) bool {
	return a.Name == b.Name && a.Email == b.Email && a.When.Equal(b.When)
}

func commitOf(t testing.TB, r *testRepo, id hash.ObjectID) *object.Commit {
	t.Helper()
	commit, err := r.db().Commit(id)
	if err != nil {
		t.Fatalf("Commit(%s) returned error %v", id, err)
	}
	return commit
}

func TestATrailerThatNamesSomebodyElseIsDroppedBeforeTheCommitIsShared(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "one\n")
	mustStage(t, r, "a.txt")
	old := r.commitAll("work\n\nCo-authored-by: Bob <bob@example.com>")

	result, err := StripAttribution(t.Context(), r.repo, refs.BranchName("main"))
	if err != nil {
		t.Fatalf("StripAttribution returned error %v", err)
	}

	if result.Rewritten != 1 || result.New == old || result.Old != old {
		t.Fatalf("result = %+v, want the single commit rewritten", result)
	}
	if got := r.branchTarget("main"); got != result.New {
		t.Fatalf("main = %s, want the rewritten commit %s", got, result.New)
	}
	if got := messageOf(t, r, result.New); got != "work\n" {
		t.Fatalf("message = %q, want the trailer gone", got)
	}
	was, now := commitOf(t, r, old), commitOf(t, r, result.New)
	if now.Tree != was.Tree || !sameSignature(now.Author, was.Author) || !sameSignature(now.Committer, was.Committer) {
		t.Fatalf("commit = %+v, want only the message changed against %+v", now, was)
	}
}

func TestACleanHistoryIsLeftExactlyWhereItIs(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "one\n")
	mustStage(t, r, "a.txt")
	head := r.commitAll("work\n\nFixes: #12")

	result, err := StripAttribution(t.Context(), r.repo, refs.BranchName("main"))
	if err != nil {
		t.Fatalf("StripAttribution returned error %v", err)
	}

	if result.Rewritten != 0 || result.New != head || r.branchTarget("main") != head {
		t.Fatalf("result = %+v, want the branch untouched at %s", result, head)
	}
}

func TestTheCommitsAfterARewrittenOneFollowItToTheirNewParent(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "one\n")
	mustStage(t, r, "a.txt")
	r.commitAll("first\n\nHelped-by: Bob <bob@example.com>")
	r.writeFile("b.txt", "two\n")
	mustStage(t, r, "b.txt")
	second := r.commitAll("second")

	result, err := StripAttribution(t.Context(), r.repo, refs.BranchName("main"))
	if err != nil {
		t.Fatalf("StripAttribution returned error %v", err)
	}

	if result.Rewritten != 2 {
		t.Fatalf("rewritten = %d, want the commit and the one standing on it", result.Rewritten)
	}
	tip := commitOf(t, r, result.New)
	if tip.Message != messageOf(t, r, second) {
		t.Fatalf("message of the tip = %q, want it unchanged", tip.Message)
	}
	if got := messageOf(t, r, tip.Parents[0]); got != "first\n" {
		t.Fatalf("parent message = %q, want the trailer gone", got)
	}
}

func TestCommitsTheServerAlreadyHasAreNotRewritten(t *testing.T) {
	src := newFetchServer(t)
	client := cloneForFetch(t, src)
	published := client.branchTarget("main")

	client.writeFile("c.txt", "mine\n")
	mustStage(t, client, "c.txt")
	client.commitAll("mine\n\nSuggested-by: Bob <bob@example.com>")

	result, err := StripAttribution(t.Context(), client.repo, refs.BranchName("main"))
	if err != nil {
		t.Fatalf("StripAttribution returned error %v", err)
	}

	if result.Rewritten != 1 {
		t.Fatalf("rewritten = %d, want only the commit that was never pushed", result.Rewritten)
	}
	tip := commitOf(t, client, result.New)
	if tip.Parents[0] != published {
		t.Fatalf("parent = %s, want the published commit %s left as it is", tip.Parents[0], published)
	}
}

func TestACommitWithoutAnAuthorIsNotLetThrough(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "one\n")
	mustStage(t, r, "a.txt")
	parent := r.commitAll("first")

	db := r.db()
	tree := commitOf(t, r, parent).Tree
	nameless := &object.Commit{
		Tree:      tree,
		Parents:   []hash.ObjectID{parent},
		Author:    object.Signature{When: time.Unix(1700001000, 0)},
		Committer: object.Signature{Name: "ann", Email: "ann@example.com", When: time.Unix(1700001000, 0)},
		Message:   "nameless\n",
	}
	id, err := db.PutObject(nameless)
	if err != nil {
		t.Fatal(err)
	}
	store := r.refs()
	tx := store.Begin()
	if err := tx.Set(refs.BranchName("main"), id); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	_, err = StripAttribution(t.Context(), r.repo, refs.BranchName("main"))

	if !errors.Is(err, ErrUnsignedCommit) {
		t.Fatalf("err = %v, want ErrUnsignedCommit", err)
	}
	if got := r.branchTarget("main"); got != id {
		t.Fatalf("main = %s, want the branch left alone at %s", got, id)
	}
}

func TestABranchThatIsNotThereIsNothingToClean(t *testing.T) {
	r := newTestRepo(t)

	result, err := StripAttribution(t.Context(), r.repo, refs.BranchName("main"))

	if err != nil || result.Rewritten != 0 || !result.New.IsZero() {
		t.Fatalf("result = %+v, err = %v, want nothing to do", result, err)
	}
}

func TestTheSignatureOfARewrittenCommitIsDropped(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "one\n")
	mustStage(t, r, "a.txt")
	parent := r.commitAll("first")

	db := r.db()
	signed := &object.Commit{
		Tree:         commitOf(t, r, parent).Tree,
		Parents:      []hash.ObjectID{parent},
		Author:       object.Signature{Name: "ann", Email: "ann@example.com", When: time.Unix(1700002000, 0)},
		Committer:    object.Signature{Name: "ann", Email: "ann@example.com", When: time.Unix(1700002000, 0)},
		GPGSignature: "-----BEGIN PGP SIGNATURE-----\nnot a real one\n-----END PGP SIGNATURE-----",
		Message:      "signed\n\nCo-authored-by: Bob <bob@example.com>\n",
	}
	id, err := db.PutObject(signed)
	if err != nil {
		t.Fatal(err)
	}
	store := r.refs()
	tx := store.Begin()
	if err := tx.Set(refs.BranchName("main"), id); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	result, err := StripAttribution(t.Context(), r.repo, refs.BranchName("main"))
	if err != nil {
		t.Fatalf("StripAttribution returned error %v", err)
	}

	if got := commitOf(t, r, result.New); got.GPGSignature != "" {
		t.Fatal("a signature of a message that changed must not be carried over")
	}
}

func writeRefFile(t *testing.T, r *testRepo, rel, content string) {
	t.Helper()
	path := filepath.Join(r.repo.GitDir(), rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestStrippingReportsWhatWentWrongOnTheWay(t *testing.T) {
	withAttribution := func(t *testing.T) *testRepo {
		t.Helper()
		r := newTestRepo(t)
		r.writeFile("a.txt", "one\n")
		mustStage(t, r, "a.txt")
		r.commitAll("work\n\nCo-authored-by: Bob <bob@example.com>")
		return r
	}

	t.Run("the objects cannot be opened", func(t *testing.T) {
		r := withAttribution(t)
		swapOdbOpenFailOnCall(t, 1)

		if _, err := StripAttribution(t.Context(), r.repo, refs.BranchName("main")); !errors.Is(err, errInjected) {
			t.Fatalf("err = %v, want the injected failure", err)
		}
	})

	t.Run("the branch cannot be read", func(t *testing.T) {
		r := withAttribution(t)
		writeRefFile(t, r, filepath.Join("refs", "heads", "main"), "not a hash\n")

		if _, err := StripAttribution(t.Context(), r.repo, refs.BranchName("main")); err == nil {
			t.Fatal("a malformed branch must be reported")
		}
	})

	t.Run("the tracking refs cannot be read", func(t *testing.T) {
		r := withAttribution(t)
		writeRefFile(t, r, filepath.Join("refs", "remotes", "origin", "main"), "not a hash\n")

		if _, err := StripAttribution(t.Context(), r.repo, refs.BranchName("main")); err == nil {
			t.Fatal("a malformed tracking ref must be reported")
		}
	})

	t.Run("the shallow file cannot be read", func(t *testing.T) {
		r := withAttribution(t)
		writeRefFile(t, r, "shallow", "not a hash\n")

		if _, err := StripAttribution(t.Context(), r.repo, refs.BranchName("main")); err == nil {
			t.Fatal("a malformed shallow file must be reported")
		}
	})

	t.Run("the rewritten commit cannot be written", func(t *testing.T) {
		r := withAttribution(t)
		swapDBPutObject(t, func(*odb.DB, object.Object) (hash.ObjectID, error) { return hash.Zero, errInjected })

		if _, err := StripAttribution(t.Context(), r.repo, refs.BranchName("main")); !errors.Is(err, errInjected) {
			t.Fatalf("err = %v, want the injected failure", err)
		}
	})

	t.Run("the history cannot be read", func(t *testing.T) {
		r := withAttribution(t)
		missing := hash.SumSHA1(object.TypeCommit.String(), []byte("no such commit"))
		writeRefFile(t, r, filepath.Join("refs", "heads", "main"), missing.String()+"\n")

		if _, err := StripAttribution(t.Context(), r.repo, refs.BranchName("main")); err == nil {
			t.Fatal("a history that cannot be read must be reported")
		}
	})

	t.Run("the branch is locked by somebody else", func(t *testing.T) {
		r := withAttribution(t)
		writeRefFile(t, r, filepath.Join("refs", "heads", "main.lock"), "")

		if _, err := StripAttribution(t.Context(), r.repo, refs.BranchName("main")); err == nil {
			t.Fatal("a locked branch must be reported")
		}
	})

	t.Run("the branch cannot be moved", func(t *testing.T) {
		r := withAttribution(t)
		swapTxUpdate(t, func(*refs.Transaction, refs.Name, hash.ObjectID, hash.ObjectID) error { return errInjected })

		if _, err := StripAttribution(t.Context(), r.repo, refs.BranchName("main")); !errors.Is(err, errInjected) {
			t.Fatalf("err = %v, want the injected failure", err)
		}
	})
}
