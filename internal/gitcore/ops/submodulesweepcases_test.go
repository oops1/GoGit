package ops

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/remote"
)

func embedAbsorbedLib(t *testing.T, super *testRepo) {
	t.Helper()
	gitDir := super.path(".git/modules/lib")
	if err := os.Remove(super.path("libs/lib/.git")); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(gitDir, super.path("libs/lib/.git")); err != nil {
		t.Fatal(err)
	}
	config := super.readFile("libs/lib/.git/config")
	var kept []string
	for line := range strings.SplitSeq(config, "\n") {
		if !strings.Contains(line, "worktree") {
			kept = append(kept, line)
		}
	}
	super.writeFile("libs/lib/.git/config", strings.Join(kept, "\n"))
}

func updatedNestedProject(t *testing.T) *nestedProject {
	t.Helper()
	n := newNestedProject(t)
	n.mustUpdate(t, SubmoduleUpdateOptions{Init: true, Recursive: true})
	return n
}

func TestSubmoduleAddSurvivesInjectedFaults(t *testing.T) {
	p := newLibProject(t, libGitmodules)
	sweepSubmoduleFaults(t, p.world, func(ctx context.Context, t *testing.T, w sweepWorld) error {
		_, err := SubmoduleAdd(ctx, w.open(t, "super"), "../lib", SubmoduleAddOptions{Path: "deps/lib", Branch: "main"})
		return err
	})
}

func TestSubmoduleDeinitSurvivesInjectedFaults(t *testing.T) {
	n := updatedNestedProject(t)
	embedAbsorbedLib(t, n.super)
	sweepSubmoduleFaults(t, n.world, func(ctx context.Context, t *testing.T, w sweepWorld) error {
		return SubmoduleDeinit(ctx, w.open(t, "super"), []string{"libs/lib"}, SubmoduleDeinitOptions{})
	})
}

func TestSubmoduleRemoveSurvivesInjectedFaults(t *testing.T) {
	n := updatedNestedProject(t)
	sweepSubmoduleFaults(t, n.world, func(ctx context.Context, t *testing.T, w sweepWorld) error {
		return SubmoduleRemove(ctx, w.open(t, "super"), []string{"libs/lib"}, SubmoduleRemoveOptions{})
	})
}

func switchProject(t *testing.T) *nestedProject {
	t.Helper()
	n := updatedNestedProject(t)
	main := n.super.branchTarget("main")
	n.super.createBranch("moved", main)
	n.super.createBranch("plain", main)
	if err := Switch(t.Context(), n.super.reopen(), "moved", SwitchOptions{}); err != nil {
		t.Fatal(err)
	}
	n.record(n.one)
	if err := Switch(t.Context(), n.super.reopen(), "plain", SwitchOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := SubmoduleRemove(t.Context(), n.super.reopen(), []string{"libs/lib"}, SubmoduleRemoveOptions{Force: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := Commit(t.Context(), n.super.reopen(), CommitOptions{Message: "plain"}); err != nil {
		t.Fatal(err)
	}
	if err := Switch(t.Context(), n.super.reopen(), "main", SwitchOptions{}); err != nil {
		t.Fatal(err)
	}
	n.mustUpdate(t, SubmoduleUpdateOptions{Recursive: true})
	n.super.appendConfig("[submodule]\n\trecurse = true\n")
	return n
}

func TestSwitchRecursionSurvivesInjectedFaults(t *testing.T) {
	n := switchProject(t)
	for _, target := range []string{"moved", "plain"} {
		t.Run(target, func(t *testing.T) {
			sweepSubmoduleFaults(t, n.world, func(ctx context.Context, t *testing.T, w sweepWorld) error {
				return Switch(ctx, w.open(t, "super"), target, SwitchOptions{})
			})
		})
	}
	if err := Switch(t.Context(), n.super.reopen(), "plain", SwitchOptions{}); err != nil {
		t.Fatal(err)
	}
	t.Run("added", func(t *testing.T) {
		sweepSubmoduleFaults(t, n.world, func(ctx context.Context, t *testing.T, w sweepWorld) error {
			return Switch(ctx, w.open(t, "super"), "main", SwitchOptions{})
		})
	})
}

func TestFetchRecursionSurvivesInjectedFaults(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(t *testing.T, p *libProject, clone *testRepo)
	}{
		{"on demand", func(t *testing.T, p *libProject, clone *testRepo) {}},
		{"unpopulated", func(t *testing.T, p *libProject, clone *testRepo) {
			if err := SubmoduleDeinit(t.Context(), clone.reopen(), nil, SubmoduleDeinitOptions{All: true}); err != nil {
				t.Fatal(err)
			}
		}},
		{"hidden commit", func(t *testing.T, p *libProject, clone *testRepo) {
			p.lib.writeRawRef("refs/heads/main", p.two.String()+"\n")
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := newLibProject(t, libGitmodules)
			clone := p.cloneSuper(t, "work", CloneOptions{RecurseSubmodules: true})
			three := p.lib.commitFile("lib.txt", "three\n", "three")
			p.record(three)
			tc.prepare(t, p, clone)
			sweepSubmoduleFaults(t, p.world, func(ctx context.Context, t *testing.T, w sweepWorld) error {
				_, err := Fetch(ctx, w.open(t, "work"), "", remote.FetchOptions{})
				return err
			})
		})
	}
}
