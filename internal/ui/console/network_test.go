package console

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/ops"
	"github.com/oops1/gogit/internal/gitcore/refspec"
	"github.com/oops1/gogit/internal/gitcore/remote"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/gitcore/transport"
)

func clonedPair(t *testing.T) (server, client *testRepo) {
	t.Helper()
	server = newTestRepo(t)
	server.commit("initial", map[string]string{"a.txt": "one\n"})
	dest := filepath.Join(t.TempDir(), "clone")
	r, err := ops.Clone(t.Context(), server.dir, dest, ops.CloneOptions{})
	if err != nil {
		t.Fatal(err)
	}
	writeConfigIdentity(t, r)
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	opened, err := gitrepo.Open(dest, gitrepo.OpenOptions{NoSystem: true, GlobalFile: isolatedGlobalFile(t)})
	if err != nil {
		t.Fatal(err)
	}
	client = &testRepo{t: t, repo: opened, dir: dest, env: Env{Repo: opened, Now: server.env.Now}}
	t.Cleanup(func() { _ = client.repo.Close() })
	return server, client
}

func TestFetchBringsInTheNewCommits(t *testing.T) {
	server, client := clonedPair(t)
	server.commit("second", map[string]string{"b.txt": "b\n"})

	got := lines(client.run("fetch"))

	if len(got) != 2 || !strings.HasPrefix(got[0], "Objects: ") {
		t.Fatalf("out = %#v", got)
	}
	if !strings.HasSuffix(got[1], "refs/remotes/origin/main") {
		t.Fatalf("out = %#v", got)
	}
}

func TestFetchTakesAnExplicitRemoteAndFlags(t *testing.T) {
	server, client := clonedPair(t)
	server.commit("second", map[string]string{"b.txt": "b\n"})

	if got := client.run("fetch --prune --tags origin"); !strings.HasPrefix(got, "Objects: ") {
		t.Fatalf("out = %q", got)
	}
}

func TestFetchRefusesTwoRemotes(t *testing.T) {
	_, client := clonedPair(t)

	if err := client.runFails("fetch origin other"); !errors.Is(err, ErrUsage) {
		t.Fatalf("err = %v", err)
	}
}

func TestFetchOfAnUnknownRemoteFails(t *testing.T) {
	_, client := clonedPair(t)

	if err := client.runFails("fetch nowhere"); !errors.Is(err, remote.ErrNoRemote) {
		t.Fatalf("err = %v", err)
	}
}

func TestPullFastForwardsTheBranch(t *testing.T) {
	server, client := clonedPair(t)
	server.commit("second", map[string]string{"b.txt": "b\n"})

	got := lines(client.run("pull"))

	if len(got) != 2 || !strings.Contains(got[1], "..") {
		t.Fatalf("out = %#v", got)
	}
}

func TestPullSaysWhenThereIsNothingNew(t *testing.T) {
	_, client := clonedPair(t)

	got := lines(client.run("pull --prune"))

	if !slices.Contains(got, "Already up to date.") {
		t.Fatalf("out = %#v", got)
	}
}

func TestPullReportsConflicts(t *testing.T) {
	server, client := clonedPair(t)
	server.commit("theirs", map[string]string{"a.txt": "theirs\n"})
	client.commit("ours", map[string]string{"a.txt": "ours\n"})

	got := lines(client.run("pull"))

	if !slices.Contains(got, "CONFLICT: a.txt") {
		t.Fatalf("out = %#v", got)
	}
}

func TestPullRefusesTwoRemotes(t *testing.T) {
	_, client := clonedPair(t)

	if err := client.runFails("pull a b"); !errors.Is(err, ErrUsage) {
		t.Fatalf("err = %v", err)
	}
}

func TestPushSendsTheLocalCommits(t *testing.T) {
	server, client := clonedPair(t)
	client.commit("local", map[string]string{"b.txt": "b\n"})

	got := client.run("push")

	if got != "  main -> main" {
		t.Fatalf("out = %q", got)
	}
	if listed := lines(server.run("log --oneline")); len(listed) != 2 {
		t.Fatalf("server log = %#v", listed)
	}
}

func TestPushSaysWhenThereIsNothingToSend(t *testing.T) {
	_, client := clonedPair(t)

	if got := client.run("push"); got != "Everything up-to-date" {
		t.Fatalf("out = %q", got)
	}
}

func TestPushCarriesTagsAndForcesOnRequest(t *testing.T) {
	_, client := clonedPair(t)
	client.commit("local", map[string]string{"b.txt": "b\n"})
	client.run("tag v1")

	if got := client.run("push --follow-tags --force --atomic origin"); got == "" {
		t.Fatal("push must report what it sent")
	}
}

func TestPushTakesAnExplicitRefspec(t *testing.T) {
	server, client := clonedPair(t)
	client.commit("local", map[string]string{"b.txt": "b\n"})

	client.run("push origin refs/heads/main:refs/heads/copy")

	if got := lines(server.run("branch")); !slices.Contains(got, "  copy") {
		t.Fatalf("server branches = %#v", got)
	}
}

func TestPushRefusesABadRefspec(t *testing.T) {
	_, client := clonedPair(t)

	if err := client.runFails("push origin :::"); !errors.Is(err, refspec.ErrInvalid) {
		t.Fatalf("err = %v", err)
	}
}

func TestPushFromADetachedHeadNeedsARefspec(t *testing.T) {
	_, client := clonedPair(t)
	head := client.run("log --oneline")
	client.run("checkout " + strings.Fields(head)[0])

	if err := client.runFails("push"); !errors.Is(err, ErrUsage) {
		t.Fatalf("err = %v", err)
	}
}

func TestPushReportsARejection(t *testing.T) {
	server, client := clonedPair(t)
	server.commit("theirs", map[string]string{"a.txt": "theirs\n"})
	client.commit("ours", map[string]string{"a.txt": "ours\n"})

	err := client.runFails("push")

	if err == nil {
		t.Fatal("a non-fast-forward push must fail")
	}
}

func TestChangeMarkersFollowGit(t *testing.T) {
	tests := []struct {
		name   string
		change remote.Change
		want   string
	}{
		{"created", remote.Change{Created: true}, "*"},
		{"deleted", remote.Change{Deleted: true}, "-"},
		{"forced", remote.Change{Forced: true}, "+"},
		{"plain", remote.Change{}, " "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := changeMarker(tt.change); got != tt.want {
				t.Fatalf("marker = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPushPrintsWhatTheServerRefused(t *testing.T) {
	result := remote.PushResult{
		Rejected: []transport.RefStatus{{Name: "refs/heads/main", Message: "non-fast-forward"}},
	}

	if got := formatPush(result); got != "! refs/heads/main non-fast-forward" {
		t.Fatalf("out = %q", got)
	}
}

func TestTagModeFollowsTheFlag(t *testing.T) {
	if tagMode(false) != remote.TagsFollow || tagMode(true) != remote.TagsAll {
		t.Fatal("the tag mode must follow the flag")
	}
}
