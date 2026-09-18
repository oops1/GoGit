package ops

import (
	"errors"
	"os"
	"slices"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/repo"
)

func (n *nestedProject) switchTo(t *testing.T, target string, opts SwitchOptions) ([]SubmoduleEvent, error) {
	t.Helper()
	events, got := collectEvents()
	opts.SubmoduleEvents = events
	err := Switch(t.Context(), n.super.reopen(), target, opts)
	return *got, err
}

func (n *nestedProject) mustSwitch(t *testing.T, target string) []SubmoduleEvent {
	t.Helper()
	events, err := n.switchTo(t, target, SwitchOptions{})
	if err != nil {
		t.Fatalf("Switch(%s) returned error %v", target, err)
	}
	return events
}

func (n *nestedProject) branchWithGitmodules(t *testing.T, name, from, gitmodules string, links map[string]hash.ObjectID) {
	t.Helper()
	n.super.appendConfig("[submodule]\n\trecurse = false\n")
	n.super.createBranch(name, n.super.branchTarget(from))
	n.mustSwitch(t, name)
	n.super.writeGitmodules(gitmodules)
	for path, id := range links {
		n.super.setGitlink(path, id)
	}
	n.super.commitAll(name)
	n.mustSwitch(t, from)
	n.super.appendConfig("[submodule]\n\trecurse = true\n")
}

func TestSwitchRecursionMovesRemovesAndRestoresSubmodules(t *testing.T) {
	n := switchProject(t)

	events := n.mustSwitch(t, "moved")
	if n.libHead(t) != n.one || n.super.exists("libs/lib/inner") || !slices.Equal(eventKinds(events), []SubmoduleEventKind{SubmoduleCleared, SubmoduleCheckedOut}) || events[0].Path != "libs/lib/inner" {
		t.Fatalf("moved: events = %+v", events)
	}
	events = n.mustSwitch(t, "main")
	if submoduleHead(t, n.super.path("libs/lib/inner")) != n.innerOne || !slices.Equal(eventKinds(events), []SubmoduleEventKind{SubmoduleCheckedOut, SubmoduleCheckedOut}) {
		t.Fatalf("main: events = %+v", events)
	}
	n.super.writeFile("libs/lib/untracked.txt", "keep\n")
	events = n.mustSwitch(t, "plain")
	if !n.super.exists("libs/lib/untracked.txt") || n.super.exists("libs/lib/.git") || !slices.Contains(eventKinds(events), SubmoduleCleared) {
		t.Fatalf("plain: events = %+v", events)
	}
	if err := os.Remove(n.super.path("libs/lib/untracked.txt")); err != nil {
		t.Fatal(err)
	}
	events = n.mustSwitch(t, "main")
	if n.libHead(t) != n.two || !slices.Contains(eventKinds(events), SubmoduleCheckedOut) {
		t.Fatalf("back to main: events = %+v", events)
	}
}

func TestSwitchRecursionSkipsInactiveAndUnpopulatedSubmodules(t *testing.T) {
	n := switchProject(t)
	if err := SubmoduleDeinit(t.Context(), n.super.reopen(), []string{"libs/lib"}, SubmoduleDeinitOptions{Force: true}); err != nil {
		t.Fatal(err)
	}
	if events := n.mustSwitch(t, "plain"); len(events) != 0 {
		t.Fatalf("an inactive submodule produced %+v", events)
	}
	n.super.appendConfig("[submodule \"lib\"]\n\tactive = true\n")
	if events := n.mustSwitch(t, "main"); len(events) != 2 {
		t.Fatalf("restoring an active submodule produced %+v", events)
	}
	if err := os.RemoveAll(n.super.path("libs/lib")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(n.super.path("libs/lib"), 0o777); err != nil {
		t.Fatal(err)
	}
	if events := n.mustSwitch(t, "moved"); len(events) != 0 {
		t.Fatalf("an unpopulated submodule produced %+v", events)
	}
}

func TestSwitchRecursionRefusesBeforeTouchingTheWorkTree(t *testing.T) {
	tests := []struct {
		name    string
		from    string
		target  string
		prepare func(t *testing.T, n *nestedProject)
		want    error
	}{
		{"broken active flag", "main", "moved", func(t *testing.T, n *nestedProject) {
			n.super.appendConfig("[submodule \"lib\"]\n\tactive = maybe\n")
		}, nil},
		{"known path blocked by a directory", "plain", "main", func(t *testing.T, n *nestedProject) {
			n.super.writeFile(".gitmodules", libGitmodules)
			if err := os.MkdirAll(n.super.path("libs/lib"), 0o777); err != nil {
				t.Fatal(err)
			}
		}, ErrSubmoduleNotUpdated},
		{"missing git directory", "plain", "main", func(t *testing.T, n *nestedProject) {
			if err := os.RemoveAll(n.super.path(".git/modules/lib")); err != nil {
				t.Fatal(err)
			}
		}, ErrSubmoduleGitDirMissing},
		{"staged new file", "main", "moved", func(t *testing.T, n *nestedProject) {
			n.super.writeFile("libs/lib/new.txt", "new\n")
			mustStage(t, n.submoduleRepo(t), "new.txt")
		}, ErrSubmoduleDirtyIndex},
		{"staged removal", "main", "moved", func(t *testing.T, n *nestedProject) {
			sub := n.submoduleRepo(t)
			idx := sub.index()
			idx.Remove("lib.txt")
			sub.saveIndex(idx)
		}, ErrSubmoduleDirtyIndex},
		{"modified file", "main", "moved", func(t *testing.T, n *nestedProject) {
			n.super.writeFile("libs/lib/lib.txt", "changed\n")
		}, ErrWouldOverwrite},
		{"broken submodule head", "main", "moved", func(t *testing.T, n *nestedProject) {
			n.super.writeFile(".git/modules/lib/HEAD", "ref: refs/heads/broken\n")
			n.super.writeFile(".git/modules/lib/refs/heads/broken", "garbage\n")
		}, ErrSubmoduleNotUpdated},
		{"broken submodule index", "main", "moved", func(t *testing.T, n *nestedProject) {
			n.super.writeFile(".git/modules/lib/index", "garbage")
		}, ErrSubmoduleNotUpdated},
		{"broken path rules", "main", "moved", func(t *testing.T, n *nestedProject) {
			n.super.appendConfigAt(".git/modules/lib/config", "[core]\n\tprotectNTFS = maybe\n")
		}, ErrSubmoduleNotUpdated},
		{"broken sparse setting", "main", "moved", func(t *testing.T, n *nestedProject) {
			n.super.appendConfigAt(".git/modules/lib/config", "[core]\n\tsparseCheckout = maybe\n")
		}, ErrSubmoduleNotUpdated},
		{"commit missing from the submodule", "main", "ghost", func(t *testing.T, n *nestedProject) {
			n.branchWithGitmodules(t, "ghost", "main", libGitmodules, map[string]hash.ObjectID{"libs/lib": hash.SumSHA1("commit", []byte("ghost"))})
		}, ErrSubmoduleNotUpdated},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			n := switchProject(t)
			if tc.from != "main" {
				n.mustSwitch(t, tc.from)
			}
			tc.prepare(t, n)
			head := n.super.branchTarget(tc.from)

			_, err := n.switchTo(t, tc.target, SwitchOptions{})

			if err == nil || tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("Switch returned %v, want %v", err, tc.want)
			}
			if current, _ := n.super.headSymbolicTarget(); current.Short() != tc.from || n.super.branchTarget(tc.from) != head {
				t.Fatalf("the refused switch moved HEAD to %s", current)
			}
		})
	}
}

func (r *testRepo) appendConfigAt(rel, text string) {
	r.t.Helper()
	r.writeFile(rel, r.readFile(rel)+text)
}

func TestSwitchRecursionReportsFailuresWhileApplying(t *testing.T) {
	tests := []struct {
		name    string
		from    string
		target  string
		force   bool
		prepare func(t *testing.T, n *nestedProject)
	}{
		{"renamed module with a broken active flag", "main", "renamed", false, func(t *testing.T, n *nestedProject) {
			n.branchWithGitmodules(t, "renamed", "main", "[submodule \"renamed\"]\n\tpath = libs/lib\n\turl = ../lib\n", map[string]hash.ObjectID{"libs/lib": n.one})
			n.super.appendConfig("[submodule \"renamed\"]\n\tactive = maybe\n")
		}},
		{"embedded repository with worktrees", "main", "plain", false, func(t *testing.T, n *nestedProject) {
			embedAbsorbedLib(t, n.super)
			n.super.writeFile("libs/lib/.git/worktrees/w/HEAD", "x\n")
		}},
		{"git directory nested in another module", "plain", "deep", false, func(t *testing.T, n *nestedProject) {
			n.branchWithGitmodules(t, "deep", "plain", "[submodule \"lib/deep\"]\n\tpath = deep\n\turl = ../lib\n", map[string]hash.ObjectID{"deep": n.one})
			n.super.appendConfig("[submodule \"lib/deep\"]\n\tactive = true\n")
			bare, err := repo.Init(n.super.path(".git/modules/lib/deep"), repo.InitOptions{Bare: true, NoSystem: true})
			if err != nil {
				t.Fatal(err)
			}
			_ = bare.Close()
		}},
		{"index in the way", "plain", "main", false, func(t *testing.T, n *nestedProject) {
			_ = os.Remove(n.super.path(".git/modules/lib/index"))
			n.super.writeFile(".git/modules/lib/index/blocker", "x\n")
		}},
		{"broken path rules of a restored submodule", "plain", "main", false, func(t *testing.T, n *nestedProject) {
			n.super.appendConfigAt(".git/modules/lib/config", "[core]\n\tprotectNTFS = maybe\n")
		}},
		{"broken index of a forced move", "main", "moved", true, func(t *testing.T, n *nestedProject) {
			n.super.writeFile(".git/modules/lib/index", "garbage")
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			n := switchProject(t)
			if tc.from != "main" {
				n.mustSwitch(t, tc.from)
			}
			tc.prepare(t, n)

			if _, err := n.switchTo(t, tc.target, SwitchOptions{Force: tc.force}); err == nil {
				t.Fatal("Switch succeeded")
			}
		})
	}
}
