package app

import (
	"context"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/ops"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/conflict"
)

var newConflictView = conflict.NewView

var readConflict = ops.ReadConflict

var saveResolution = ops.SaveResolution

func (a *App) openConflictEditor(path string) {
	if a.opened() == nil {
		return
	}
	var file ops.ConflictFile
	a.startRead(func(ctx context.Context, r *gitrepo.Repository) error {
		read, err := readConflict(ctx, r, path)
		file = read
		return err
	}, func(err error) {
		if err != nil {
			a.log.Warn("read conflict failed", "path", path, "error", err)
			a.statusLabel.SetText(i18n.Tf("Status.ConflictOpenFailed", err))
			return
		}
		a.showConflictEditor(file)
	})
}

func (a *App) showConflictEditor(file ops.ConflictFile) {
	if file.Binary {
		a.statusLabel.SetText(i18n.Tf("Status.ConflictOpenFailed", i18n.T("Dialog.Conflict.Binary")))
		return
	}
	view, err := newConflictView()
	if err != nil {
		a.log.Warn("open conflict dialog failed", "error", err)
		a.statusLabel.SetText(i18n.Tf("Status.ConflictOpenFailed", err))
		return
	}
	view.Show(conflict.File{
		Path:         file.Path,
		OursLabel:    a.oursLabel(),
		BaseLabel:    i18n.T("Dialog.Conflict.Side.Base"),
		TheirsLabel:  a.theirsLabel(),
		Blocks:       file.Blocks,
		Style:        file.Style,
		FinalNewline: endsWithNewline(file),
	})
	if bounds := a.root.Bounds(); !bounds.Empty() {
		view.FitWithin(bounds.Dx(), bounds.Dy())
	}
	view.OnSave = func(content string, resolved bool) {
		a.saveConflict(view, file.Path, content, resolved)
	}
	view.OnClose = func() { a.eng.CloseModal(view.Dialog()) }
	view.Dialog().CancelAction = view.OnClose
	a.showModal(view.Dialog(), view)
}

func (a *App) saveConflict(view *conflict.View, path, content string, resolved bool) {
	a.startWrite(func(ctx context.Context, r *gitrepo.Repository) error {
		return saveResolution(ctx, r, path, []byte(content), ops.ResolutionOptions{MarkResolved: resolved})
	}, func(err error) {
		if err != nil {
			a.log.Warn("save resolution failed", "path", path, "error", err)
			view.SaveFailed(err)
			return
		}
		view.Saved(resolved)
		if resolved {
			a.statusLabel.SetText(i18n.Tf("Status.ConflictSaved", path))
			a.eng.CloseModal(view.Dialog())
		}
		a.RefreshRepository()
	})
}

func (a *App) oursLabel() string {
	if name := a.currentBranchName(); name != "" {
		return name
	}
	return i18n.T("Dialog.Conflict.Side.Ours")
}

func (a *App) theirsLabel() string { return theirsName(a.workingMergeState()) }

func theirsName(state ops.MergeState) string {
	if state.InProgress() {
		if name := strings.TrimSpace(operationSubject(state)); name != "" {
			return name
		}
	}
	return i18n.T("Dialog.Conflict.Side.Theirs")
}

func endsWithNewline(file ops.ConflictFile) bool {
	for _, side := range [][]byte{file.Ours, file.Theirs, file.Base} {
		if len(side) > 0 {
			return side[len(side)-1] == '\n'
		}
	}
	return false
}
