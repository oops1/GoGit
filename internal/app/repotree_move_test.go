package app

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/oops1/gogit/internal/config"
	"github.com/oops1/gogit/internal/repo"
)

func appWithGroups(t *testing.T) (*App, map[string]string) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Groups = []config.Group{
		{ID: "work", Name: "Work"},
		{ID: "archive", Name: "Archive"},
		{ID: "inner", Name: "Inner", Parent: "work"},
	}
	cfg.Repositories = []config.Repository{
		{ID: "r1", Name: "Main", Path: filepath.Join(dir, "main"), Group: "work"},
		{ID: "r2", Name: "Other", Path: filepath.Join(dir, "other")},
		{ID: "w1", Name: "feature", Path: filepath.Join(dir, "feature"), Worktree: true, Parent: "r1"},
	}
	a := newTestAppWithConfig(t, cfg)
	return a, map[string]string{"work": "work", "archive": "archive", "inner": "inner"}
}

func parentIDOf(t *testing.T, a *App, id string) string {
	t.Helper()
	parent, ok := a.registry.ParentOf(id)
	if !ok {
		return ""
	}
	return parent.ID
}

func TestARepositoryDroppedOnAGroupJoinsIt(t *testing.T) {
	a, _ := appWithGroups(t)

	a.moveTreeNode("r2", "archive", true)

	if got := parentIDOf(t, a, "r2"); got != "archive" {
		t.Fatalf("parent = %q, want the group it was dropped on", got)
	}
	saved, err := config.Load(a.paths.ConfigFile())
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range saved.Repositories {
		if r.ID == "r2" && r.Group != "archive" {
			t.Fatalf("saved group = %q, want the new one", r.Group)
		}
	}
}

func TestANodeDroppedNextToAnotherJoinsItsGroup(t *testing.T) {
	a, _ := appWithGroups(t)

	a.moveTreeNode("r2", "r1", false)

	if got := parentIDOf(t, a, "r2"); got != "work" {
		t.Fatalf("parent = %q, want the group of the node it was dropped next to", got)
	}
}

func TestANodeDroppedNextToARootNodeLeavesItsGroup(t *testing.T) {
	a, _ := appWithGroups(t)

	a.moveTreeNode("r1", "r2", false)

	if got := parentIDOf(t, a, "r1"); got != "" {
		t.Fatalf("parent = %q, want the root", got)
	}
}

func TestAGroupDroppedOnAGroupBecomesItsChild(t *testing.T) {
	a, _ := appWithGroups(t)

	a.moveTreeNode("archive", "work", true)

	if got := parentIDOf(t, a, "archive"); got != "work" {
		t.Fatalf("parent = %q, want the group it was dropped on", got)
	}
}

func TestANodeDroppedOnItselfStaysWhereItIs(t *testing.T) {
	a, _ := appWithGroups(t)

	a.moveTreeNode("work", "work", true)

	if got := parentIDOf(t, a, "work"); got != "" {
		t.Fatalf("parent = %q, want the root", got)
	}
}

func TestAnUnknownNodeIsNotMoved(t *testing.T) {
	a, _ := appWithGroups(t)

	a.moveTreeNode("missing", "work", true)

	if _, ok := a.registry.Find("missing"); ok {
		t.Fatal("an unknown node cannot be moved")
	}
}

func TestANodeDroppedNextToAWorktreeStaysWhereItIs(t *testing.T) {
	a, _ := appWithGroups(t)

	a.moveTreeNode("r2", "w1", false)

	if got := parentIDOf(t, a, "r2"); got != "" {
		t.Fatalf("parent = %q, want the root", got)
	}
}

func TestAMoveTheRegistryRefusesIsLogged(t *testing.T) {
	a, _ := appWithGroups(t)

	a.moveTreeNode("work", "inner", true)

	if got := parentIDOf(t, a, "work"); got != "" {
		t.Fatalf("parent = %q, want a group not to move into its own child", got)
	}
}

func TestACollapsedGroupIsRememberedAndForgotten(t *testing.T) {
	a, _ := appWithGroups(t)

	a.rememberGroupState("work", false)

	saved, err := config.Load(a.paths.ConfigFile())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(saved.UI.CollapsedGroups, "work") {
		t.Fatalf("collapsed = %v, want the group in it", saved.UI.CollapsedGroups)
	}

	a.rememberGroupState("work", true)

	saved, err = config.Load(a.paths.ConfigFile())
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(saved.UI.CollapsedGroups, "work") {
		t.Fatalf("collapsed = %v, want the group gone from it", saved.UI.CollapsedGroups)
	}
}

func TestAGroupStateThatDidNotChangeIsNotSavedAgain(t *testing.T) {
	a, _ := appWithGroups(t)
	a.rememberGroupState("work", false)
	stamp, err := os.Stat(a.paths.ConfigFile())
	if err != nil {
		t.Fatal(err)
	}

	a.rememberGroupState("work", false)
	a.rememberGroupState("archive", true)

	again, err := os.Stat(a.paths.ConfigFile())
	if err != nil {
		t.Fatal(err)
	}
	if !again.ModTime().Equal(stamp.ModTime()) {
		t.Fatal("a state that did not change must not be written again")
	}
}

func TestAConfigThatCannotBeSavedAfterAMoveIsLogged(t *testing.T) {
	a, _ := appWithGroups(t)
	if err := os.RemoveAll(a.paths.ConfigFile()); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(a.paths.ConfigFile(), 0o700); err != nil {
		t.Fatal(err)
	}

	a.moveTreeNode("r2", "archive", true)
	a.rememberGroupState("work", false)

	if got := parentIDOf(t, a, "r2"); got != "archive" {
		t.Fatalf("parent = %q, want the move to hold even when the config cannot be saved", got)
	}
}

func TestTheTreeIsArrangedFromTheRegistryAfterAMove(t *testing.T) {
	a, _ := appWithGroups(t)

	a.moveTreeNode("r2", "archive", true)

	item, ok := a.reposView.Item("r2")
	if !ok {
		t.Fatal("the moved repository must still be in the tree")
	}
	archive, ok := a.reposView.Item("archive")
	if !ok {
		t.Fatal("the group must be in the tree")
	}
	if !slices.Contains(archive.Children, item) {
		t.Fatal("the moved repository must show under its new group")
	}
}

func TestTheCollapsedGroupsOfTheConfigReachTheTree(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Groups = []config.Group{{ID: "work", Name: "Work"}}
	cfg.Repositories = []config.Repository{{ID: "r1", Name: "Main", Path: filepath.Join(dir, "main"), Group: "work"}}
	cfg.UI.CollapsedGroups = []string{"work"}

	a := newTestAppWithConfig(t, cfg)

	item, ok := a.reposView.Item("work")
	if !ok {
		t.Fatal("the group must be in the tree")
	}
	if item.Expanded {
		t.Fatal("a group collapsed in the config must open collapsed")
	}
	if node, ok := a.registry.Find("r1"); !ok || node.Kind != repo.KindRepository {
		t.Fatal("the repository must still be in the registry")
	}
}
