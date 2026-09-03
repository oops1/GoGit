package repo

import (
	"encoding/hex"
	"errors"
	"path/filepath"
	"testing"

	"github.com/oops1/gogit/internal/config"
)

func namePath(base, name string) string {
	return filepath.Join(base, name)
}

func TestBuildTreeSortsGroupsBeforeRepositoriesCaseInsensitive(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Groups = []config.Group{
		{ID: "gb", Name: "beta"},
		{ID: "ga", Name: "Alpha"},
	}
	cfg.Repositories = []config.Repository{
		{ID: "rb", Name: "bravo", Path: namePath(dir, "bravo")},
		{ID: "ra", Name: "Alpha repo", Path: namePath(dir, "alpha")},
	}
	reg := New(cfg)
	roots := reg.Roots()
	if len(roots) != 4 {
		t.Fatalf("roots = %d, want 4", len(roots))
	}
	if roots[0].Kind != KindGroup || roots[1].Kind != KindGroup {
		t.Fatalf("groups must sort first: %+v", roots)
	}
	if roots[0].Name != "Alpha" || roots[1].Name != "beta" {
		t.Fatalf("group order: %q %q", roots[0].Name, roots[1].Name)
	}
	if roots[2].Name != "Alpha repo" || roots[3].Name != "bravo" {
		t.Fatalf("repository order: %q %q", roots[2].Name, roots[3].Name)
	}
}

func TestBuildTreeNestsGroupsAndRepositories(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Groups = []config.Group{
		{ID: "parent", Name: "Parent"},
		{ID: "child", Name: "Child", Parent: "parent"},
	}
	cfg.Repositories = []config.Repository{
		{ID: "r1", Name: "R1", Path: namePath(dir, "r1"), Group: "child"},
	}
	reg := New(cfg)
	roots := reg.Roots()
	if len(roots) != 1 {
		t.Fatalf("roots = %d, want 1", len(roots))
	}
	parent := roots[0]
	if len(parent.Children) != 1 || parent.Children[0].ID != "child" {
		t.Fatalf("parent children: %+v", parent.Children)
	}
	child := parent.Children[0]
	if len(child.Children) != 1 || child.Children[0].ID != "r1" {
		t.Fatalf("child children: %+v", child.Children)
	}
}

func TestBuildTreePlacesWorktreeUnderRepositoryParent(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Repositories = []config.Repository{
		{ID: "r1", Name: "R1", Path: namePath(dir, "r1")},
		{ID: "w1", Name: "feature", Path: namePath(dir, "feature"), Worktree: true, Parent: "r1"},
	}
	reg := New(cfg)
	roots := reg.Roots()
	if len(roots) != 1 {
		t.Fatalf("roots = %d, want 1", len(roots))
	}
	r1 := roots[0]
	if len(r1.Children) != 1 {
		t.Fatalf("r1 children = %d", len(r1.Children))
	}
	w1 := r1.Children[0]
	if w1.Kind != KindWorktree || w1.ID != "w1" {
		t.Fatalf("worktree node: %+v", w1)
	}
}

func TestBuildTreePlacesDanglingGroupReferenceAtRoot(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Repositories = []config.Repository{
		{ID: "r1", Name: "R1", Path: namePath(dir, "r1"), Group: "missing"},
	}
	reg := New(cfg)
	roots := reg.Roots()
	if len(roots) != 1 || roots[0].ID != "r1" {
		t.Fatalf("roots = %+v", roots)
	}
}

func TestBuildTreePlacesDanglingWorktreeParentAtRoot(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Repositories = []config.Repository{
		{ID: "w1", Name: "feature", Path: namePath(dir, "feature"), Worktree: true, Parent: "missing"},
	}
	reg := New(cfg)
	roots := reg.Roots()
	if len(roots) != 1 || roots[0].ID != "w1" {
		t.Fatalf("roots = %+v", roots)
	}
}

func TestBuildTreeBreaksGroupParentCycle(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Groups = []config.Group{
		{ID: "a", Name: "A", Parent: "b"},
		{ID: "b", Name: "B", Parent: "a"},
	}
	reg := New(cfg)
	roots := reg.Roots()
	total := 0
	for range reg.Walk() {
		total++
	}
	if total != 2 {
		t.Fatalf("total nodes = %d, want 2", total)
	}
	found := false
	for _, r := range roots {
		if r.ID == "a" || r.ID == "b" {
			found = true
		}
	}
	if !found {
		t.Fatal("cycle must be broken by attaching one node to root")
	}
	_ = dir
}

func TestBuildTreeBreaksSelfParentCycle(t *testing.T) {
	cfg := config.Default()
	cfg.Groups = []config.Group{
		{ID: "a", Name: "A", Parent: "a"},
	}
	reg := New(cfg)
	roots := reg.Roots()
	if len(roots) != 1 || roots[0].ID != "a" {
		t.Fatalf("self-referencing group must land at root: %+v", roots)
	}
}

func TestWalkVisitsEveryNodeDepthFirst(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Groups = []config.Group{{ID: "g", Name: "G"}}
	cfg.Repositories = []config.Repository{
		{ID: "r1", Name: "R1", Path: namePath(dir, "r1"), Group: "g"},
	}
	reg := New(cfg)
	var ids []string
	for n := range reg.Walk() {
		ids = append(ids, n.ID)
	}
	if len(ids) != 2 || ids[0] != "g" || ids[1] != "r1" {
		t.Fatalf("walk order = %v", ids)
	}
}

func TestWalkStopsWhenYieldReturnsFalse(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Groups = []config.Group{{ID: "g1", Name: "G1"}, {ID: "g2", Name: "G2"}}
	cfg.Repositories = []config.Repository{
		{ID: "r1", Name: "R1", Path: namePath(dir, "r1"), Group: "g1"},
	}
	reg := New(cfg)
	count := 0
	for range reg.Walk() {
		count++
		if count == 1 {
			break
		}
	}
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}
}

func TestWalkStopsInsideChildSubtree(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Groups = []config.Group{{ID: "g1", Name: "G1"}, {ID: "g2", Name: "G2"}}
	cfg.Repositories = []config.Repository{
		{ID: "r1", Name: "R1", Path: namePath(dir, "r1"), Group: "g1"},
	}
	reg := New(cfg)
	var ids []string
	for n := range reg.Walk() {
		ids = append(ids, n.ID)
		if n.ID == "r1" {
			break
		}
	}
	if len(ids) != 2 || ids[0] != "g1" || ids[1] != "r1" {
		t.Fatalf("ids = %v", ids)
	}
}

func TestFindByID(t *testing.T) {
	cfg := config.Default()
	cfg.Groups = []config.Group{{ID: "g", Name: "G"}}
	reg := New(cfg)
	n, ok := reg.Find("g")
	if !ok || n.Name != "G" {
		t.Fatalf("find: %+v %v", n, ok)
	}
	if _, ok := reg.Find("missing"); ok {
		t.Fatal("unexpected find")
	}
}

func TestFindByPathNormalizesInput(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	abs := namePath(dir, "r1")
	cfg.Repositories = []config.Repository{{ID: "r1", Name: "R1", Path: abs}}
	reg := New(cfg)
	n, ok := reg.FindByPath(namePath(dir, "./r1"))
	if !ok || n.ID != "r1" {
		t.Fatalf("find by path: %+v %v", n, ok)
	}
	if _, ok := reg.FindByPath(namePath(dir, "missing")); ok {
		t.Fatal("unexpected find")
	}
}

func TestFindByPathIgnoresGroups(t *testing.T) {
	cfg := config.Default()
	cfg.Groups = []config.Group{{ID: "g", Name: "G"}}
	reg := New(cfg)
	if _, ok := reg.FindByPath(""); ok {
		t.Fatal("groups have no path")
	}
}

func TestActiveNodeLifecycle(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Repositories = []config.Repository{{ID: "r1", Name: "R1", Path: namePath(dir, "r1")}}
	reg := New(cfg)
	if _, ok := reg.Active(); ok {
		t.Fatal("no active node initially")
	}
	if err := reg.SetActive("missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	if err := reg.SetActive("r1"); err != nil {
		t.Fatal(err)
	}
	n, ok := reg.Active()
	if !ok || n.ID != "r1" {
		t.Fatalf("active: %+v %v", n, ok)
	}
	if err := reg.RemoveRepository("r1"); err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.Active(); ok {
		t.Fatal("active node must disappear once removed")
	}
}

func TestAddGroupCreatesRootGroup(t *testing.T) {
	cfg := config.Default()
	reg := New(cfg)
	n, err := reg.AddGroup("Work", "")
	if err != nil {
		t.Fatal(err)
	}
	if n.Kind != KindGroup || n.Name != "Work" {
		t.Fatalf("node = %+v", n)
	}
	if len(cfg.Groups) != 1 || cfg.Groups[0].ID != n.ID {
		t.Fatalf("config not updated: %+v", cfg.Groups)
	}
}

func TestAddGroupNestedUnderParent(t *testing.T) {
	cfg := config.Default()
	reg := New(cfg)
	parent, err := reg.AddGroup("Parent", "")
	if err != nil {
		t.Fatal(err)
	}
	child, err := reg.AddGroup("Child", parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := reg.Find(parent.ID)
	if !ok || len(got.Children) != 1 || got.Children[0].ID != child.ID {
		t.Fatalf("nesting failed: %+v", got)
	}
}

func TestAddGroupFailsOnIDCollision(t *testing.T) {
	cfg := config.Default()
	reg := New(cfg)
	first, err := reg.AddGroup("First", "")
	if err != nil {
		t.Fatal(err)
	}
	fixed := first.ID
	restore := forceNextID(t, fixed)
	defer restore()
	if _, err := reg.AddGroup("Second", ""); !errors.Is(err, ErrDuplicateID) {
		t.Fatalf("err = %v, want ErrDuplicateID", err)
	}
}

func TestAddGroupFailsWhenRandReadErrors(t *testing.T) {
	restore := forceRandError(t)
	defer restore()
	cfg := config.Default()
	reg := New(cfg)
	if _, err := reg.AddGroup("X", ""); err == nil {
		t.Fatal("expected error")
	}
}

func TestRenameGroup(t *testing.T) {
	cfg := config.Default()
	reg := New(cfg)
	g, err := reg.AddGroup("Old", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.RenameGroup(g.ID, "New"); err != nil {
		t.Fatal(err)
	}
	n, _ := reg.Find(g.ID)
	if n.Name != "New" {
		t.Fatalf("name = %q", n.Name)
	}
	if err := reg.RenameGroup("missing", "X"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestMoveGroupRejectsSelfParent(t *testing.T) {
	cfg := config.Default()
	reg := New(cfg)
	g, err := reg.AddGroup("G", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.MoveGroup(g.ID, g.ID); !errors.Is(err, ErrCycle) {
		t.Fatalf("err = %v, want ErrCycle", err)
	}
}

func TestMoveGroupRejectsMoveIntoDescendant(t *testing.T) {
	cfg := config.Default()
	reg := New(cfg)
	parent, err := reg.AddGroup("Parent", "")
	if err != nil {
		t.Fatal(err)
	}
	child, err := reg.AddGroup("Child", parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.MoveGroup(parent.ID, child.ID); !errors.Is(err, ErrCycle) {
		t.Fatalf("err = %v, want ErrCycle", err)
	}
}

func TestMoveGroupSucceedsToRoot(t *testing.T) {
	cfg := config.Default()
	reg := New(cfg)
	parent, err := reg.AddGroup("Parent", "")
	if err != nil {
		t.Fatal(err)
	}
	child, err := reg.AddGroup("Child", parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.MoveGroup(child.ID, ""); err != nil {
		t.Fatal(err)
	}
	roots := reg.Roots()
	found := false
	for _, r := range roots {
		if r.ID == child.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("child not moved to root: %+v", roots)
	}
}

func TestMoveGroupFailsOnMissingSource(t *testing.T) {
	cfg := config.Default()
	reg := New(cfg)
	if err := reg.MoveGroup("missing", ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestMoveGroupFailsOnMissingDestination(t *testing.T) {
	cfg := config.Default()
	reg := New(cfg)
	g, err := reg.AddGroup("G", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.MoveGroup(g.ID, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestRemoveGroupReparentsChildrenToGrandparent(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	reg := New(cfg)
	grandparent, err := reg.AddGroup("Grandparent", "")
	if err != nil {
		t.Fatal(err)
	}
	middle, err := reg.AddGroup("Middle", grandparent.ID)
	if err != nil {
		t.Fatal(err)
	}
	childGroup, err := reg.AddGroup("ChildGroup", middle.ID)
	if err != nil {
		t.Fatal(err)
	}
	repoNode, err := reg.AddRepository("R", namePath(dir, "r"), middle.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.RemoveGroup(middle.ID); err != nil {
		t.Fatal(err)
	}
	gp, ok := reg.Find(grandparent.ID)
	if !ok {
		t.Fatal("grandparent missing")
	}
	ids := map[string]bool{}
	for _, c := range gp.Children {
		ids[c.ID] = true
	}
	if !ids[childGroup.ID] || !ids[repoNode.ID] {
		t.Fatalf("children not reparented to grandparent: %+v", gp.Children)
	}
	if _, ok := reg.Find(middle.ID); ok {
		t.Fatal("removed group must be gone")
	}
}

func TestRemoveGroupFailsWhenMissing(t *testing.T) {
	cfg := config.Default()
	reg := New(cfg)
	if err := reg.RemoveGroup("missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestAddRepositoryNormalizesPath(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	reg := New(cfg)
	n, err := reg.AddRepository("R", filepath.Join(dir, "sub", "..", "r1"), "")
	if err != nil {
		t.Fatal(err)
	}
	want, _ := filepath.Abs(filepath.Join(dir, "r1"))
	if n.Path != want {
		t.Fatalf("path = %q, want %q", n.Path, want)
	}
}

func TestAddRepositoryFailsOnDuplicatePath(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	reg := New(cfg)
	p := namePath(dir, "r1")
	if _, err := reg.AddRepository("R1", p, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.AddRepository("R1 copy", p, ""); !errors.Is(err, ErrDuplicatePath) {
		t.Fatalf("err = %v, want ErrDuplicatePath", err)
	}
}

func TestAddRepositoryFailsOnIDCollision(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	reg := New(cfg)
	first, err := reg.AddRepository("R1", namePath(dir, "r1"), "")
	if err != nil {
		t.Fatal(err)
	}
	restore := forceNextID(t, first.ID)
	defer restore()
	if _, err := reg.AddRepository("R2", namePath(dir, "r2"), ""); !errors.Is(err, ErrDuplicateID) {
		t.Fatalf("err = %v, want ErrDuplicateID", err)
	}
}

func TestAddRepositoryFailsWhenRandReadErrors(t *testing.T) {
	restore := forceRandError(t)
	defer restore()
	dir := t.TempDir()
	cfg := config.Default()
	reg := New(cfg)
	if _, err := reg.AddRepository("R", namePath(dir, "r1"), ""); err == nil {
		t.Fatal("expected error")
	}
}

func TestRenameRepository(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	reg := New(cfg)
	n, err := reg.AddRepository("Old", namePath(dir, "r1"), "")
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.RenameRepository(n.ID, "New"); err != nil {
		t.Fatal(err)
	}
	got, _ := reg.Find(n.ID)
	if got.Name != "New" {
		t.Fatalf("name = %q", got.Name)
	}
	if err := reg.RenameRepository("missing", "X"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestMoveRepositoryChangesGroup(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	reg := New(cfg)
	g, err := reg.AddGroup("G", "")
	if err != nil {
		t.Fatal(err)
	}
	n, err := reg.AddRepository("R", namePath(dir, "r1"), "")
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.MoveRepository(n.ID, g.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := reg.Find(g.ID)
	if len(got.Children) != 1 || got.Children[0].ID != n.ID {
		t.Fatalf("move failed: %+v", got.Children)
	}
}

func TestMoveRepositoryFailsWhenMissing(t *testing.T) {
	cfg := config.Default()
	reg := New(cfg)
	if err := reg.MoveRepository("missing", ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestMoveRepositoryFailsOnWorktree(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	reg := New(cfg)
	parent, err := reg.AddRepository("R", namePath(dir, "r1"), "")
	if err != nil {
		t.Fatal(err)
	}
	w, err := reg.AddWorktree(parent.ID, "W", namePath(dir, "w1"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.MoveRepository(w.ID, ""); !errors.Is(err, ErrKindMismatch) {
		t.Fatalf("err = %v, want ErrKindMismatch", err)
	}
}

func TestRemoveRepositoryRemovesItsWorktrees(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	reg := New(cfg)
	parent, err := reg.AddRepository("R", namePath(dir, "r1"), "")
	if err != nil {
		t.Fatal(err)
	}
	w, err := reg.AddWorktree(parent.ID, "W", namePath(dir, "w1"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.RemoveRepository(parent.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.Find(parent.ID); ok {
		t.Fatal("repository must be removed")
	}
	if _, ok := reg.Find(w.ID); ok {
		t.Fatal("worktree must be removed with its parent")
	}
}

func TestRemoveRepositoryFailsWhenMissing(t *testing.T) {
	cfg := config.Default()
	reg := New(cfg)
	if err := reg.RemoveRepository("missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestRemoveRepositoryFailsOnGroupID(t *testing.T) {
	cfg := config.Default()
	reg := New(cfg)
	g, err := reg.AddGroup("G", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.RemoveRepository(g.ID); !errors.Is(err, ErrKindMismatch) {
		t.Fatalf("err = %v, want ErrKindMismatch", err)
	}
}

func TestAddWorktreeUnderRepository(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	reg := New(cfg)
	parent, err := reg.AddRepository("R", namePath(dir, "r1"), "")
	if err != nil {
		t.Fatal(err)
	}
	w, err := reg.AddWorktree(parent.ID, "feature", namePath(dir, "feature"))
	if err != nil {
		t.Fatal(err)
	}
	if w.Kind != KindWorktree {
		t.Fatalf("kind = %v", w.Kind)
	}
	got, _ := reg.Find(parent.ID)
	if len(got.Children) != 1 || got.Children[0].ID != w.ID {
		t.Fatalf("worktree not attached: %+v", got.Children)
	}
}

func TestAddWorktreeUnderWorktree(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	reg := New(cfg)
	parent, err := reg.AddRepository("R", namePath(dir, "r1"), "")
	if err != nil {
		t.Fatal(err)
	}
	w1, err := reg.AddWorktree(parent.ID, "W1", namePath(dir, "w1"))
	if err != nil {
		t.Fatal(err)
	}
	w2, err := reg.AddWorktree(w1.ID, "W2", namePath(dir, "w2"))
	if err != nil {
		t.Fatal(err)
	}
	got, _ := reg.Find(w1.ID)
	if len(got.Children) != 1 || got.Children[0].ID != w2.ID {
		t.Fatalf("nested worktree not attached: %+v", got.Children)
	}
}

func TestAddWorktreeFailsWhenParentMissing(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	reg := New(cfg)
	if _, err := reg.AddWorktree("missing", "W", namePath(dir, "w")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestAddWorktreeFailsWhenParentIsGroup(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	reg := New(cfg)
	g, err := reg.AddGroup("G", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.AddWorktree(g.ID, "W", namePath(dir, "w")); !errors.Is(err, ErrKindMismatch) {
		t.Fatalf("err = %v, want ErrKindMismatch", err)
	}
}

func TestAddWorktreeFailsOnDuplicatePath(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	reg := New(cfg)
	parent, err := reg.AddRepository("R", namePath(dir, "r1"), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.AddWorktree(parent.ID, "W", namePath(dir, "r1")); !errors.Is(err, ErrDuplicatePath) {
		t.Fatalf("err = %v, want ErrDuplicatePath", err)
	}
}

func TestAddWorktreeFailsOnIDCollision(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	reg := New(cfg)
	parent, err := reg.AddRepository("R", namePath(dir, "r1"), "")
	if err != nil {
		t.Fatal(err)
	}
	restore := forceNextID(t, parent.ID)
	defer restore()
	if _, err := reg.AddWorktree(parent.ID, "W", namePath(dir, "w1")); !errors.Is(err, ErrDuplicateID) {
		t.Fatalf("err = %v, want ErrDuplicateID", err)
	}
}

func TestAddWorktreeFailsWhenRandReadErrors(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	reg := New(cfg)
	parent, err := reg.AddRepository("R", namePath(dir, "r1"), "")
	if err != nil {
		t.Fatal(err)
	}
	restore := forceRandError(t)
	defer restore()
	if _, err := reg.AddWorktree(parent.ID, "W", namePath(dir, "w1")); err == nil {
		t.Fatal("expected error")
	}
}

func TestConfigRoundTripPreservesTree(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	reg := New(cfg)
	parent, err := reg.AddGroup("Parent", "")
	if err != nil {
		t.Fatal(err)
	}
	repoNode, err := reg.AddRepository("R", namePath(dir, "r1"), parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.AddWorktree(repoNode.ID, "W", namePath(dir, "w1")); err != nil {
		t.Fatal(err)
	}
	data, err := cfg.Encode()
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := config.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	reg2 := New(loaded)
	var before, after []string
	for n := range reg.Walk() {
		before = append(before, n.ID)
	}
	for n := range reg2.Walk() {
		after = append(after, n.ID)
	}
	if len(before) != len(after) {
		t.Fatalf("node count mismatch: %d vs %d", len(before), len(after))
	}
	for i := range before {
		if before[i] != after[i] {
			t.Fatalf("order mismatch at %d: %q vs %q", i, before[i], after[i])
		}
	}
}

func TestFindByPathReturnsFalseWhenPathCannotBeNormalized(t *testing.T) {
	restore := forceAbsPathError(t)
	defer restore()
	cfg := config.Default()
	reg := New(cfg)
	if _, ok := reg.FindByPath("whatever"); ok {
		t.Fatal("expected false")
	}
}

func TestAddRepositoryFailsWhenPathCannotBeNormalized(t *testing.T) {
	restore := forceAbsPathError(t)
	defer restore()
	cfg := config.Default()
	reg := New(cfg)
	if _, err := reg.AddRepository("R", "whatever", ""); err == nil {
		t.Fatal("expected error")
	}
}

func TestAddWorktreeFailsWhenPathCannotBeNormalized(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	reg := New(cfg)
	parent, err := reg.AddRepository("R", namePath(dir, "r1"), "")
	if err != nil {
		t.Fatal(err)
	}
	restore := forceAbsPathError(t)
	defer restore()
	if _, err := reg.AddWorktree(parent.ID, "W", "whatever"); err == nil {
		t.Fatal("expected error")
	}
}

func TestCompareNodesOrdersGroupsBeforeOthersAndBreaksTiesByID(t *testing.T) {
	group := &Node{Kind: KindGroup, ID: "g", Name: "same"}
	repository := &Node{Kind: KindRepository, ID: "r", Name: "same"}
	if compareNodes(group, repository) >= 0 {
		t.Fatal("group must sort before repository")
	}
	if compareNodes(repository, group) <= 0 {
		t.Fatal("repository must sort after group")
	}
	sameA := &Node{Kind: KindRepository, ID: "a", Name: "Same"}
	sameB := &Node{Kind: KindRepository, ID: "b", Name: "same"}
	if compareNodes(sameA, sameB) >= 0 {
		t.Fatal("equal names must break ties by id")
	}
}

func forceAbsPathError(t *testing.T) func() {
	t.Helper()
	prev := absPath
	absPath = func(string) (string, error) { return "", errTestAbs }
	return func() { absPath = prev }
}

var errTestAbs = errors.New("repo: forced abs error")

func forceNextID(t *testing.T, id string) func() {
	t.Helper()
	b, err := hex.DecodeString(id)
	if err != nil {
		t.Fatal(err)
	}
	prev := randRead
	randRead = func(dst []byte) (int, error) {
		copy(dst, b)
		return len(dst), nil
	}
	return func() { randRead = prev }
}

func forceRandError(t *testing.T) func() {
	t.Helper()
	prev := randRead
	randRead = func(dst []byte) (int, error) {
		return 0, errTestRand
	}
	return func() { randRead = prev }
}

var errTestRand = errors.New("repo: forced rand error")
