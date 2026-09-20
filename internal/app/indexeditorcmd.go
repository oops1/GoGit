package app

import (
	"context"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/gitcore/ops"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/indexeditor"
)

var newIndexEditorView = indexeditor.NewView

var readIndexSides = ops.ReadIndexSides

var saveIndexContent = ops.SaveIndexContent

func (a *App) registerIndexEditorHandlers() {
	a.handlers[CmdIndexEditor] = a.editSelectedInIndex
}

func (a *App) editSelectedInIndex() {
	rows := a.selectedWorkingRows()
	if len(rows) == 0 {
		return
	}
	a.openIndexEditor(rows[0].RelPath)
}

func (a *App) openIndexEditor(path string) {
	if a.opened() == nil {
		return
	}
	var sides ops.IndexSides
	a.startRead(func(ctx context.Context, r *gitrepo.Repository) error {
		read, err := readIndexSides(ctx, r, path)
		sides = read
		return err
	}, func(err error) {
		if err != nil {
			a.log.Warn("read index sides failed", "path", path, "error", err)
			a.statusLabel.SetText(i18n.Tf("Status.IndexEditorFailed", err))
			return
		}
		a.showIndexEditor(sides)
	})
}

func (a *App) showIndexEditor(sides ops.IndexSides) {
	if sides.Binary {
		a.statusLabel.SetText(i18n.Tf("Status.IndexEditorBinary", sides.Path))
		return
	}
	view, err := newIndexEditorView()
	if err != nil {
		a.log.Warn("open index editor failed", "error", err)
		a.statusLabel.SetText(i18n.Tf("Status.IndexEditorFailed", err))
		return
	}
	view.Show(indexeditor.File{
		Path:      sides.Path,
		HeadLabel: a.headLabel(),
		Head:      sides.Head,
		Index:     sides.Index,
		Working:   sides.Working,
	})
	if bounds := a.root.Bounds(); !bounds.Empty() {
		view.FitWithin(bounds.Dx(), bounds.Dy())
	}
	view.OnSave = func(content string) { a.saveIndexEdit(view, content, false) }
	view.OnClose = func() { a.closeIndexEditor(view) }
	view.Dialog().OnClosing = func() bool {
		if !view.Modified() {
			return true
		}
		a.closeIndexEditor(view)
		return false
	}
	a.showModal(view.Dialog(), view)
}

func (a *App) closeIndexEditor(view *indexeditor.View) {
	if !view.Modified() {
		a.eng.CloseModal(view.Dialog())
		return
	}
	a.askSave(i18n.T("Dialog.IndexEditor.Unsaved.Title"), i18n.Tf("Dialog.IndexEditor.Unsaved.Message", view.Path()), func(answer widget.MessageBoxResult) {
		switch answer {
		case widget.MBResultYes:
			a.saveIndexEdit(view, view.Result(), true)
		case widget.MBResultNo:
			a.eng.CloseModal(view.Dialog())
		}
	})
}

func (a *App) saveIndexEdit(view *indexeditor.View, content string, closeAfter bool) {
	path := view.Path()
	a.startWrite(func(ctx context.Context, r *gitrepo.Repository) error {
		return saveIndexContent(ctx, r, path, []byte(content))
	}, func(err error) {
		if err != nil {
			a.log.Warn("save index content failed", "path", path, "error", err)
			view.SaveFailed(err)
			return
		}
		view.Saved(content)
		a.statusLabel.SetText(i18n.Tf("Status.IndexEditorSaved", path))
		if closeAfter {
			a.eng.CloseModal(view.Dialog())
		}
		a.RefreshRepository()
	})
}

func (a *App) headLabel() string {
	if name := a.currentBranchName(); name != "" {
		return name
	}
	return i18n.T("Dialog.IndexEditor.Side.Head")
}
