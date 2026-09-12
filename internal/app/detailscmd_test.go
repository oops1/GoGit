package app

import (
	"context"
	"errors"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/diff"
	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/ops"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/i18n"
)

func waitForDetails(t *testing.T, a *App, want hash.ObjectID) {
	t.Helper()
	waitForDialog(t, a, func() int {
		if a.detailsView.Details().Commit != want {
			return 0
		}
		return 1
	}, "the commit details")
}

func TestSelectingACommitFillsTheDetailsPanel(t *testing.T) {
	a, target := forkedApp(t, false)
	head := branchTip(t, target, "main")

	readOnDispatcher(t, a, func() bool {
		a.selectedCommit = head
		a.showCommitDetails(head)
		return true
	})

	waitForDetails(t, a, head)
	details := readOnDispatcher(t, a, a.detailsView.Details)
	if details.Author == "" || details.Message == "" {
		t.Fatalf("details = %+v", details)
	}
	if len(details.Changes) == 0 || len(details.Files) == 0 {
		t.Fatalf("details = %+v", details)
	}
}

func TestTheJournalSelectionFillsTheDetailsPanel(t *testing.T) {
	a, target := forkedApp(t, false)
	waitForJournalRows(t, a, 1)
	head := branchTip(t, target, "main")

	selectJournalRow(t, a, 0)

	waitForDetails(t, a, head)
}

func TestAnEmptyCommitClearsTheDetailsPanel(t *testing.T) {
	a, target := forkedApp(t, false)
	head := branchTip(t, target, "main")
	readOnDispatcher(t, a, func() bool {
		a.selectedCommit = head
		a.showCommitDetails(head)
		return true
	})
	waitForDetails(t, a, head)

	readOnDispatcher(t, a, func() bool { a.showCommitDetails(hash.Zero); return true })

	if got := readOnDispatcher(t, a, a.detailsView.Details); !got.Commit.IsZero() {
		t.Fatalf("details = %+v", got)
	}
}

func TestDetailsOfAnotherCommitDoNotOverwriteTheSelectionThatFollowed(t *testing.T) {
	a, target := forkedApp(t, false)
	head := branchTip(t, target, "main")
	other := branchTip(t, target, "feature")
	prev := readDetails
	readDetails = func(ctx context.Context, r *gitrepo.Repository, rev string, opts ops.DetailsOptions) (ops.CommitDetails, error) {
		details, err := prev(ctx, r, rev, opts)
		return details, err
	}
	t.Cleanup(func() { readDetails = prev })

	readOnDispatcher(t, a, func() bool {
		a.selectedCommit = head
		a.showCommitDetails(other)
		return true
	})
	waitForPostQueueDrain(t, a)

	if got := readOnDispatcher(t, a, a.detailsView.Details); got.Commit == other {
		t.Fatalf("details = %+v", got)
	}
}

func TestAFailedDetailsReadIsReported(t *testing.T) {
	a, target := forkedApp(t, false)
	prev := readDetails
	readDetails = func(context.Context, *gitrepo.Repository, string, ops.DetailsOptions) (ops.CommitDetails, error) {
		return ops.CommitDetails{}, errors.New("no details")
	}
	t.Cleanup(func() { readDetails = prev })

	readOnDispatcher(t, a, func() bool { a.showCommitDetails(branchTip(t, target, "main")); return true })

	waitForStatusText(t, a, i18n.Tf("Status.DetailsFailed", errors.New("no details")))
}

func TestTheDetailsModelNamesWhatHappenedToEachFile(t *testing.T) {
	when := object.Signature{Name: "ann", Email: "ann@example.com"}
	model := detailsModel(ops.CommitDetails{
		Commit:    hash.SumSHA1("commit", []byte("head")),
		Author:    when,
		Committer: when,
		Message:   "a subject\n",
		Changes: []diff.File{
			{Status: diff.StatusAdded, NewPath: "added"},
			{Status: diff.StatusRenamed, OldPath: "f", NewPath: "moved"},
		},
		Files:     []ops.TreeFile{{Path: "added", Size: 6}},
		MoreFiles: true,
	})

	if len(model.Changes) != 2 || model.Changes[0].Status != i18n.T("Files.State.Added") {
		t.Fatalf("model = %+v", model)
	}
	if len(model.Files) != 1 || model.Files[0].Size != 6 || !model.MoreFiles {
		t.Fatalf("model = %+v", model)
	}
	if model.Author != "ann" || model.Committer != "ann" {
		t.Fatalf("model = %+v", model)
	}
}
