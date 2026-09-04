package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/oops1/gogit/internal/config"
	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/refs"
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

func seedPreviewChangeCommits(t *testing.T, path, branch string, parent hash.ObjectID) hash.ObjectID {
	t.Helper()
	db, store := withJournalRepo(t, path)
	base := putChangesTree(t, db, map[string]string{
		"README.md": "# Demo\n\nGo.Git preview repository.\n",
		"main.go":   "package main\n\nfunc main() {\n\tprintln(\"hi\")\n}\n",
	})
	baseCommit := putChangesCommit(t, db, base, parent)
	changed := putChangesTree(t, db, map[string]string{
		"README.md": "# Demo\n\nGo.Git preview repository.\nSecond line describing the diff view.\n",
		"main.go":   "package main\n\nfunc main() {\n\tprintln(\"hello, gogit\")\n}\n",
	})
	tip := putChangesCommit(t, db, changed, baseCommit)
	setRef(t, store, refs.BranchName(branch), tip)
	return tip
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

	for _, theme := range []string{config.ThemeDark, config.ThemeLight} {
		cfg := config.Default()
		cfg.Repositories = []config.Repository{{ID: "r1", Name: "demo", Path: target}}
		a := newTestAppWithConfig(t, cfg)
		a.SetTheme(theme)
		a.ActivateRepository("r1")
		waitForJournalRows(t, a, 10)
		selectJournalRow(t, a, 0)
		waitForFilesRows(t, a, 2)
		a.Engine().SaveFrames(dir + "/opened-" + theme)
		a.Engine().Start()
		time.Sleep(700 * time.Millisecond)
		a.Engine().Stop()
	}
}
