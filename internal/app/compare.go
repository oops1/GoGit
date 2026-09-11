package app

import (
	"cmp"
	"context"
	"path/filepath"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/gitcore/diff"
	"github.com/oops1/gogit/internal/gitcore/worktree"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/changes"
	"github.com/oops1/gogit/internal/ui/compare"
)

var (
	newCompareView     = compare.NewView
	showSaveFileDialog = func(eng widget.ModalShower, opts widget.FileDialogOptions, cb func(string, bool)) *widget.FileDialog {
		return widget.NewMessageBox(eng).ShowSaveFile(opts, cb)
	}
)

type compareSide struct {
	path     string
	title    string
	note     string
	text     []byte
	readOnly bool
}

func (a *App) openCompare() {
	left, right := comparePair(a.workingRoot(), a.orderedWorkingPaths())
	a.openCompareFiles(left, right)
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

func (a *App) orderedWorkingPaths() []string {
	selected := map[string]bool{}
	for _, path := range a.selectedWorkingPaths() {
		selected[path] = true
	}
	var ordered []string
	for i := range a.filesItems.Count() {
		if row, ok := a.filesItems.Get(i).(changes.Row); ok && selected[row.RelPath] {
			ordered = append(ordered, row.RelPath)
		}
	}
	return ordered
}

func (a *App) openCompareFiles(left, right string) {
	a.openCompareSides(compareSide{path: left}, compareSide{path: right})
}

func (a *App) openCompareSides(left, right compareSide) {
	view, err := newCompareView()
	if err != nil {
		a.log.Warn("open compare window failed", "error", err)
		return
	}
	for side, content := range map[widget.DiffSide]compareSide{widget.DiffLeft: left, widget.DiffRight: right} {
		fillCompareSide(view, side, content)
	}
	view.OnPick = func(side widget.DiffSide) { a.pickCompareFile(view, side) }
	view.OnSaveAs = func(side widget.DiffSide) { a.saveCompareAs(view, side) }
	view.OnClose = func() { a.closeCompare(view) }
	if bounds := a.root.Bounds(); !bounds.Empty() {
		view.FitWithin(bounds.Dx(), bounds.Dy())
	}
	a.showModal(view.Dialog(), view)
}

func fillCompareSide(view *compare.View, side widget.DiffSide, content compareSide) {
	switch {
	case content.path != "":
		_ = view.Load(side, content.path)
	case content.title != "":
		view.Diff().SetText(side, content.title, content.note, string(content.text))
	}
	view.Diff().SetReadOnly(side, content.readOnly)
}

func (a *App) onFilesRowActivated(_ int, item any) {
	row, ok := item.(changes.Row)
	o := a.opened()
	if !ok || o == nil {
		return
	}
	a.filesMu.Lock()
	mode, files, entries := a.filesMode, a.currentFiles, a.currentEntries
	a.filesMu.Unlock()
	var left, right compareSide
	var err error
	switch mode {
	case filesModeCommit:
		file, found := fileOf(files, row)
		if !found {
			return
		}
		left, right, err = commitSides(o, file, a.selectedCommit.String())
	default:
		entry, found := entryOf(entries, row)
		if !found {
			return
		}
		left, right, err = workingSides(context.Background(), o, entry)
	}
	if err != nil {
		a.log.Warn("compare file failed", "path", row.RelPath, "error", err)
		return
	}
	a.openCompareSides(left, right)
}

func workingSides(ctx context.Context, o *openedRepository, entry worktree.Entry) (compareSide, compareSide, error) {
	wt := o.currentWorktree()
	oldPath := cmp.Or(entry.OrigPath, entry.Path)
	_, id, err := wt.HeadBlob(ctx, oldPath)
	if err != nil {
		return compareSide{}, compareSide{}, err
	}
	data, present, err := blobData(o.db, id)
	if err != nil {
		return compareSide{}, compareSide{}, err
	}
	left := compareSide{title: i18n.T("Dialog.Compare.Head"), note: oldPath, text: data, readOnly: true}
	if !present {
		left.title = i18n.T("Dialog.Compare.NotInRepository")
	}
	right := compareSide{path: filepath.Join(o.repo.WorkTree(), filepath.FromSlash(entry.Path))}
	if entry.Unstaged == worktree.StatusDeleted || entry.Staged == worktree.StatusDeleted {
		right = compareSide{title: i18n.T("Dialog.Compare.Deleted"), note: entry.Path, readOnly: true}
	}
	return left, right, nil
}

func commitSides(o *openedRepository, file diff.File, commit string) (compareSide, compareSide, error) {
	oldData, oldPresent, err := blobData(o.db, file.OldID)
	if err != nil {
		return compareSide{}, compareSide{}, err
	}
	newData, newPresent, err := blobData(o.db, file.NewID)
	if err != nil {
		return compareSide{}, compareSide{}, err
	}
	left := compareSide{title: i18n.T("Dialog.Compare.Parent"), note: cmp.Or(file.OldPath, file.NewPath), text: oldData, readOnly: true}
	if !oldPresent {
		left.title = i18n.T("Dialog.Compare.NotInParent")
	}
	right := compareSide{title: shortCommit(commit), note: cmp.Or(file.NewPath, file.OldPath), text: newData, readOnly: true}
	if !newPresent {
		right.title = i18n.T("Dialog.Compare.Deleted")
	}
	return left, right, nil
}

func shortCommit(id string) string {
	return id[:min(len(id), compareShortHash)]
}

const compareShortHash = 7

func (a *App) pickCompareFile(view *compare.View, side widget.DiffSide) {
	opts := widget.FileDialogOptions{Title: i18n.T(compareSideKey(side, "Dialog.Compare.PickLeft", "Dialog.Compare.PickRight")), StartDir: a.compareStartDir(view, side)}
	showOpenFileDialog(a.eng, opts, func(path string, ok bool) {
		if ok {
			view.Diff().SetReadOnly(side, false)
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
