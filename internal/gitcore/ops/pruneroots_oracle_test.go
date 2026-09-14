//go:build oracle

package ops

import (
	"slices"
	"strings"
	"testing"
	"time"
)

func gitPruneKeepsRebaseAutostash(o *oracle) bool {
	dir := o.repoDir("autostash-probe")
	newOracleRepo(o, dir)
	o.write(dir, "probe.txt", "probe\n")
	o.run(dir, "add", "probe.txt")
	o.run(dir, "commit", "-q", "-m", "probe")
	stash := strings.TrimSpace(o.run(dir, "commit-tree", "-m", "probe autostash", "HEAD^{tree}"))
	o.write(dir, ".git/rebase-merge/autostash", stash+"\n")
	return !slices.Contains(gitObjectSet(o.run(dir, "prune", "-n", "--expire=now")), stash)
}

func TestOraclePruneKeepsResolveUndoAndAutostashObjectsLikeGit(t *testing.T) {
	o := newOracle(t)
	keepsAutostash := gitPruneKeepsRebaseAutostash(o)
	dir := o.repoDir("undo")
	newOracleRepo(o, dir)
	o.write(dir, "f.txt", "base\n")
	o.run(dir, "add", "f.txt")
	o.run(dir, "commit", "-q", "-m", "base")
	o.run(dir, "switch", "-q", "-c", "other")
	o.write(dir, "f.txt", "theirs only kept by resolve-undo\n")
	o.run(dir, "commit", "-q", "-a", "-m", "theirs")
	o.run(dir, "switch", "-q", "main")
	o.write(dir, "f.txt", "ours\n")
	o.run(dir, "commit", "-q", "-a", "-m", "ours")
	if _, err := o.attempt(dir, "merge", "-q", "other"); err == nil {
		t.Fatal("the merge did not conflict")
	}
	o.write(dir, "f.txt", "resolved\n")
	o.run(dir, "add", "f.txt")
	for _, name := range []string{"MERGE_HEAD", "MERGE_MSG", "MERGE_MODE"} {
		o.remove(dir, ".git/"+name)
	}
	o.run(dir, "branch", "-q", "-D", "other")
	o.run(dir, "reflog", "expire", "--expire=now", "--all")
	stash := strings.TrimSpace(o.run(dir, "commit-tree", "-m", "autostash", "HEAD^{tree}"))
	o.write(dir, ".git/rebase-merge/autostash", stash+"\n")
	r := o.openRepo(dir)
	ageEveryLooseObject(t, r.ObjectsDir(), time.Now().Add(-24*time.Hour))
	expire := time.Now().Add(-12 * time.Hour)

	dry, err := Prune(t.Context(), r, PruneOptions{Expire: expire, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	var ours []string
	for _, id := range dry.Unreachable {
		ours = append(ours, id.String())
	}
	slices.Sort(ours)
	if slices.Contains(ours, stash) {
		t.Fatalf("our prune takes the rebase autostash commit %s", stash)
	}
	theirsSet := gitObjectSet(o.run(dir, "prune", "-n", "--expire=12.hours.ago"))
	if !keepsAutostash {
		t.Logf("the installed git predates rebase autostash roots and prunes %s, so it is left out of the comparison", stash)
		theirsSet = slices.DeleteFunc(theirsSet, func(id string) bool { return id == stash })
	}
	if len(ours) == 0 || !slices.Equal(ours, theirsSet) {
		t.Fatalf("pruned objects differ:\nours   %v\ntheirs %v", ours, theirsSet)
	}

	if _, err := Prune(t.Context(), r, PruneOptions{Expire: expire}); err != nil {
		t.Fatal(err)
	}
	o.run(dir, "fsck", "--no-dangling", "--no-progress")
	o.run(dir, "cat-file", "-e", stash)
	undo := o.run(dir, "ls-files", "--resolve-undo")
	if strings.TrimSpace(undo) == "" {
		t.Fatal("git recorded no resolve-undo information, the oracle did not exercise it")
	}
	for line := range strings.Lines(undo) {
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			o.run(dir, "cat-file", "-e", fields[1])
		}
	}
}
