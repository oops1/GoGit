package app

import (
	"context"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/ops"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/changes"
	"github.com/oops1/gogit/internal/ui/commitdetails"
	"github.com/oops1/gogit/internal/ui/journal"
)

var readDetails = ops.Details

func (a *App) showCommitDetails(id hash.ObjectID) {
	if id.IsZero() {
		a.detailsView.Clear()
		return
	}
	var details ops.CommitDetails
	a.startRead(func(ctx context.Context, r *gitrepo.Repository) error {
		read, err := readDetails(ctx, r, id.String(), ops.DetailsOptions{})
		details = read
		return err
	}, func(err error) {
		if err != nil {
			a.log.Warn("read commit details failed", "commit", id.String(), "error", err)
			a.statusLabel.SetText(i18n.Tf("Status.DetailsFailed", err))
			return
		}
		if a.selectedCommit == details.Commit {
			a.detailsView.SetCopyHandler(a.copyToClipboard)
			a.detailsView.SetParentHandler(a.selectJournalCommit)
			a.detailsView.Show(detailsModel(details))
		}
	})
}

func detailsModel(details ops.CommitDetails) commitdetails.Details {
	model := commitdetails.Details{
		Commit:    details.Commit,
		Parents:   details.Parents,
		Author:    details.Author.Name,
		AuthorAt:  details.Author.When,
		Committer: details.Committer.Name,
		CommitAt:  details.Committer.When,
		Message:   details.Message,
		Branches:  details.Branches,
		Tags:      details.Tags,
		MoreFiles: details.MoreFiles,
	}
	for _, file := range details.Changes {
		model.Changes = append(model.Changes, commitdetails.Change{
			Status:  string(changes.DiffRowStatus(file.Status)),
			Path:    file.NewPath,
			Old:     file.OldPath,
			Added:   file.Added(),
			Deleted: file.Deleted(),
		})
	}
	for _, file := range details.Files {
		model.Files = append(model.Files, commitdetails.File{Path: file.Path, Size: file.Size})
	}
	return model
}

func (a *App) selectJournalCommit(id hash.ObjectID) {
	grid := a.journalGrid().Grid
	items := grid.ItemsSource()
	for index := range items.Count() {
		if row, ok := items.Get(index).(journal.Row); ok && row.ID == id {
			grid.SetSelectedIndex(index)
			return
		}
	}
}
