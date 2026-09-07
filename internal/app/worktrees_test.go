package app

import (
	"bytes"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/config"
	"github.com/oops1/gogit/internal/gitcore/ops"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/repo"
)

func stubListWorktrees(t *testing.T, replacement func(*gitrepo.Repository) ([]ops.Worktree, error)) {
	t.Helper()
	original := listWorktrees
	listWorktrees = replacement
	t.Cleanup(func() { listWorktrees = original })
}

func repositoryWithWorktreeOnDisk(t *testing.T, a *App) (*repo.Node, string) {
	t.Helper()
	dir := t.TempDir()
	created, err := gitrepo.Init(dir, gitrepo.InitOptions{InitialBranch: "main", NoSystem: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := created.Close(); err != nil {
		t.Fatal(err)
	}
	node, err := a.registry.AddRepository("main", dir, "")
	if err != nil {
		t.Fatal(err)
	}
	linked := filepath.Join(t.TempDir(), "feature")
	if err := os.MkdirAll(linked, 0o755); err != nil {
		t.Fatal(err)
	}
	return node, linked
}

func TestOpeningARepositoryAdoptsTheWorktreesFoundOnDisk(t *testing.T) {
	a := newTestApp(t)
	node, linked := repositoryWithWorktreeOnDisk(t, a)
	stubListWorktrees(t, func(*gitrepo.Repository) ([]ops.Worktree, error) {
		return []ops.Worktree{
			{Path: node.Path, Main: true},
			{Path: linked, ID: "feature", Branch: "refs/heads/feature"},
		}, nil
	})

	if !a.syncWorktrees(node.ID, nil) {
		t.Fatal("a worktree that is on disk but not in the tree must be adopted")
	}

	parent, _ := a.registry.Find(node.ID)
	if len(parent.Children) != 1 {
		t.Fatalf("children = %d, want the worktree", len(parent.Children))
	}
	child := parent.Children[0]
	if child.Kind != repo.KindWorktree || child.Path != filepath.Clean(linked) || child.Name != "feature" {
		t.Fatalf("child = %+v, want the linked worktree named after its branch", child)
	}
}

func TestADetachedWorktreeIsNamedAfterItsDirectory(t *testing.T) {
	a := newTestApp(t)
	node, linked := repositoryWithWorktreeOnDisk(t, a)
	stubListWorktrees(t, func(*gitrepo.Repository) ([]ops.Worktree, error) {
		return []ops.Worktree{{Path: linked, ID: "feature"}}, nil
	})

	a.syncWorktrees(node.ID, nil)

	parent, _ := a.registry.Find(node.ID)
	if parent.Children[0].Name != filepath.Base(linked) {
		t.Fatalf("name = %q, want the directory name", parent.Children[0].Name)
	}
}

func TestAWorktreeThatIsGoneLeavesTheTree(t *testing.T) {
	a := newTestApp(t)
	node, linked := repositoryWithWorktreeOnDisk(t, a)
	if _, err := a.registry.AddWorktree(node.ID, "feature", linked); err != nil {
		t.Fatal(err)
	}
	stubListWorktrees(t, func(*gitrepo.Repository) ([]ops.Worktree, error) {
		return []ops.Worktree{{Path: node.Path, Main: true}}, nil
	})

	if !a.syncWorktrees(node.ID, nil) {
		t.Fatal("a worktree that vanished from disk must leave the tree")
	}

	parent, _ := a.registry.Find(node.ID)
	if len(parent.Children) != 0 {
		t.Fatalf("children = %d, want none", len(parent.Children))
	}
}

func TestAnUnchangedWorktreeListLeavesTheTreeAlone(t *testing.T) {
	a := newTestApp(t)
	node, linked := repositoryWithWorktreeOnDisk(t, a)
	if _, err := a.registry.AddWorktree(node.ID, "feature", linked); err != nil {
		t.Fatal(err)
	}
	stubListWorktrees(t, func(*gitrepo.Repository) ([]ops.Worktree, error) {
		return []ops.Worktree{
			{Path: node.Path, Main: true},
			{Path: linked, ID: "feature", Branch: "refs/heads/feature"},
		}, nil
	})

	if a.syncWorktrees(node.ID, nil) {
		t.Fatal("a list that matches the tree must not report a change")
	}
}

func TestAListThatCannotBeReadLeavesTheTreeAlone(t *testing.T) {
	a := newTestApp(t)
	node, _ := repositoryWithWorktreeOnDisk(t, a)
	stubListWorktrees(t, func(*gitrepo.Repository) ([]ops.Worktree, error) {
		return nil, errors.New("no worktrees for you")
	})

	if a.syncWorktrees(node.ID, nil) {
		t.Fatal("a failure must not report a change")
	}
}

func TestSyncingWorktreesOfAnUnknownParentAddsNothing(t *testing.T) {
	a := newTestApp(t)
	stubListWorktrees(t, func(*gitrepo.Repository) ([]ops.Worktree, error) {
		return []ops.Worktree{{Path: t.TempDir(), ID: "orphan"}}, nil
	})

	if a.syncWorktrees("no-such-node", nil) {
		t.Fatal("a worktree cannot be adopted by a parent that is not there")
	}
}

func TestAdoptingIsSkippedForAWorktreeNode(t *testing.T) {
	a := newTestApp(t)
	node, linked := repositoryWithWorktreeOnDisk(t, a)
	child, err := a.registry.AddWorktree(node.ID, "feature", linked)
	if err != nil {
		t.Fatal(err)
	}
	called := false
	stubListWorktrees(t, func(*gitrepo.Repository) ([]ops.Worktree, error) {
		called = true
		return nil, nil
	})

	a.adoptWorktreesOf(child, &openedRepository{})

	if called {
		t.Fatal("a worktree has no worktrees of its own to adopt")
	}
}

func TestAdoptingSavesTheTreeItChanged(t *testing.T) {
	a := newTestApp(t)
	node, linked := repositoryWithWorktreeOnDisk(t, a)
	stubListWorktrees(t, func(*gitrepo.Repository) ([]ops.Worktree, error) {
		return []ops.Worktree{{Path: linked, ID: "feature", Branch: "refs/heads/feature"}}, nil
	})

	a.adoptWorktreesOf(node, &openedRepository{})

	saved, err := config.Load(a.paths.ConfigFile())
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range saved.Repositories {
		if r.Worktree && r.Path == filepath.Clean(linked) {
			return
		}
	}
	t.Fatalf("the adopted worktree was not written to the config: %+v", saved.Repositories)
}

func TestAWorktreeThatCannotBeForgottenIsLogged(t *testing.T) {
	a := newTestApp(t)
	node, linked := repositoryWithWorktreeOnDisk(t, a)
	child, err := a.registry.AddWorktree(node.ID, "feature", linked)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.registry.RemoveRepository(child.ID); err != nil {
		t.Fatal(err)
	}
	parent, _ := a.registry.Find(node.ID)
	parent.Children = append(parent.Children, &repo.Node{ID: "ghost", Kind: repo.KindWorktree, Path: linked})
	stubListWorktrees(t, func(*gitrepo.Repository) ([]ops.Worktree, error) {
		return []ops.Worktree{{Path: node.Path, Main: true}}, nil
	})

	if a.syncWorktrees(node.ID, nil) {
		t.Fatal("a worktree the registry refuses to forget must not count as a change")
	}
}

func TestAdoptingLogsAConfigItCannotSave(t *testing.T) {
	widget.ClearStrings()
	t.Cleanup(widget.ClearStrings)
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "config.toml"), 0o700); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	a, err := New(config.Default(), config.Paths{Dir: dir}, logger)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)
	node, linked := repositoryWithWorktreeOnDisk(t, a)
	stubListWorktrees(t, func(*gitrepo.Repository) ([]ops.Worktree, error) {
		return []ops.Worktree{{Path: linked, ID: "feature", Branch: "refs/heads/feature"}}, nil
	})

	a.adoptWorktreesOf(node, &openedRepository{})

	if !strings.Contains(buf.String(), "save config failed") {
		t.Fatalf("expected the save failure to be logged: %s", buf.String())
	}
}
