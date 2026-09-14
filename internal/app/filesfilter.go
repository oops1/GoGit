package app

import (
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/changes"
)

func (a *App) setFilesRows(rows []changes.Row) {
	a.filesMu.Lock()
	a.filesAllRows = rows
	a.filesMu.Unlock()
	a.applyFilesFilter()
}

func (a *App) onFilesFilterChanged(text string) {
	a.filesMu.Lock()
	a.filesFilterQuery = text
	a.filesMu.Unlock()
	a.applyFilesFilter()
}

func (a *App) setFilesDirFilter(dir string) {
	a.filesMu.Lock()
	a.filesDirFilter = dir
	a.filesMu.Unlock()
	a.applyFilesFilter()
}

func (a *App) clearFilesDirFilter() {
	a.setFilesDirFilter("")
}

func (a *App) applyFilesFilter() {
	a.filesMu.Lock()
	rows := a.filesAllRows
	query := a.filesFilterQuery
	allowed := a.filesStatusAllowed
	dir := a.filesDirFilter
	mode := a.filesMode
	a.filesMu.Unlock()

	filtered := changes.FilterRowsByStatus(rows, query, allowed)
	if mode == filesModeWorking {
		filtered = changes.FilterRowsByDirectory(filtered, dir, a.cfg.UI.FilesSubdirectories)
	}
	items := make([]interface{}, len(filtered))
	for i, r := range filtered {
		items[i] = r
	}
	a.filesItems.SetItems(items)
	a.setFilesCounterText(len(filtered), len(rows), len(filtered) != len(rows))
	a.selectPendingFilesRow(filtered)
}

func (a *App) selectFilesPathWhenShown(path string) {
	a.filesMu.Lock()
	a.filesPendingPath = path
	a.filesMu.Unlock()
}

func (a *App) selectPendingFilesRow(rows []changes.Row) {
	a.filesMu.Lock()
	path := a.filesPendingPath
	a.filesMu.Unlock()
	if path == "" {
		return
	}
	for index, row := range rows {
		if row.RelPath == path && row.Status != changes.RowConflict {
			a.selectFilesPathWhenShown("")
			a.filesGrid.Data().Grid.SetSelectedIndex(index)
			return
		}
	}
}

func (a *App) setFilesCounterText(shown, total int, filtered bool) {
	if a.filesFilterLabel == nil {
		return
	}
	if !filtered {
		a.filesFilterLabel.SetText(i18n.Tf("Files.Filter.Count", total))
		return
	}
	a.filesFilterLabel.SetText(i18n.Tf("Files.Filter.CountFiltered", shown, total))
}
