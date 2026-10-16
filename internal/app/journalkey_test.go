package app

import (
	"context"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/revision"
	"github.com/oops1/gogit/internal/ui/branches"
	"github.com/oops1/gogit/internal/ui/journal"
)

func keySnapshot(t *testing.T) branches.Snapshot {
	t.Helper()
	return branches.Snapshot{
		Current: "main",
		HeadID:  memoID(t, "11"),
		Local: []branches.Branch{
			{Name: refs.BranchName("main"), Target: memoID(t, "11"), Upstream: refs.RemoteBranchName("origin", "main")},
		},
		Remotes: []branches.Remote{{
			Name:     "origin",
			Head:     refs.RemoteBranchName("origin", "main"),
			Branches: []branches.Branch{{Name: refs.RemoteBranchName("origin", "main"), Target: memoID(t, "22")}},
		}},
		Tags: []branches.Tag{{Name: refs.TagName("v1"), Target: memoID(t, "33"), Peeled: memoID(t, "44")}},
	}
}

func TestTheJournalKeyFollowsEverythingTheJournalIsBuiltFrom(t *testing.T) {
	base := journalKeyOf(keySnapshot(t), true, 500)
	tests := []struct {
		name   string
		change func(s *branches.Snapshot) (hasRemotes bool, logMax int)
	}{
		{"the current branch", func(s *branches.Snapshot) (bool, int) { s.Current = "dev"; return true, 500 }},
		{"a detached head", func(s *branches.Snapshot) (bool, int) { s.Detached = true; return true, 500 }},
		{"the head commit", func(s *branches.Snapshot) (bool, int) { s.HeadID = memoID(t, "99"); return true, 500 }},
		{"a local branch tip", func(s *branches.Snapshot) (bool, int) { s.Local[0].Target = memoID(t, "99"); return true, 500 }},
		{"a symbolic branch", func(s *branches.Snapshot) (bool, int) {
			s.Local[0].SymbolicTarget = refs.BranchName("x")
			return true, 500
		}},
		{"a new local branch", func(s *branches.Snapshot) (bool, int) {
			s.Local = append(s.Local, branches.Branch{Name: refs.BranchName("new"), Target: memoID(t, "55")})
			return true, 500
		}},
		{"a remote head", func(s *branches.Snapshot) (bool, int) {
			s.Remotes[0].Head = refs.RemoteBranchName("origin", "dev")
			return true, 500
		}},
		{"a remote branch tip", func(s *branches.Snapshot) (bool, int) {
			s.Remotes[0].Branches[0].Target = memoID(t, "99")
			return true, 500
		}},
		{"a new remote", func(s *branches.Snapshot) (bool, int) {
			s.Remotes = append(s.Remotes, branches.Remote{Name: "fork"})
			return true, 500
		}},
		{"a tag target", func(s *branches.Snapshot) (bool, int) { s.Tags[0].Target = memoID(t, "99"); return true, 500 }},
		{"a peeled tag", func(s *branches.Snapshot) (bool, int) { s.Tags[0].Peeled = memoID(t, "99"); return true, 500 }},
		{"the presence of remotes", func(s *branches.Snapshot) (bool, int) { return false, 500 }},
		{"the log length", func(s *branches.Snapshot) (bool, int) { return true, 1000 }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			snap := keySnapshot(t)
			hasRemotes, logMax := tc.change(&snap)

			if journalKeyOf(snap, hasRemotes, logMax) == base {
				t.Fatalf("the key did not change with %s", tc.name)
			}
		})
	}
}

func TestTheJournalKeyIgnoresWhatTheJournalIsNotBuiltFrom(t *testing.T) {
	base := journalKeyOf(keySnapshot(t), true, 500)
	snap := keySnapshot(t)
	snap.Local[0].Upstream = refs.RemoteBranchName("origin", "other")
	snap.Stashes = []branches.Stash{{Index: 0, Commit: hash.ObjectID{1}, Message: "wip"}}

	if journalKeyOf(snap, true, 500) != base {
		t.Fatal("the key changed with an upstream name or a stash")
	}
}

func TestTheJournalKeeperReportsOnlyChanges(t *testing.T) {
	var keeper journalKeeper

	if !keeper.changed("a") || keeper.changed("a") || !keeper.changed("b") {
		t.Fatal("the keeper did not report exactly the changes")
	}
	keeper.remember("c")
	if keeper.changed("c") {
		t.Fatal("a remembered key was reported as a change")
	}
}

func countJournalRestarts(t *testing.T) *int {
	t.Helper()
	restarts := new(int)
	previous := newJournalPager
	newJournalPager = func(ctx context.Context, source revision.Context, opts journal.Options) journalPager {
		*restarts++
		return previous(ctx, source, opts)
	}
	t.Cleanup(func() { newJournalPager = previous })
	return restarts
}

func TestARefreshWithUnchangedRefsDoesNotRestartTheJournal(t *testing.T) {
	a, _, _ := cloneForDivergence(t)
	a.stopWatcher()
	restarts := countJournalRestarts(t)

	a.RefreshRepository()
	a.RefreshRepository()
	a.refreshAtOnce()

	if *restarts != 0 {
		t.Fatalf("the journal was restarted %d times while no ref moved, want none", *restarts)
	}
}

func TestARefreshAfterABranchMovedRestartsTheJournalOnce(t *testing.T) {
	a, _, local := cloneForDivergence(t)
	a.stopWatcher()
	restarts := countJournalRestarts(t)
	addLocalCommitWithMessage(t, local, "main", "second.txt", "second\n", "second")

	a.RefreshRepository()
	a.RefreshRepository()

	if *restarts != 1 {
		t.Fatalf("the journal was restarted %d times after one ref moved, want once", *restarts)
	}
}

func TestTheRefreshCommandAlwaysReloadsTheJournal(t *testing.T) {
	a, _, _ := cloneForDivergence(t)
	a.stopWatcher()
	restarts := countJournalRestarts(t)

	a.Dispatch(CmdRefresh)

	if *restarts != 1 {
		t.Fatalf("the journal was restarted %d times by the refresh command, want once", *restarts)
	}
}
