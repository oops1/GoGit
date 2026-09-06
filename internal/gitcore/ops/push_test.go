package ops

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/refspec"
	"github.com/oops1/gogit/internal/gitcore/remote"
	"github.com/oops1/gogit/internal/gitcore/repo"
)

func mustPushSpecs(t testing.TB, texts ...string) []refspec.RefSpec {
	t.Helper()
	specs, err := refspec.ParseAll(texts)
	if err != nil {
		t.Fatalf("refspec.ParseAll returned error %v", err)
	}
	return specs
}

func TestPushSendsCommitsAndMovesRemoteTracking(t *testing.T) {
	src := newFetchServer(t)
	client := cloneForFetch(t, src)

	client.writeFile("c.txt", "new\n")
	mustStage(t, client, "c.txt")
	commit := client.commitAll("client commit")

	result, err := Push(t.Context(), client.repo, "origin", remote.PushOptions{
		Refspecs: mustPushSpecs(t, "refs/heads/main:refs/heads/main"),
	})
	if err != nil {
		t.Fatalf("Push returned error %v", err)
	}
	if len(result.Changes) != 1 || result.Changes[0].New != commit || result.Changes[0].Created {
		t.Fatalf("Push returned changes %+v", result.Changes)
	}
	if got := src.branchTargetIn(refs.BranchName("main")); got != commit {
		t.Fatalf("server main = %s, want %s", got, commit)
	}
	if got := client.branchTargetIn(refs.RemoteBranchName("origin", "main")); got != commit {
		t.Fatalf("client origin/main = %s, want %s", got, commit)
	}
	reopened := client.reopen()
	if remoteName, _ := reopened.Config().Get("branch.main.remote"); remoteName != "origin" {
		t.Fatalf("branch.main.remote = %q, want unchanged origin", remoteName)
	}
}

func TestPushWithoutRefspecsIsANoOp(t *testing.T) {
	src := newFetchServer(t)
	client := cloneForFetch(t, src)

	result, err := Push(t.Context(), client.repo, "", remote.PushOptions{})
	if err != nil {
		t.Fatalf("Push returned error %v", err)
	}
	if len(result.Changes) != 0 {
		t.Fatalf("Push returned changes %+v, want none", result.Changes)
	}
}

func TestPushSetsUpstreamOnFirstPushOfANewBranch(t *testing.T) {
	server := newTestRepo(t)
	client := newTestRepo(t)
	client.writeFile("a.txt", "hello\n")
	mustStage(t, client, "a.txt")
	commit := client.commitAll("initial")
	if err := AddRemote(client.repo, "origin", server.dir); err != nil {
		t.Fatalf("AddRemote returned error %v", err)
	}
	client.repo = client.reopen()

	result, err := Push(t.Context(), client.repo, "", remote.PushOptions{
		Refspecs: mustPushSpecs(t, "refs/heads/main:refs/heads/main"),
	})
	if err != nil {
		t.Fatalf("Push returned error %v", err)
	}
	if len(result.Changes) != 1 || !result.Changes[0].Created || result.Changes[0].New != commit {
		t.Fatalf("Push returned changes %+v", result.Changes)
	}
	if got := server.branchTargetIn(refs.BranchName("main")); got != commit {
		t.Fatalf("server main = %s, want %s", got, commit)
	}

	reopened := client.reopen()
	if remoteName, _ := reopened.Config().Get("branch.main.remote"); remoteName != "origin" {
		t.Fatalf("branch.main.remote = %q, want origin", remoteName)
	}
	if merge, _ := reopened.Config().Get("branch.main.merge"); merge != "refs/heads/main" {
		t.Fatalf("branch.main.merge = %q, want refs/heads/main", merge)
	}
}

func TestPushDoesNotOverwriteAnExistingUpstreamConfig(t *testing.T) {
	server := newTestRepo(t)
	client := newTestRepo(t)
	client.writeFile("a.txt", "hello\n")
	mustStage(t, client, "a.txt")
	client.commitAll("initial")
	client.createBranch("feature", client.branchTargetIn(refs.BranchName("main")))
	if err := AddRemote(client.repo, "origin", server.dir); err != nil {
		t.Fatalf("AddRemote returned error %v", err)
	}
	client.appendConfig("[branch \"feature\"]\n\tremote = elsewhere\n\tmerge = refs/heads/other\n")
	client.repo = client.reopen()

	result, err := Push(t.Context(), client.repo, "", remote.PushOptions{
		Refspecs: mustPushSpecs(t, "refs/heads/feature:refs/heads/feature"),
	})
	if err != nil {
		t.Fatalf("Push returned error %v", err)
	}
	if len(result.Changes) != 1 || !result.Changes[0].Created {
		t.Fatalf("Push returned changes %+v, want a single created change", result.Changes)
	}

	reopened := client.reopen()
	if remoteName, _ := reopened.Config().Get("branch.feature.remote"); remoteName != "elsewhere" {
		t.Fatalf("branch.feature.remote = %q, want unchanged elsewhere", remoteName)
	}
}

func TestPushRejectsNonFastForwardUpdate(t *testing.T) {
	src := newFetchServer(t)
	clientA := cloneForFetch(t, src)
	clientB := cloneForFetch(t, src)
	specs := mustPushSpecs(t, "refs/heads/main:refs/heads/main")

	clientA.writeFile("a.txt", "from A\n")
	mustStage(t, clientA, "a.txt")
	first := clientA.commitAll("A's commit")
	if _, err := Push(t.Context(), clientA.repo, "", remote.PushOptions{Refspecs: specs}); err != nil {
		t.Fatalf("Push (A) returned error %v", err)
	}

	if _, err := Fetch(t.Context(), clientB.repo, "", remote.FetchOptions{}); err != nil {
		t.Fatalf("Fetch (B) returned error %v", err)
	}
	clientB.writeFile("b.txt", "from B\n")
	mustStage(t, clientB, "b.txt")
	clientB.commitAll("B's divergent commit")

	_, err := Push(t.Context(), clientB.repo, "", remote.PushOptions{Refspecs: specs})
	if !errors.Is(err, remote.ErrNonFastForward) {
		t.Fatalf("Push (B) returned %v, want %v", err, remote.ErrNonFastForward)
	}
	if got := src.branchTargetIn(refs.BranchName("main")); got != first {
		t.Fatalf("server main = %s, want unchanged %s", got, first)
	}
}

func TestPushFailsWhenCurrentBranchRefsCannotBeOpened(t *testing.T) {
	src := newFetchServer(t)
	client := cloneForFetch(t, src)
	swapRefsOpen(t, func(refs.Options) (*refs.Store, error) { return nil, errInjected })

	_, err := Push(t.Context(), client.repo, "", remote.PushOptions{})
	if !errors.Is(err, errInjected) {
		t.Fatalf("Push returned %v, want %v", err, errInjected)
	}
}

func TestPushPropagatesUpstreamConfigWriteFailures(t *testing.T) {
	server := newTestRepo(t)
	client := newTestRepo(t)
	client.writeFile("a.txt", "hello\n")
	mustStage(t, client, "a.txt")
	client.commitAll("initial")

	serverURL := strings.ReplaceAll(server.dir, "\\", "/")
	remoteConfig := "[remote \"origin\"]\n\turl = " + serverURL + "\n\tfetch = +refs/heads/*:refs/remotes/origin/*\n"
	if err := os.WriteFile(client.globalFile, []byte(remoteConfig), 0o666); err != nil {
		t.Fatalf("WriteFile returned error %v", err)
	}
	if err := os.Remove(client.repo.CommonPath("config")); err != nil {
		t.Fatalf("Remove returned error %v", err)
	}
	client.repo = client.reopen()

	_, err := Push(t.Context(), client.repo, "", remote.PushOptions{
		Refspecs: mustPushSpecs(t, "refs/heads/main:refs/heads/main"),
	})
	if !errors.Is(err, ErrNoLocalConfig) {
		t.Fatalf("Push returned %v, want %v", err, ErrNoLocalConfig)
	}
}

func TestPushFailsWhenTheNetworkViewCannotBeOpened(t *testing.T) {
	src := newFetchServer(t)
	client := cloneForFetch(t, src)
	swapCloneRepoOpenLayout(t, func(repo.Layout, repo.OpenOptions) (*repo.Repository, error) { return nil, errInjected })

	_, err := Push(t.Context(), client.repo, "", remote.PushOptions{})
	if !errors.Is(err, errInjected) {
		t.Fatalf("Push returned %v, want %v", err, errInjected)
	}
}

func TestPushFailsWhenRemoteNameIsUnknown(t *testing.T) {
	src := newFetchServer(t)
	client := cloneForFetch(t, src)

	_, err := Push(t.Context(), client.repo, "does-not-exist", remote.PushOptions{})
	if !errors.Is(err, remote.ErrNoRemote) {
		t.Fatalf("Push returned %v, want %v", err, remote.ErrNoRemote)
	}
}

func TestPushFailsWhenContextIsAlreadyCanceled(t *testing.T) {
	src := newFetchServer(t)
	client := cloneForFetch(t, src)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := Push(ctx, client.repo, "", remote.PushOptions{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Push returned %v, want %v", err, context.Canceled)
	}
}
