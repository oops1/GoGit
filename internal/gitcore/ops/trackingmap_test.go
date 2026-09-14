package ops

import (
	"errors"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/config"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/remote"
)

func replaceFetchRefspecs(t *testing.T, r *testRepo, name string, specs ...string) {
	t.Helper()
	file, ok := r.repo.Config().File(config.LevelLocal)
	if !ok {
		t.Fatal("no local config")
	}
	_ = file.UnsetAll("remote." + name + ".fetch")
	for _, spec := range specs {
		if err := file.Add("remote."+name+".fetch", spec); err != nil {
			t.Fatal(err)
		}
	}
	if err := file.Save(file.Path()); err != nil {
		t.Fatal(err)
	}
	r.repo = r.reopen()
}

func TestPushSetsTheUpstreamOfTheBranchThatWasPushed(t *testing.T) {
	server := newTestRepo(t)
	client := newTestRepo(t)
	client.writeFile("a.txt", "hello\n")
	mustStage(t, client, "a.txt")
	client.commitAll("initial")
	if err := AddRemote(client.repo, "origin", server.dir); err != nil {
		t.Fatal(err)
	}
	client.repo = client.reopen()

	if _, err := Push(t.Context(), client.repo, "", remote.PushOptions{
		Refspecs: mustPushSpecs(t, "refs/heads/main:refs/heads/published"),
	}); err != nil {
		t.Fatalf("Push returned error %v", err)
	}

	cfg := client.reopen().Config()
	if got, _ := cfg.Get("branch.main.remote"); got != "origin" {
		t.Fatalf("branch.main.remote = %q, want origin", got)
	}
	if got, _ := cfg.Get("branch.main.merge"); got != "refs/heads/published" {
		t.Fatalf("branch.main.merge = %q, want refs/heads/published", got)
	}
	if _, ok := cfg.Branch("published"); ok {
		t.Fatal("a config section appeared for a branch that does not exist locally")
	}
}

func TestPullReadsTheTrackingRefItsFetchRefspecNames(t *testing.T) {
	src := newFetchServer(t)
	client := cloneForFetch(t, src)
	replaceFetchRefspecs(t, client, "origin", "+refs/heads/*:refs/remotes/mirror/*")
	src.writeFile("a.txt", "hello v2\n")
	mustStage(t, src, "a.txt")
	second := src.commitAll("second")

	result, err := Pull(t.Context(), client.repo, PullOptions{})
	if err != nil {
		t.Fatalf("Pull returned error %v", err)
	}
	if !result.Updated || result.New != second {
		t.Fatalf("Pull result = %+v, want main moved to %s", result, second)
	}
	if got := client.branchTargetIn(refs.Name("refs/remotes/mirror/main")); got != second {
		t.Fatalf("mirror/main = %s", got)
	}
}

func TestPullFailsWhenNoFetchRefspecCoversTheMergeRef(t *testing.T) {
	src := newFetchServer(t)
	client := cloneForFetch(t, src)
	replaceFetchRefspecs(t, client, "origin", "+refs/heads/other:refs/remotes/origin/other")

	if _, err := Pull(t.Context(), client.repo, PullOptions{}); !errors.Is(err, ErrNoUpstream) {
		t.Fatalf("Pull returned %v, want %v", err, ErrNoUpstream)
	}
}

func TestStartingABranchMapsItsUpstreamThroughFetchRefspecs(t *testing.T) {
	tr := newTestRepo(t)
	head := tr.commitFiles("base", map[string]string{"f": "f\n"})
	tr.appendConfig("[remote \"origin\"]\n\turl = https://example.invalid/repo.git\n\tfetch = +refs/heads/*:refs/remotes/mirror/*\n" +
		"[remote \"tagged\"]\n\turl = https://example.invalid/tags.git\n\tfetch = +refs/tags/*:refs/remotes/tagged/*\n")
	tr.repo = tr.reopen()
	tr.remoteBranch("mirror/topic", head)
	tr.remoteBranch("tagged/v1", head)

	if _, err := StartBranch(t.Context(), tr.repo, "topic", "mirror/topic", StartBranchOptions{Track: true}); err != nil {
		t.Fatalf("StartBranch returned error %v", err)
	}
	cfg := tr.reopen().Config()
	if got, _ := cfg.Get("branch.topic.remote"); got != "origin" {
		t.Fatalf("branch.topic.remote = %q, want origin", got)
	}
	if got, _ := cfg.Get("branch.topic.merge"); got != "refs/heads/topic" {
		t.Fatalf("branch.topic.merge = %q", got)
	}

	if _, err := StartBranch(t.Context(), tr.reopen(), "release", "tagged/v1", StartBranchOptions{Track: true}); !errors.Is(err, ErrNoUpstream) {
		t.Fatalf("StartBranch from a tag mirror returned %v, want %v", err, ErrNoUpstream)
	}
}
