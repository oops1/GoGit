package app

import (
	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/changes"
	"github.com/oops1/gogit/internal/ui/icons"
)

const filesStatusIconSize = 16

var filesStatusButtons = map[changes.StatusFilter]string{
	changes.FilterStaged:    "filesFilterStaged",
	changes.FilterModified:  "filesFilterModified",
	changes.FilterAdded:     "filesFilterAdded",
	changes.FilterDeleted:   "filesFilterDeleted",
	changes.FilterRenamed:   "filesFilterRenamed",
	changes.FilterUntracked: "filesFilterUntracked",
	changes.FilterIgnored:   "filesFilterIgnored",
	changes.FilterConflict:  "filesFilterConflict",
}

var filesStatusTipKeys = map[changes.StatusFilter]string{
	changes.FilterStaged:    "Files.Filter.Status.Staged",
	changes.FilterModified:  "Files.Filter.Status.Modified",
	changes.FilterAdded:     "Files.Filter.Status.Added",
	changes.FilterDeleted:   "Files.Filter.Status.Deleted",
	changes.FilterRenamed:   "Files.Filter.Status.Renamed",
	changes.FilterUntracked: "Files.Filter.Status.Untracked",
	changes.FilterIgnored:   "Files.Filter.Status.Ignored",
	changes.FilterConflict:  "Files.Filter.Status.Conflict",
}

func (a *App) filesStatusButton(f changes.StatusFilter) *widget.Button {
	return a.named[filesStatusButtons[f]].(*widget.Button)
}

func (a *App) restoreFilesStatusFilter() {
	disabled := changes.NormalizeStatusFilters(a.cfg.UI.FilesStatusFilter)
	a.filesMu.Lock()
	a.filesStatusAllowed = changes.AllowedStatusFilters(disabled)
	a.filesMu.Unlock()
}

func (a *App) wireFilesStatusButtons() {
	for _, f := range changes.AllStatusFilters {
		filter := f
		a.filesStatusButton(filter).OnClick = func() { a.toggleFilesStatusFilter(filter) }
	}
	a.retranslateFilesStatusButtons()
}

func (a *App) toggleFilesStatusFilter(f changes.StatusFilter) {
	a.filesMu.Lock()
	next := make(map[changes.StatusFilter]bool, len(a.filesStatusAllowed))
	for k, v := range a.filesStatusAllowed {
		next[k] = v
	}
	next[f] = !next[f]
	a.filesStatusAllowed = next
	a.filesMu.Unlock()
	a.saveFilesStatusFilter()
	a.applyFilesStatusButtonVisuals(themeFor(a.EffectiveTheme()))
	a.applyFilesFilter()
}

func (a *App) saveFilesStatusFilter() {
	a.filesMu.Lock()
	allowed := a.filesStatusAllowed
	a.filesMu.Unlock()
	disabled := changes.DisabledStatusFilters(allowed)
	names := make([]string, len(disabled))
	for i, f := range disabled {
		names[i] = string(f)
	}
	a.cfg.UI.FilesStatusFilter = names
	if err := a.cfg.Save(a.paths.ConfigFile()); err != nil {
		a.log.Warn("save config failed", "error", err)
	}
}

func (a *App) applyFilesStatusButtonVisuals(t *widget.Theme) {
	a.filesMu.Lock()
	allowed := a.filesStatusAllowed
	a.filesMu.Unlock()
	for _, f := range changes.AllStatusFilters {
		btn := a.filesStatusButton(f)
		btn.Icon = icons.Status(string(f), filesStatusIconSize)
		btn.IconSize = filesStatusIconSize
		btn.IconPos = widget.IconOnly
		btn.HoverBG = t.BtnHoverBG
		btn.PressedBG = t.BtnPressedBG
		if allowed[f] {
			btn.Background = t.BtnPressedBG
		} else {
			btn.Background = t.PanelBG
		}
	}
}

func (a *App) retranslateFilesStatusButtons() {
	for f, key := range filesStatusTipKeys {
		a.filesStatusButton(f).SetToolTip(i18n.T(key))
	}
}
