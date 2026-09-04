package app

import (
	"path/filepath"
	"testing"

	"github.com/oops1/headless-gui/v3/widget"
	"github.com/oops1/headless-gui/v3/widget/treeview"

	"github.com/oops1/gogit/internal/config"
	"github.com/oops1/gogit/internal/gitcore/index"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/refs"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
)

func buildModifiedNestedFilesFixture(t *testing.T, target string) {
	t.Helper()
	initTestRepoWithBranch(t, target, "main")
	r, err := gitrepo.Open(target, gitrepo.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	db, err := odb.Open(r.ObjectsDir(), odb.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	store, err := refs.Open(refs.Options{GitDir: r.GitDir(), CommonDir: r.CommonDir(), Committer: testCommitter})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()

	mainID := putChangesBlob(t, db, "package main\n")
	utilID := putChangesBlob(t, db, "package pkg\n")
	readmeID := putChangesBlob(t, db, "hi\n")

	pkgTree := &object.Tree{Entries: []object.TreeEntry{{Mode: object.ModeBlob, Name: "util.go", ID: utilID}}}
	pkgTree.Sort()
	pkgTreeID, err := db.PutObject(pkgTree)
	if err != nil {
		t.Fatal(err)
	}
	srcTree := &object.Tree{Entries: []object.TreeEntry{
		{Mode: object.ModeBlob, Name: "main.go", ID: mainID},
		{Mode: object.ModeTree, Name: "pkg", ID: pkgTreeID},
	}}
	srcTree.Sort()
	srcTreeID, err := db.PutObject(srcTree)
	if err != nil {
		t.Fatal(err)
	}
	rootTree := &object.Tree{Entries: []object.TreeEntry{
		{Mode: object.ModeTree, Name: "src", ID: srcTreeID},
		{Mode: object.ModeBlob, Name: "readme.md", ID: readmeID},
	}}
	rootTree.Sort()
	rootTreeID, err := db.PutObject(rootTree)
	if err != nil {
		t.Fatal(err)
	}
	commitID := putChangesCommit(t, db, rootTreeID)
	setRef(t, store, refs.BranchName("main"), commitID)

	idx := index.New(index.Version2)
	idx.Add(index.Entry{Path: "src/main.go", Mode: object.ModeBlob, ID: mainID, Stage: index.StageMerged})
	idx.Add(index.Entry{Path: "src/pkg/util.go", Mode: object.ModeBlob, ID: utilID, Stage: index.StageMerged})
	idx.Add(index.Entry{Path: "readme.md", Mode: object.ModeBlob, ID: readmeID, Stage: index.StageMerged})
	if err := idx.WriteFile(r.IndexFile(), index.Version2); err != nil {
		t.Fatal(err)
	}

	if err := writeFile(filepath.Join(target, "src"), "main.go", "package main\n\nfunc main() {}\n"); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(filepath.Join(target, "src", "pkg"), "util.go", "package pkg\n\nfunc Util() {}\n"); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(target, "readme.md", "hi\n"); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(target, "extra.txt", "extra\n"); err != nil {
		t.Fatal(err)
	}
}

func findRepoTreeChild(item *treeview.TreeViewItem, name string) *treeview.TreeViewItem {
	for _, c := range item.Children {
		if c.DisplayText() == name {
			return c
		}
	}
	return nil
}

func TestActivateRepositoryShowsTheBranchInTheTreeLabelForEveryRepositoryNotJustTheActiveOne(t *testing.T) {
	dir := t.TempDir()
	target1 := filepath.Join(dir, "main")
	target2 := filepath.Join(dir, "other")
	initTestRepoWithBranch(t, target1, "develop")
	initTestRepoWithBranch(t, target2, "feature")
	cfg := config.Default()
	cfg.Repositories = []config.Repository{
		{ID: "r1", Name: "Main", Path: target1},
		{ID: "r2", Name: "Other", Path: target2},
	}
	a := newTestAppWithConfig(t, cfg)

	a.ActivateRepository("r1")

	item1, ok := a.reposView.Item("r1")
	if !ok || item1.DisplayText() != "Main (develop)" {
		t.Fatalf("r1 text = %q, want %q", item1.DisplayText(), "Main (develop)")
	}
	item2, ok := a.reposView.Item("r2")
	if !ok || item2.DisplayText() != "Other (feature)" {
		t.Fatalf("r2 text = %q, want %q (branches show for inactive repositories too)", item2.DisplayText(), "Other (feature)")
	}
}

func TestActivateRepositoryOnDetachedHeadShowsTheShortHashInTheTreeLabel(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	initTestRepo(t, target)
	head := oid(t, "aa")
	detachTestHead(t, target, head)
	cfg := config.Default()
	cfg.Repositories = []config.Repository{{ID: "r1", Name: "Main", Path: target}}
	a := newTestAppWithConfig(t, cfg)

	a.ActivateRepository("r1")

	item, ok := a.reposView.Item("r1")
	want := "Main (" + head.String()[:shortHashLength] + ")"
	if !ok || item.DisplayText() != want {
		t.Fatalf("text = %q, want %q", item.DisplayText(), want)
	}
}

func TestRepositoryWithoutABranchOnDiskShowsNoSuffix(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Repositories = []config.Repository{{ID: "r1", Name: "Main", Path: filepath.Join(dir, "missing")}}
	a := newTestAppWithConfig(t, cfg)

	item, ok := a.reposView.Item("r1")
	if !ok || item.DisplayText() != "Main" {
		t.Fatalf("text = %q, want %q", item.DisplayText(), "Main")
	}
}

func TestRefreshRepositoryUpdatesTheTreeLabelAfterABranchChange(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	initTestRepoWithBranch(t, target, "main")
	a := activatedWorkingApp(t, target)
	waitForWorkingRows(t, a, 0)
	item, _ := a.reposView.Item("r1")
	if item.DisplayText() != "Main (main)" {
		t.Fatalf("text = %q, want %q", item.DisplayText(), "Main (main)")
	}

	head := oid(t, "bb")
	detachTestHead(t, target, head)

	a.Dispatch(CmdRefresh)

	item, _ = a.reposView.Item("r1")
	want := "Main (" + head.String()[:shortHashLength] + ")"
	if item.DisplayText() != want {
		t.Fatalf("text after refresh = %q, want %q", item.DisplayText(), want)
	}
}

func TestApplyingTheThemeDoesNotRereadBranchesFromDisk(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	initTestRepoWithBranch(t, target, "main")
	a := activatedWorkingApp(t, target)
	waitForWorkingRows(t, a, 0)

	calls := 0
	prev := currentBranch
	currentBranch = func(path string) string {
		calls++
		return prev(path)
	}
	t.Cleanup(func() { currentBranch = prev })

	a.SetTheme("dark")

	if calls != 0 {
		t.Fatalf("applying a theme must reuse the cached branches, got %d filesystem reads", calls)
	}
	item, _ := a.reposView.Item("r1")
	if item.DisplayText() != "Main (main)" {
		t.Fatalf("text after theme change = %q, want %q", item.DisplayText(), "Main (main)")
	}
}

func TestSelectingADirectoryInTheReposTreeFiltersTheFilesPanel(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	buildModifiedNestedFilesFixture(t, target)
	a := activatedWorkingApp(t, target)
	waitForWorkingRows(t, a, 3)

	root, ok := a.reposView.Item("r1")
	if !ok {
		t.Fatal("repository item missing")
	}
	tree := a.Widget("reposTree").(*widget.TreeViewWidget)
	tree.Tree.ExpandItem(root)
	src := findRepoTreeChild(root, "src")
	if src == nil {
		t.Fatal("src item missing")
	}

	tree.Tree.SetSelectedItem(src)

	if n := filesRowCountOnDispatcher(t, a); n != 2 {
		t.Fatalf("filtered rows = %d, want 2 (src/main.go and src/pkg/util.go)", n)
	}
}

func TestSelectingTheRepositoryNodeClearsTheDirectoryFilter(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	buildModifiedNestedFilesFixture(t, target)
	a := activatedWorkingApp(t, target)
	waitForWorkingRows(t, a, 3)

	root, _ := a.reposView.Item("r1")
	tree := a.Widget("reposTree").(*widget.TreeViewWidget)
	tree.Tree.ExpandItem(root)
	src := findRepoTreeChild(root, "src")
	tree.Tree.SetSelectedItem(src)
	if n := filesRowCountOnDispatcher(t, a); n != 2 {
		t.Fatalf("filtered rows = %d, want 2", n)
	}

	tree.Tree.SetSelectedItem(root)

	if n := filesRowCountOnDispatcher(t, a); n != 3 {
		t.Fatalf("rows after selecting the repository = %d, want 3 (filter cleared)", n)
	}
}

func TestSelectingADirectoryInAnInactiveRepositoryActivatesItFirst(t *testing.T) {
	dir := t.TempDir()
	target1 := filepath.Join(dir, "main")
	target2 := filepath.Join(dir, "other")
	initTestRepoWithBranch(t, target1, "main")
	buildModifiedNestedFilesFixture(t, target2)
	cfg := config.Default()
	cfg.Repositories = []config.Repository{
		{ID: "r1", Name: "Main", Path: target1},
		{ID: "r2", Name: "Other", Path: target2},
	}
	a := newTestAppWithConfig(t, cfg)
	a.ActivateRepository("r1")
	waitForWorkingRows(t, a, 0)

	root2, ok := a.reposView.Item("r2")
	if !ok {
		t.Fatal("r2 item missing")
	}
	tree := a.Widget("reposTree").(*widget.TreeViewWidget)
	tree.Tree.ExpandItem(root2)
	src := findRepoTreeChild(root2, "src")
	if src == nil {
		t.Fatal("src item missing under the inactive repository")
	}

	tree.Tree.SetSelectedItem(src)

	if a.State().ActiveRepository != "r2" {
		t.Fatalf("active repository = %q, want r2 (selecting a directory of another repository must activate it)", a.State().ActiveRepository)
	}
	waitForWorkingRows(t, a, 2)
	if n := filesRowCountOnDispatcher(t, a); n != 2 {
		t.Fatalf("filtered rows = %d, want 2", n)
	}
}

func TestSelectingADirectoryOfTheAlreadyActiveRepositoryDoesNotReactivate(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	buildModifiedNestedFilesFixture(t, target)
	a := activatedWorkingApp(t, target)
	waitForWorkingRows(t, a, 3)
	before := a.opened()

	root, _ := a.reposView.Item("r1")
	tree := a.Widget("reposTree").(*widget.TreeViewWidget)
	tree.Tree.ExpandItem(root)
	src := findRepoTreeChild(root, "src")
	tree.Tree.SetSelectedItem(src)

	if a.opened() != before {
		t.Fatal("selecting a directory of the active repository must not reopen it")
	}
}
