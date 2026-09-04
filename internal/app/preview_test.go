package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/oops1/gogit/internal/config"
	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/index"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/refs"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
)

func TestPreviewFrames(t *testing.T) {
	dir := os.Getenv("GOGIT_PREVIEW_DIR")
	if dir == "" {
		t.Skip("GOGIT_PREVIEW_DIR not set")
	}
	for _, theme := range []string{config.ThemeDark, config.ThemeLight} {
		a := newTestApp(t)
		a.SetTheme(theme)
		a.SetActiveRepository("demo", false)
		a.Engine().SaveFrames(dir + "/" + theme)
		a.Engine().Start()
		time.Sleep(700 * time.Millisecond)
		a.Engine().Stop()
	}
}

const previewReadmeContent = "# Demo\n\nGo.Git preview repository.\nSecond line describing the diff view.\n"

const previewMainGoContent = "package main\n\nfunc main() {\n\tprintln(\"hello, gogit\")\n}\n"

func seedPreviewChangeCommits(t *testing.T, path, branch string, parent hash.ObjectID) hash.ObjectID {
	t.Helper()
	db, store := withJournalRepo(t, path)
	base := putChangesTree(t, db, map[string]string{
		"README.md": "# Demo\n\nGo.Git preview repository.\n",
		"main.go":   "package main\n\nfunc main() {\n\tprintln(\"hi\")\n}\n",
	})
	baseCommit := putChangesCommit(t, db, base, parent)
	changed := putChangesTree(t, db, map[string]string{
		"README.md": previewReadmeContent,
		"main.go":   previewMainGoContent,
	})
	tip := putChangesCommit(t, db, changed, baseCommit)
	setRef(t, store, refs.BranchName(branch), tip)
	return tip
}

func seedPreviewIndex(t *testing.T, target string) {
	t.Helper()
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

	idx := index.New(index.Version2)
	for name, content := range map[string]string{"README.md": previewReadmeContent, "main.go": previewMainGoContent} {
		id, err := db.Put(object.TypeBlob, []byte(content))
		if err != nil {
			t.Fatal(err)
		}
		idx.Add(index.Entry{Path: name, Mode: object.ModeBlob, ID: id, Stage: index.StageMerged})
	}
	if err := idx.WriteFile(r.IndexFile(), index.Version2); err != nil {
		t.Fatal(err)
	}
}

func seedPreviewWorkingTreeChanges(t *testing.T, path string) {
	t.Helper()
	if err := writeFile(path, "README.md", "# Demo\n\nGo.Git preview repository.\nEdited straight in the working copy.\n"); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(path, "main.go", previewMainGoContent); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(path, "NOTES.md", "Ideas for the next release.\n"); err != nil {
		t.Fatal(err)
	}
}

func TestPreviewOpenedRepository(t *testing.T) {
	dir := os.Getenv("GOGIT_PREVIEW_DIR")
	if dir == "" {
		t.Skip("GOGIT_PREVIEW_DIR not set")
	}
	target := filepath.Join(t.TempDir(), "demo")
	initTestRepoWithBranch(t, target, "main")
	ids := seedJournalCommits(t, target, "main", 8)
	addBranchAndTag(t, target, "feature/preview", "v1.0")
	seedPreviewChangeCommits(t, target, "main", ids[len(ids)-1])
	seedPreviewIndex(t, target)
	seedPreviewWorkingTreeChanges(t, target)

	cfg := config.Default()
	cfg.Repositories = []config.Repository{{ID: "r1", Name: "demo", Path: target}}

	for _, theme := range []string{config.ThemeDark, config.ThemeLight} {
		working := newTestAppWithConfig(t, cfg)
		working.SetTheme(theme)
		working.ActivateRepository("r1")
		waitForJournalRows(t, working, 10)
		waitForWorkingRows(t, working, 2)
		selectFilesRow(t, working, 0)
		working.Engine().SaveFrames(dir + "/working-" + theme)
		working.Engine().Start()
		time.Sleep(700 * time.Millisecond)
		working.Engine().Stop()

		opened := newTestAppWithConfig(t, cfg)
		opened.SetTheme(theme)
		opened.ActivateRepository("r1")
		waitForJournalRows(t, opened, 10)
		selectJournalRow(t, opened, 0)
		waitForFilesRows(t, opened, 2)
		opened.Engine().SaveFrames(dir + "/opened-" + theme)
		opened.Engine().Start()
		time.Sleep(700 * time.Millisecond)
		opened.Engine().Stop()
	}
}
