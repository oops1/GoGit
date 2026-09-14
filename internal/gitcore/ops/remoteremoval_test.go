package ops

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/refs"
)

func TestRemoveRemoteForgetsItsTrackingRefsAndUpstreams(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	first := r.commitAll("initial")
	if err := AddRemote(r.repo, "origin", "https://example.com/a.git"); err != nil {
		t.Fatal(err)
	}
	if err := AddRemote(r.repo, "other", "https://example.com/b.git"); err != nil {
		t.Fatal(err)
	}
	r.appendConfig("[branch \"main\"]\n\tremote = origin\n\tmerge = refs/heads/main\n" +
		"[branch \"side\"]\n\tremote = other\n\tmerge = refs/heads/side\n\tpushRemote = origin\n" +
		"[remote]\n\tpushDefault = origin\n")
	store := r.refs()
	tx := store.Begin()
	for _, name := range []string{"refs/remotes/origin/main", "refs/remotes/origin/feature/x", "refs/remotes/other/main"} {
		if err := tx.Set(refs.Name(name), first); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.SetSymbolic("refs/remotes/origin/HEAD", "refs/remotes/origin/main"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	_ = store.Close()

	reopened := r.reopen()
	if err := RemoveRemote(reopened, "origin"); err != nil {
		t.Fatalf("RemoveRemote returned error %v", err)
	}

	after := r.refs()
	for _, gone := range []string{"refs/remotes/origin/main", "refs/remotes/origin/feature/x", "refs/remotes/origin/HEAD"} {
		if _, err := after.Lookup(refs.Name(gone)); !errors.Is(err, refs.ErrNotFound) {
			t.Fatalf("%s is still there: %v", gone, err)
		}
	}
	if got := r.branchTargetIn("refs/remotes/other/main"); got != first {
		t.Fatalf("the other remote lost its tracking ref: %s", got)
	}
	cfg := r.reopen().Config()
	for _, key := range []string{"branch.main.remote", "branch.main.merge", "branch.side.pushRemote", "remote.pushDefault"} {
		if value, ok := cfg.Get(key); ok {
			t.Fatalf("%s = %q survived the removal", key, value)
		}
	}
	if value, _ := cfg.Get("branch.side.remote"); value != "other" {
		t.Fatalf("branch.side.remote = %q, want other", value)
	}
}

func TestRemoveRemoteKeepsRefsWhenItsFetchRefspecIsBroken(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	first := r.commitAll("initial")
	r.appendConfig("[remote \"odd\"]\n\turl = https://example.com/a.git\n\tfetch = refs/heads/*:refs/remotes/odd/x\n")
	r.createBranch("kept", first)
	if err := RemoveRemote(r.reopen(), "odd"); err != nil {
		t.Fatalf("RemoveRemote returned error %v", err)
	}
	if got := r.branchTarget("kept"); got != first {
		t.Fatalf("kept = %s", got)
	}
}

func TestRemoveRemoteReportsRefsThatCannotBeOpenedOrRead(t *testing.T) {
	r := newTestRepo(t)
	if err := AddRemote(r.repo, "origin", "https://example.com/a.git"); err != nil {
		t.Fatal(err)
	}
	broken := filepath.Join(r.repo.CommonDir(), "refs", "remotes", "origin", "bad")
	if err := os.MkdirAll(filepath.Dir(broken), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(broken, []byte("not a ref\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RemoveRemote(r.reopen(), "origin"); err == nil {
		t.Fatal("RemoveRemote hid an unreadable tracking ref")
	}

	if err := AddRemote(r.reopen(), "second", "https://example.com/b.git"); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("refs unavailable")
	original := refsOpen
	refsOpen = func(refs.Options) (*refs.Store, error) { return nil, failure }
	t.Cleanup(func() { refsOpen = original })
	if err := RemoveRemote(r.reopen(), "second"); !errors.Is(err, failure) {
		t.Fatalf("RemoveRemote returned %v, want %v", err, failure)
	}
}

func TestRenameBranchCarriesItsConfigSection(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	first := r.commitAll("initial")
	r.createBranch("feature", first)
	r.createBranch("other", first)
	r.appendConfig("[branch \"feature\"]\n\tremote = origin\n\tmerge = refs/heads/feature\n" +
		"[branch \"other\"]\n\tremote = stale\n")

	if err := RenameBranch(t.Context(), r.reopen(), "feature", "other", true); err != nil {
		t.Fatalf("RenameBranch returned error %v", err)
	}
	cfg := r.reopen().Config()
	if _, ok := cfg.Branch("feature"); ok {
		t.Fatal("the old branch section is still there")
	}
	branch, ok := cfg.Branch("other")
	if !ok || branch.Remote != "origin" || len(branch.Merge) != 1 || branch.Merge[0] != "refs/heads/feature" {
		t.Fatalf("branch.other = %+v, want the settings of feature", branch)
	}

	if err := RenameBranch(t.Context(), r.reopen(), "other", "plain", false); err != nil {
		t.Fatal(err)
	}
	if err := RenameBranch(t.Context(), r.reopen(), "main", "trunk", false); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(r.repo.CommonPath("config")); err != nil {
		t.Fatal(err)
	}
	if err := RenameBranch(t.Context(), r.reopen(), "trunk", "main", false); err != nil {
		t.Fatalf("RenameBranch without a local config returned %v", err)
	}
}
