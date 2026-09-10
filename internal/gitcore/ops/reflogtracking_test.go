package ops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/remote"
)

func trackingReflog(t testing.TB, r *testRepo, name refs.Name) string {
	t.Helper()
	path := filepath.Join(r.repo.CommonDir(), "logs", filepath.FromSlash(string(name)))
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		t.Fatal(err)
	}
	return string(data)
}

func startTrackingReflog(t testing.TB, r *testRepo, name refs.Name) {
	t.Helper()
	path := filepath.Join(r.repo.CommonDir(), "logs", filepath.FromSlash(string(name)))
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err == nil {
		return
	}
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestPushMovesTheTrackingRefOfARepositoryThatKeepsAReflog(t *testing.T) {
	src := newFetchServer(t)
	client := cloneForFetch(t, src)
	tracking := refs.RemoteBranchName("origin", "main")
	startTrackingReflog(t, client, tracking)

	client.writeFile("c.txt", "new\n")
	mustStage(t, client, "c.txt")
	commit := client.commitAll("client commit")

	if _, err := Push(t.Context(), client.repo, "origin", remote.PushOptions{
		Refspecs: mustPushSpecs(t, "refs/heads/main:refs/heads/main"),
	}); err != nil {
		t.Fatalf("Push returned error %v", err)
	}

	if got := client.branchTargetIn(tracking); got != commit {
		t.Fatalf("origin/main = %s, want %s", got, commit)
	}
	if log := trackingReflog(t, client, tracking); !strings.Contains(log, "update by push") {
		t.Fatalf("reflog = %q, want the push written down", log)
	}
}

func TestFetchMovesTheTrackingRefOfARepositoryThatKeepsAReflog(t *testing.T) {
	src := newFetchServer(t)
	client := cloneForFetch(t, src)
	tracking := refs.RemoteBranchName("origin", "main")
	startTrackingReflog(t, client, tracking)

	src.writeFile("b.txt", "world\n")
	mustStage(t, src, "b.txt")
	second := src.commitAll("second")

	if _, err := Fetch(t.Context(), client.repo, "", remote.FetchOptions{}); err != nil {
		t.Fatalf("Fetch returned error %v", err)
	}

	if got := client.branchTargetIn(tracking); got != second {
		t.Fatalf("origin/main = %s, want %s", got, second)
	}
	if log := trackingReflog(t, client, tracking); !strings.Contains(log, "fetch") {
		t.Fatalf("reflog = %q, want the fetch written down", log)
	}
}

func TestTheReflogOfAFetchNamesTheUserOfTheRepository(t *testing.T) {
	src := newFetchServer(t)
	client := cloneForFetch(t, src)
	tracking := refs.RemoteBranchName("origin", "main")
	startTrackingReflog(t, client, tracking)

	src.writeFile("b.txt", "world\n")
	mustStage(t, src, "b.txt")
	src.commitAll("second")

	if _, err := Fetch(t.Context(), client.repo, "", remote.FetchOptions{}); err != nil {
		t.Fatalf("Fetch returned error %v", err)
	}

	user := client.repo.Config().User()
	if log := trackingReflog(t, client, tracking); !strings.Contains(log, user.Email) {
		t.Fatalf("reflog = %q, want the committer %q of the repository", log, user.Email)
	}
}
