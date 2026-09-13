package app

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/worktree"
	"github.com/oops1/gogit/internal/ui/journal"
)

func appWithHeldQueue(t *testing.T) (*App, journal.Row) {
	t.Helper()
	target := filepath.Join(t.TempDir(), "main")
	buildWorkingRepoFixture(t, target)
	a := activatedWorkingApp(t, target)
	waitForWorkingRows(t, a, 1)
	waitForJournalRows(t, a, 1)
	waitForWorkingIdle(t, a)
	row := journalRowOnDispatcher(t, a, 0)
	a.stopPostDriver()
	a.stopPostDriver = func() {}
	a.Engine().Flush()
	return a, row
}

func TestACommitDiffCancelledBeforeItIsShownStaysHidden(t *testing.T) {
	a, row := appWithHeldQueue(t)
	ctx, cancel := context.WithCancel(t.Context())

	a.runDiff(ctx, a.opened().db, row.ID)
	cancel()
	a.Engine().Flush()

	if doc := shownDocumentOf(a); doc.OldName != "" || doc.NewName != "" {
		t.Fatalf("a cancelled commit diff was shown: %+v", doc)
	}
}

func TestAWorkingDiffCancelledBeforeItIsShownStaysHidden(t *testing.T) {
	a, _ := appWithHeldQueue(t)
	ctx, cancel := context.WithCancel(t.Context())

	a.runWorkingDiff(ctx, a.opened(), worktree.Entry{Path: "modified.txt", Unstaged: worktree.StatusModified})
	cancel()
	a.Engine().Flush()

	if doc := shownDocumentOf(a); doc.OldName != "" || doc.NewName != "" {
		t.Fatalf("a cancelled working diff was shown: %+v", doc)
	}
}
