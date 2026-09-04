package repos

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/oops1/headless-gui/v3/widget/treeview"

	"github.com/oops1/gogit/internal/config"
	"github.com/oops1/gogit/internal/repo"
)

func mkdirs(t *testing.T, base string, rel ...string) {
	t.Helper()
	for _, r := range rel {
		if err := os.MkdirAll(filepath.Join(base, filepath.FromSlash(r)), 0o777); err != nil {
			t.Fatal(err)
		}
	}
}

func mkfile(t *testing.T, base, rel string) {
	t.Helper()
	full := filepath.Join(base, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func repoRegistry(t *testing.T, path string) (*repo.Registry, string) {
	t.Helper()
	cfg := config.Default()
	reg := repo.New(cfg)
	n, err := reg.AddRepository("Main", path, "")
	if err != nil {
		t.Fatal(err)
	}
	return reg, n.ID
}

func findChild(item *treeview.TreeViewItem, name string) *treeview.TreeViewItem {
	for _, c := range item.Children {
		if c.DisplayText() == name {
			return c
		}
	}
	return nil
}

func TestDirTreeShowsOnlySubdirectoriesNotFiles(t *testing.T) {
	dir := t.TempDir()
	mkdirs(t, dir, "src")
	mkfile(t, dir, "readme.md")
	reg, repoID := repoRegistry(t, dir)
	v, tw := bound(t)
	v.Render(reg, nil)

	root, ok := v.Item(repoID)
	if !ok {
		t.Fatal("repository item missing")
	}
	tw.Tree.ExpandItem(root)

	if len(root.Children) != 1 || root.Children[0].DisplayText() != "src" {
		t.Fatalf("children = %+v, want only [src]", root.Children)
	}
}

func TestDirTreeHidesTheDotGitDirectory(t *testing.T) {
	dir := t.TempDir()
	mkdirs(t, dir, ".git", "src")
	reg, repoID := repoRegistry(t, dir)
	v, tw := bound(t)
	v.Render(reg, nil)

	root, _ := v.Item(repoID)
	tw.Tree.ExpandItem(root)

	if len(root.Children) != 1 || root.Children[0].DisplayText() != "src" {
		t.Fatalf("children = %+v, want .git hidden", root.Children)
	}
}

func TestDirTreeShowsHiddenAndIgnoredLookingDirectories(t *testing.T) {
	dir := t.TempDir()
	mkdirs(t, dir, ".github", "output", "node_modules")
	reg, repoID := repoRegistry(t, dir)
	v, tw := bound(t)
	v.Render(reg, nil)

	root, _ := v.Item(repoID)
	tw.Tree.ExpandItem(root)

	want := map[string]bool{".github": true, "output": true, "node_modules": true}
	if len(root.Children) != len(want) {
		t.Fatalf("children = %+v, want %v", root.Children, want)
	}
	for _, c := range root.Children {
		if !want[c.DisplayText()] {
			t.Fatalf("unexpected child %q", c.DisplayText())
		}
	}
}

func TestDirTreeSortsSubdirectoriesCaseInsensitively(t *testing.T) {
	dir := t.TempDir()
	mkdirs(t, dir, "Beta", "alpha", "Gamma")
	reg, repoID := repoRegistry(t, dir)
	v, tw := bound(t)
	v.Render(reg, nil)

	root, _ := v.Item(repoID)
	tw.Tree.ExpandItem(root)

	if len(root.Children) != 3 {
		t.Fatalf("children = %d, want 3", len(root.Children))
	}
	got := []string{root.Children[0].DisplayText(), root.Children[1].DisplayText(), root.Children[2].DisplayText()}
	want := []string{"alpha", "Beta", "Gamma"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}

func TestDirTreeRootWithoutSubdirectoriesHasNoExpandArrow(t *testing.T) {
	dir := t.TempDir()
	mkfile(t, dir, "readme.md")
	reg, repoID := repoRegistry(t, dir)
	v, _ := bound(t)
	v.Render(reg, nil)

	root, _ := v.Item(repoID)
	if root.HasChildren() {
		t.Fatal("a repository with no subdirectories must not show an expand arrow")
	}
}

func TestDirTreeNestedDirectoryWithNoSubdirectoriesHasNoExpandArrow(t *testing.T) {
	dir := t.TempDir()
	mkdirs(t, dir, "src")
	mkfile(t, dir, "src/main.go")
	reg, repoID := repoRegistry(t, dir)
	v, tw := bound(t)
	v.Render(reg, nil)

	root, _ := v.Item(repoID)
	tw.Tree.ExpandItem(root)
	src := findChild(root, "src")
	if src == nil {
		t.Fatal("src missing")
	}
	if src.HasChildren() {
		t.Fatal("src has only files, must not show an expand arrow")
	}
}

func TestDirTreeCollapsedRootShowsAStubPlaceholderBeforeExpansion(t *testing.T) {
	dir := t.TempDir()
	mkdirs(t, dir, "src")
	reg, repoID := repoRegistry(t, dir)
	v, _ := bound(t)
	v.Render(reg, nil)

	root, _ := v.Item(repoID)
	if !root.HasChildren() {
		t.Fatal("collapsed repository with subdirectories must show an expand arrow")
	}
	if len(root.Children) != 1 || root.Children[0].DisplayText() != "" {
		t.Fatalf("children = %+v, want a single anonymous stub", root.Children)
	}
}

func TestDirTreeExpandingDoesNotReadDescendantsBeyondOneLevel(t *testing.T) {
	dir := t.TempDir()
	mkdirs(t, dir, "src/pkg/inner")
	reg, repoID := repoRegistry(t, dir)
	v, tw := bound(t)

	var reads []string
	prev := source
	source = func(path string) ([]string, error) {
		reads = append(reads, path)
		return prev(path)
	}
	t.Cleanup(func() { source = prev })

	v.Render(reg, nil)
	reads = nil
	root, _ := v.Item(repoID)
	tw.Tree.ExpandItem(root)

	src := findChild(root, "src")
	if src == nil {
		t.Fatal("src missing")
	}
	if !src.HasChildren() {
		t.Fatal("src has pkg subdirectory, must show an expand arrow")
	}
	if len(src.Children) != 1 || src.Children[0].DisplayText() != "" {
		t.Fatalf("src children = %+v, want a stub, not real descendants", src.Children)
	}
	wantReads := map[string]bool{dir: true, filepath.Join(dir, "src"): true}
	if len(reads) != len(wantReads) {
		t.Fatalf("reads = %v, want exactly %v (no deeper recursion)", reads, wantReads)
	}
	for _, r := range reads {
		if !wantReads[r] {
			t.Fatalf("unexpected read of %q, wantReads = %v", r, wantReads)
		}
	}
}

func TestDirTreeExpandingReplacesTheStubWithRealChildrenSynchronously(t *testing.T) {
	dir := t.TempDir()
	mkdirs(t, dir, "src")
	mkdirs(t, dir, "docs")
	reg, repoID := repoRegistry(t, dir)
	v, tw := bound(t)
	v.Render(reg, nil)

	root, _ := v.Item(repoID)
	tw.Tree.ExpandItem(root)

	if len(root.Children) != 2 {
		t.Fatalf("children = %+v, want [docs src]", root.Children)
	}
	for _, c := range root.Children {
		if _, isStub := c.Tag.(stubMarker); isStub {
			t.Fatal("the stub must not remain among the real children")
		}
	}
}

func TestDirTreeExpandingTwiceDoesNotReloadFromDisk(t *testing.T) {
	dir := t.TempDir()
	mkdirs(t, dir, "src")
	reg, repoID := repoRegistry(t, dir)
	v, tw := bound(t)
	v.Render(reg, nil)

	root, _ := v.Item(repoID)
	tw.Tree.ExpandItem(root)
	tw.Tree.CollapseItem(root)

	prev := source
	called := false
	source = func(path string) ([]string, error) {
		called = true
		return prev(path)
	}
	t.Cleanup(func() { source = prev })

	tw.Tree.ExpandItem(root)
	if called {
		t.Fatal("re-expanding an already loaded node must not read the filesystem again")
	}
	if len(root.Children) != 1 || root.Children[0].DisplayText() != "src" {
		t.Fatalf("children = %+v, want [src] to remain intact", root.Children)
	}
}

func TestDirTreeReexpandingAnAlreadyLoadedNodeWithSeveralChildrenDoesNotReload(t *testing.T) {
	dir := t.TempDir()
	mkdirs(t, dir, "src", "docs")
	reg, repoID := repoRegistry(t, dir)
	v, tw := bound(t)
	v.Render(reg, nil)

	root, _ := v.Item(repoID)
	tw.Tree.ExpandItem(root)
	tw.Tree.CollapseItem(root)

	prev := source
	called := false
	source = func(path string) ([]string, error) {
		called = true
		return prev(path)
	}
	t.Cleanup(func() { source = prev })

	tw.Tree.ExpandItem(root)
	if called {
		t.Fatal("re-expanding a node already holding several real children must not read the filesystem again")
	}
	if len(root.Children) != 2 {
		t.Fatalf("children = %+v, want [docs src] to remain intact", root.Children)
	}
}

func TestDirTreeExpandErrorLeavesTheExpandingNodeWithoutChildren(t *testing.T) {
	dir := t.TempDir()
	mkdirs(t, dir, "src/pkg")
	reg, repoID := repoRegistry(t, dir)
	v, tw := bound(t)
	v.Render(reg, nil)

	root, _ := v.Item(repoID)
	tw.Tree.ExpandItem(root)
	src := findChild(root, "src")
	if src == nil {
		t.Fatal("src missing")
	}

	prev := source
	wantErr := errors.New("boom")
	srcPath := filepath.Join(dir, "src")
	source = func(path string) ([]string, error) {
		if path == srcPath {
			return nil, wantErr
		}
		return prev(path)
	}
	t.Cleanup(func() { source = prev })

	tw.Tree.ExpandItem(src)
	if len(src.Children) != 0 {
		t.Fatalf("children = %+v, want none when the read fails", src.Children)
	}
}

func TestDirTreeExpandingIgnoresItemsWithoutADirEntryTag(t *testing.T) {
	v, _ := bound(t)
	v.handleExpanded(treeview.NewItem("ghost"))
}

func TestDirTreeReadErrorLeavesTheNodeWithoutChildren(t *testing.T) {
	dir := t.TempDir()
	mkdirs(t, dir, "src")
	reg, repoID := repoRegistry(t, dir)
	v, _ := bound(t)

	prev := source
	wantErr := errors.New("boom")
	source = func(path string) ([]string, error) {
		if path == dir {
			return nil, wantErr
		}
		return prev(path)
	}
	t.Cleanup(func() { source = prev })

	v.Render(reg, nil)
	root, _ := v.Item(repoID)
	if root.HasChildren() {
		t.Fatal("a repository whose directory cannot be read must not show an expand arrow")
	}
}

func TestDirTreeExpandedStateSurvivesRerender(t *testing.T) {
	dir := t.TempDir()
	mkdirs(t, dir, "src")
	reg, repoID := repoRegistry(t, dir)
	v, tw := bound(t)
	v.Render(reg, nil)

	root, _ := v.Item(repoID)
	tw.Tree.ExpandItem(root)

	v.Render(reg, nil)
	root, _ = v.Item(repoID)
	if !root.Expanded {
		t.Fatal("expanded repository must stay expanded across re-render")
	}
	if len(root.Children) != 1 || root.Children[0].DisplayText() != "src" {
		t.Fatalf("children after re-render = %+v, want real [src], not a stub", root.Children)
	}
}

func TestDirTreeNestedExpandedStateSurvivesRerender(t *testing.T) {
	dir := t.TempDir()
	mkdirs(t, dir, "src/pkg")
	reg, repoID := repoRegistry(t, dir)
	v, tw := bound(t)
	v.Render(reg, nil)

	root, _ := v.Item(repoID)
	tw.Tree.ExpandItem(root)
	src := findChild(root, "src")
	tw.Tree.ExpandItem(src)

	v.Render(reg, nil)
	root, _ = v.Item(repoID)
	src = findChild(root, "src")
	if src == nil || !src.Expanded {
		t.Fatal("nested expanded directory must stay expanded across re-render")
	}
	if len(src.Children) != 1 || src.Children[0].DisplayText() != "pkg" {
		t.Fatalf("src children after re-render = %+v, want real [pkg]", src.Children)
	}
}

func TestDirTreeCollapsingThenRerenderingShowsAStubAgain(t *testing.T) {
	dir := t.TempDir()
	mkdirs(t, dir, "src")
	reg, repoID := repoRegistry(t, dir)
	v, tw := bound(t)
	v.Render(reg, nil)

	root, _ := v.Item(repoID)
	tw.Tree.ExpandItem(root)
	tw.Tree.CollapseItem(root)

	v.Render(reg, nil)
	root, _ = v.Item(repoID)
	if root.Expanded {
		t.Fatal("collapsed repository must stay collapsed across re-render")
	}
	if len(root.Children) != 1 || root.Children[0].DisplayText() != "" {
		t.Fatalf("children after re-render = %+v, want a stub", root.Children)
	}
}

func TestDirTreeSelectingADirectoryFiresOnSelectDirectory(t *testing.T) {
	dir := t.TempDir()
	mkdirs(t, dir, "src/pkg")
	reg, repoID := repoRegistry(t, dir)
	v, tw := bound(t)
	v.Render(reg, nil)

	root, _ := v.Item(repoID)
	tw.Tree.ExpandItem(root)
	src := findChild(root, "src")
	tw.Tree.ExpandItem(src)
	pkg := findChild(src, "pkg")
	if pkg == nil {
		t.Fatal("pkg missing")
	}

	var gotRepo, gotRel string
	v.OnSelectDirectory = func(repoID, relPath string) { gotRepo, gotRel = repoID, relPath }
	tw.Tree.SetSelectedItem(pkg)

	if gotRepo != repoID || gotRel != "src/pkg" {
		t.Fatalf("OnSelectDirectory(%q, %q), want (%q, %q)", gotRepo, gotRel, repoID, "src/pkg")
	}
}

func TestDirTreeSelectingADirectoryDoesNotFireOnSelect(t *testing.T) {
	dir := t.TempDir()
	mkdirs(t, dir, "src")
	reg, repoID := repoRegistry(t, dir)
	v, tw := bound(t)
	v.Render(reg, nil)

	root, _ := v.Item(repoID)
	tw.Tree.ExpandItem(root)
	src := findChild(root, "src")

	called := false
	v.OnSelect = func(string) { called = true }
	tw.Tree.SetSelectedItem(src)

	if called {
		t.Fatal("selecting a directory must not fire OnSelect")
	}
}

func TestDirTreeSelectingTheRepositoryItselfFiresOnSelectNotOnSelectDirectory(t *testing.T) {
	dir := t.TempDir()
	mkdirs(t, dir, "src")
	reg, repoID := repoRegistry(t, dir)
	v, tw := bound(t)
	v.Render(reg, nil)

	root, _ := v.Item(repoID)
	var selected string
	dirCalled := false
	v.OnSelect = func(id string) { selected = id }
	v.OnSelectDirectory = func(string, string) { dirCalled = true }
	tw.Tree.SetSelectedItem(root)

	if selected != repoID {
		t.Fatalf("selected = %q, want %q", selected, repoID)
	}
	if dirCalled {
		t.Fatal("selecting the repository node must not fire OnSelectDirectory")
	}
}

func TestDirTreeSelectingADirectoryWithoutAHandlerDoesNotPanic(t *testing.T) {
	dir := t.TempDir()
	mkdirs(t, dir, "src")
	reg, repoID := repoRegistry(t, dir)
	v, tw := bound(t)
	v.Render(reg, nil)

	root, _ := v.Item(repoID)
	tw.Tree.ExpandItem(root)
	src := findChild(root, "src")
	tw.Tree.SetSelectedItem(src)
}

func TestDisplayNameAppendsTheBranchForRepositoriesAndWorktreesButNotGroups(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	reg := repo.New(cfg)
	group, err := reg.AddGroup("Group", "")
	if err != nil {
		t.Fatal(err)
	}
	main, err := reg.AddRepository("Go.Git", filepath.Join(dir, "main"), "")
	if err != nil {
		t.Fatal(err)
	}
	wt, err := reg.AddWorktree(main.ID, "feature", filepath.Join(dir, "feature"))
	if err != nil {
		t.Fatal(err)
	}
	v, tw := bound(t)
	v.Render(reg, map[string]State{
		main.ID:  {Branch: "develop"},
		wt.ID:    {Branch: "feature"},
		group.ID: {Branch: "ignored-for-groups"},
	})

	_ = tw
	mainItem, _ := v.Item(main.ID)
	if mainItem.DisplayText() != "Go.Git (develop)" {
		t.Fatalf("main text = %q, want %q", mainItem.DisplayText(), "Go.Git (develop)")
	}
	wtItem, _ := v.Item(wt.ID)
	if wtItem.DisplayText() != "feature (feature)" {
		t.Fatalf("worktree text = %q, want %q", wtItem.DisplayText(), "feature (feature)")
	}
	groupItem, _ := v.Item(group.ID)
	if groupItem.DisplayText() != "Group" {
		t.Fatalf("group text = %q, want %q (branch must be ignored)", groupItem.DisplayText(), "Group")
	}
}

func TestDisplayNameOmitsTheBranchSuffixWhenThereIsNone(t *testing.T) {
	dir := t.TempDir()
	reg, repoID := repoRegistry(t, dir)
	v, _ := bound(t)
	v.Render(reg, nil)

	item, _ := v.Item(repoID)
	if item.DisplayText() != "Main" {
		t.Fatalf("text = %q, want %q", item.DisplayText(), "Main")
	}
}
