//go:build oracle

package ops

import (
	"testing"

	"github.com/oops1/gogit/internal/gitcore/remote"
)

func TestOraclePushFollowTagsSendsTheSameTagsAsGit(t *testing.T) {
	o := newOracle(t)
	dir := o.repoDir("work")
	newOracleRepo(o, dir)
	o.run(dir, "config", "core.autocrlf", "false")
	o.write(dir, "a.txt", "1\n")
	o.run(dir, "add", ".")
	o.run(dir, "commit", "-q", "-m", "one")
	o.run(dir, "tag", "-a", "-m", "v1", "v1")
	o.run(dir, "tag", "light")
	o.write(dir, "a.txt", "2\n")
	o.run(dir, "commit", "-q", "-am", "two")
	o.run(dir, "tag", "-a", "-m", "v2", "v2")
	o.run(dir, "checkout", "-q", "-b", "side")
	o.write(dir, "b.txt", "side\n")
	o.run(dir, "add", ".")
	o.run(dir, "commit", "-q", "-m", "side")
	o.run(dir, "tag", "-a", "-m", "side", "side-tag")
	o.run(dir, "checkout", "-q", "main")

	gitBare, ourBare := o.repoDir("git.git"), o.repoDir("ours.git")
	for _, bare := range []string{gitBare, ourBare} {
		o.run(bare, "init", "-q", "--bare")
	}
	o.run(dir, "remote", "add", "ours", ourBare)
	o.run(dir, "push", "-q", gitBare, "main")
	o.run(dir, "push", "-q", "ours", "main")

	for _, branch := range []string{"main", "side"} {
		o.run(dir, "push", "-q", "--follow-tags", gitBare, branch)
		spec := "refs/heads/" + branch + ":refs/heads/" + branch
		if _, err := Push(t.Context(), o.openRepo(dir), "ours", remote.PushOptions{Refspecs: mustPushSpecs(t, spec), FollowTags: true}); err != nil {
			t.Fatalf("Push %s returned error %v", branch, err)
		}
		if want, got := o.run(gitBare, "show-ref"), o.run(ourBare, "show-ref"); want != got {
			t.Fatalf("after pushing %s the server holds\n%s\nwant\n%s", branch, got, want)
		}
	}
}
