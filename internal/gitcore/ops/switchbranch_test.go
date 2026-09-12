package ops

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/refs"
)

func (r *testRepo) remoteBranch(name string, commit hash.ObjectID) refs.Name {
	r.t.Helper()
	ref := refs.Name(refs.RemotesPrefix + name)
	store := r.refs()
	tx := store.Begin()
	if err := tx.Set(ref, commit); err != nil {
		r.t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		r.t.Fatal(err)
	}
	return ref
}

func TestStartingABranchFromTheCurrentHeadSwitchesToIt(t *testing.T) {
	tr := newTestRepo(t)
	head := tr.commitFiles("base", map[string]string{"f": "f\n"})

	result, err := StartBranch(t.Context(), tr.repo, "topic", "", StartBranchOptions{})

	if err != nil {
		t.Fatalf("StartBranch returned error %v", err)
	}
	if !result.Created || result.Start != head || result.Branch != refs.BranchName("topic") {
		t.Fatalf("result = %+v", result)
	}
	if branch, ok := tr.headSymbolicTarget(); !ok || branch != refs.BranchName("topic") {
		t.Fatalf("HEAD = %s, %v", branch, ok)
	}
}

func TestStartingABranchFromARemoteOneTracksIt(t *testing.T) {
	tr := newTestRepo(t)
	head := tr.commitFiles("base", map[string]string{"f": "f\n"})
	tr.appendConfig("[remote \"origin\"]\n\turl = https://example.invalid/repo.git\n\tfetch = +refs/heads/*:refs/remotes/origin/*\n")
	tr.repo = tr.reopen()
	tr.remoteBranch("origin/topic", head)

	result, err := StartBranch(t.Context(), tr.repo, "topic", "origin/topic", StartBranchOptions{Track: true})

	if err != nil {
		t.Fatalf("StartBranch returned error %v", err)
	}
	if result.Upstream != refs.Name("refs/remotes/origin/topic") {
		t.Fatalf("result = %+v", result)
	}
	cfg := tr.reopen().Config()
	if got, _ := cfg.Get("branch.topic.remote"); got != "origin" {
		t.Fatalf("remote = %q", got)
	}
	if got, _ := cfg.Get("branch.topic.merge"); got != "refs/heads/topic" {
		t.Fatalf("merge = %q", got)
	}
}

func TestStartingABranchFromAFullRemoteNameWorksToo(t *testing.T) {
	tr := newTestRepo(t)
	head := tr.commitFiles("base", map[string]string{"f": "f\n"})
	tr.appendConfig("[remote \"origin\"]\n\turl = https://example.invalid/repo.git\n")
	tr.repo = tr.reopen()
	tr.remoteBranch("origin/topic", head)

	result, err := StartBranch(t.Context(), tr.repo, "topic", "refs/remotes/origin/topic", StartBranchOptions{Track: true})

	if err != nil || result.Upstream != refs.Name("refs/remotes/origin/topic") {
		t.Fatalf("result = %+v, %v", result, err)
	}
}

func TestStartingABranchWithoutTrackingLeavesTheConfigAlone(t *testing.T) {
	tr := newTestRepo(t)
	head := tr.commitFiles("base", map[string]string{"f": "f\n"})
	tr.remoteBranch("origin/topic", head)

	result, err := StartBranch(t.Context(), tr.repo, "topic", "origin/topic", StartBranchOptions{})

	if err != nil || result.Upstream != "" {
		t.Fatalf("result = %+v, %v", result, err)
	}
	if got, _ := tr.reopen().Config().Get("branch.topic.remote"); got != "" {
		t.Fatalf("remote = %q", got)
	}
}

func TestTrackingARemoteWithoutARemoteEntryFails(t *testing.T) {
	tr := newTestRepo(t)
	head := tr.commitFiles("base", map[string]string{"f": "f\n"})
	tr.remoteBranch("nowhere/topic", head)

	if _, err := StartBranch(t.Context(), tr.repo, "topic", "nowhere/topic", StartBranchOptions{Track: true}); !errors.Is(err, ErrNoUpstream) {
		t.Fatalf("err = %v", err)
	}
}

func TestStartingABranchFromACommitName(t *testing.T) {
	tr := newTestRepo(t)
	base := tr.commitFiles("base", map[string]string{"f": "f\n"})
	tr.commitFiles("next", map[string]string{"f": "next\n"})

	result, err := StartBranch(t.Context(), tr.repo, "topic", base.String(), StartBranchOptions{})

	if err != nil || result.Start != base {
		t.Fatalf("result = %+v, %v", result, err)
	}
	if tr.readFile("f") != "f\n" {
		t.Fatalf("f = %q", tr.readFile("f"))
	}
}

func TestStartingABranchThatExistsNeedsForce(t *testing.T) {
	tr := newTestRepo(t)
	base := tr.commitFiles("base", map[string]string{"f": "f\n"})
	next := tr.commitFiles("next", map[string]string{"f": "next\n"})
	tr.createBranch("topic", base)

	if _, err := StartBranch(t.Context(), tr.repo, "topic", next.String(), StartBranchOptions{}); !errors.Is(err, ErrBranchExists) {
		t.Fatalf("err = %v", err)
	}
	if _, err := StartBranch(t.Context(), tr.repo, "topic", next.String(), StartBranchOptions{Force: true}); err != nil {
		t.Fatalf("forced StartBranch returned error %v", err)
	}
	if tr.branchTarget("topic") != next {
		t.Fatal("the forced branch did not move")
	}
}

func TestStartingABranchWithABadNameFails(t *testing.T) {
	tr := newTestRepo(t)
	tr.commitFiles("base", map[string]string{"f": "f\n"})

	if _, err := StartBranch(t.Context(), tr.repo, "bad..name", "", StartBranchOptions{}); !errors.Is(err, ErrInvalidBranchName) {
		t.Fatalf("err = %v", err)
	}
}

func TestStartingABranchFromAnUnknownPlaceFails(t *testing.T) {
	tr := newTestRepo(t)
	tr.commitFiles("base", map[string]string{"f": "f\n"})

	if _, err := StartBranch(t.Context(), tr.repo, "topic", "nope", StartBranchOptions{}); !errors.Is(err, ErrTargetNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestStartingABranchOnAnUnbornHeadFails(t *testing.T) {
	tr := newTestRepo(t)

	if _, err := StartBranch(t.Context(), tr.repo, "topic", "", StartBranchOptions{}); !errors.Is(err, ErrUnbornHead) {
		t.Fatalf("err = %v", err)
	}
}

func TestStartingABranchHonoursACancelledContext(t *testing.T) {
	tr := newTestRepo(t)
	tr.commitFiles("base", map[string]string{"f": "f\n"})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, err := StartBranch(ctx, tr.repo, "topic", "", StartBranchOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}

func TestTrackingThatCannotBeWrittenIsReported(t *testing.T) {
	tr := newTestRepo(t)
	head := tr.commitFiles("base", map[string]string{"f": "f\n"})
	tr.appendConfig("[remote \"origin\"]\n\turl = https://example.invalid/repo.git\n")
	tr.repo = tr.reopen()
	tr.remoteBranch("origin/topic", head)
	config := filepath.Join(tr.repo.GitDir(), "config")
	if err := os.Remove(config); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(config, 0o777); err != nil {
		t.Fatal(err)
	}

	if _, err := StartBranch(t.Context(), tr.repo, "topic", "origin/topic", StartBranchOptions{Track: true}); err == nil {
		t.Fatal("a config that cannot be written was ignored")
	}
}
