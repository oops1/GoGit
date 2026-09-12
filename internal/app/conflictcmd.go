package app

import (
	"context"
	"strings"

	"github.com/oops1/headless-gui/v3/widget"

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
		a.saveConflict(view, content, resolved, false)
	}
	view.OnClose = func() { a.closeConflict(view) }
	view.Dialog().OnClosing = func() bool {
		if !view.Modified() {
			return true
		}
		a.closeConflict(view)
		return false
	}
	a.showModal(view.Dialog(), view)
}

func (a *App) closeConflict(view *conflict.View) {
	if !view.Modified() {
		a.eng.CloseModal(view.Dialog())
		return
	}
	a.askSave(i18n.T("Dialog.Conflict.Unsaved.Title"), i18n.Tf("Dialog.Conflict.Unsaved.Message", view.Path()), func(answer widget.MessageBoxResult) {
		switch answer {
		case widget.MBResultYes:
			a.saveConflict(view, view.Result(), view.Unresolved() == 0, true)
		case widget.MBResultNo:
			a.eng.CloseModal(view.Dialog())
		}
	})
}

func (a *App) saveConflict(view *conflict.View, content string, resolved, closeAfter bool) {
	path := view.Path()
	a.startWrite(func(ctx context.Context, r *gitrepo.Repository) error {
		return saveResolution(ctx, r, path, []byte(content), ops.ResolutionOptions{MarkResolved: resolved})
	}, func(err error) {
		if err != nil {
			a.log.Warn("save resolution failed", "path", path, "error", err)
			view.SaveFailed(err)
			return
		}
		view.Saved(content, resolved)
		if resolved {
			a.statusLabel.SetText(i18n.Tf("Status.ConflictSaved", path))
		}
		if resolved || closeAfter {
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
