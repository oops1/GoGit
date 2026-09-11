package app

import (
	"path/filepath"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/compare"
)

var (
	newCompareView     = compare.NewView
	showSaveFileDialog = func(eng widget.ModalShower, opts widget.FileDialogOptions, cb func(string, bool)) *widget.FileDialog {
		return widget.NewMessageBox(eng).ShowSaveFile(opts, cb)
	}
)

func (a *App) openCompare() {
	a.openCompareFiles(comparePair(a.workingRoot(), a.selectedWorkingPaths()))
}

func comparePair(root string, paths []string) (string, string) {
	if root == "" {
		return "", ""
	}
	sides := make([]string, 2)
	for i := 0; i < len(sides) && i < len(paths); i++ {
		sides[i] = filepath.Join(root, filepath.FromSlash(paths[i]))
	}
	return sides[0], sides[1]
}

func (a *App) openCompareFiles(left, right string) {
	view, err := newCompareView()
	if err != nil {
		a.log.Warn("open compare window failed", "error", err)
		return
	}
	for side, path := range map[widget.DiffSide]string{widget.DiffLeft: left, widget.DiffRight: right} {
		if path != "" {
			_ = view.Load(side, path)
		}
	}
	view.OnPick = func(side widget.DiffSide) { a.pickCompareFile(view, side) }
	view.OnSaveAs = func(side widget.DiffSide) { a.saveCompareAs(view, side) }
	view.OnClose = func() { a.closeCompare(view) }
	a.showModal(view.Dialog(), view)
}

func (a *App) pickCompareFile(view *compare.View, side widget.DiffSide) {
	opts := widget.FileDialogOptions{Title: i18n.T(compareSideKey(side, "Dialog.Compare.PickLeft", "Dialog.Compare.PickRight")), StartDir: a.compareStartDir(view, side)}
	showOpenFileDialog(a.eng, opts, func(path string, ok bool) {
		if ok {
			_ = view.Load(side, path)
		}
	})
}

func (a *App) saveCompareAs(view *compare.View, side widget.DiffSide) {
	opts := widget.FileDialogOptions{Title: i18n.T("Dialog.Compare.SaveAs"), StartDir: a.compareStartDir(view, side)}
	showSaveFileDialog(a.eng, opts, func(path string, ok bool) {
		if ok {
			_ = view.SaveAs(side, path)
		}
	})
}

func (a *App) closeCompare(view *compare.View) {
	if !view.Modified() {
		a.eng.CloseModal(view.Dialog())
		return
	}
	a.askConfirm(i18n.T("Dialog.Compare.Unsaved.Title"), i18n.T("Dialog.Compare.Unsaved.Message"), func(ok bool) {
		if ok {
			a.eng.CloseModal(view.Dialog())
		}
	})
}

func (a *App) compareStartDir(view *compare.View, side widget.DiffSide) string {
	for _, s := range []widget.DiffSide{side, side.Other()} {
		if path := view.Diff().FilePath(s); path != "" {
			return filepath.Dir(path)
		}
	}
	return a.workingRoot()
}

func (a *App) workingRoot() string {
	o := a.opened()
	if o == nil {
		return ""
	}
	return o.repo.WorkTree()
}

func compareSideKey(side widget.DiffSide, left, right string) string {
	if side == widget.DiffRight {
		return right
	}
	return left
}
