package settings

import (
	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/i18n"
)

type hintLabel struct {
	*widget.Label
}

func newHintLabel(key string) *hintLabel {
	lbl := widget.NewLabel(i18n.T(key), widget.CurrentTheme().SecondaryText)
	lbl.FontSize = hintFontSize
	lbl.PaddingX = 0
	lbl.PaddingY = 0
	return &hintLabel{Label: lbl}
}

func (h *hintLabel) ApplyTheme(t *widget.Theme) {
	h.TextColor = t.SecondaryText
}

func addHint(grid *widget.Grid, key string, row, col, rowSpan, colSpan int) *hintLabel {
	h := newHintLabel(key)
	h.SetGridProps(row, col, rowSpan, colSpan)
	grid.AddChild(h)
	return h
}

func addFieldHint(grid *widget.Grid, key string, row, maxLines int) *hintLabel {
	h := addHint(grid, key, row, 2, hintFieldRowSpan, 1)
	h.WrapText = true
	h.SetXAMLSize(0, maxLines*hintLineHeight)
	h.SetVAlign(widget.VAlignCenter)
	h.SetMargin(widget.Margin{Left: hintLeftGapFromControl})
	return h
}

func addCheckboxHint(grid *widget.Grid, key string, row int) *hintLabel {
	h := addHint(grid, key, row, 0, 1, 3)
	h.SetXAMLSize(0, hintLineHeight)
	h.SetMargin(widget.Margin{Left: checkboxTextIndent, Top: hintTopGapUnderCheckbox})
	return h
}

func addGroupDescription(grid *widget.Grid, key string, row int) *hintLabel {
	h := addHint(grid, key, row, 0, 1, 3)
	h.SetXAMLSize(0, hintLineHeight)
	return h
}

func (v *View) buildHints() {
	addGroupDescription(v.sectionGeneral, "Dialog.Settings.Group.LanguageAppearance.Desc", 1)
	addFieldHint(v.sectionGeneral, "Dialog.Settings.Language.Hint", 3, 2)
	addFieldHint(v.sectionGeneral, "Dialog.Settings.Theme.Hint", 5, 2)
	addGroupDescription(v.sectionGeneral, "Dialog.Settings.Group.Interface.Desc", 10)
	addCheckboxHint(v.sectionGeneral, "Dialog.Settings.ShowToolbar.Hint", 13)
	addCheckboxHint(v.sectionGeneral, "Dialog.Settings.ShowStatusBar.Hint", 16)
	addCheckboxHint(v.sectionGeneral, "Dialog.Settings.ToolbarCaptions.Hint", 19)
	addCheckboxHint(v.sectionGeneral, "Dialog.Settings.JournalFullAuthorName.Hint", 22)
	v.sectionGeneral.SetBounds(v.sectionGeneral.Bounds())

	addGroupDescription(v.sectionGit, "Dialog.Settings.Group.FetchSync.Desc", 1)
	addFieldHint(v.sectionGit, "Dialog.Settings.LogMaxCount.Hint", 3, 2)
	addCheckboxHint(v.sectionGit, "Dialog.Settings.AutoFetch.Hint", 6)
	addFieldHint(v.sectionGit, "Dialog.Settings.FetchInterval.Hint", 8, 2)
	addFieldHint(v.sectionGit, "Dialog.Settings.DefaultRemote.Hint", 10, 2)
	addCheckboxHint(v.sectionGit, "Dialog.Settings.PruneOnFetch.Hint", 13)
	v.sectionGit.SetBounds(v.sectionGit.Bounds())

	addFieldHint(v.gitAdvancedContent, "Dialog.Settings.WorkTreeDepth.Hint", 0, 2)
	addFieldHint(v.gitAdvancedContent, "Dialog.Settings.PullStrategy.Hint", 2, 2)
	addFieldHint(v.gitAdvancedContent, "Dialog.Settings.ShallowDepth.Hint", 4, 2)
	v.gitAdvancedContent.SetBounds(v.gitAdvancedContent.Bounds())

	addGroupDescription(v.sectionCredentials, "Dialog.Settings.Credentials.Desc", 1)
	v.sectionCredentials.SetBounds(v.sectionCredentials.Bounds())

	addGroupDescription(v.sectionSSH, "Dialog.Settings.SSH.Desc", 1)
	v.sectionSSH.SetBounds(v.sectionSSH.Bounds())
}

func (v *View) advancedRowHeight() float64 {
	if v.gitAdvanced.IsExpanded {
		return advancedRowExpanded
	}
	return advancedRowCollapsed
}

func (v *View) applyAdvancedRow() {
	height := v.advancedRowHeight()
	v.sectionGit.RowDefs[gitAdvancedRow].Value = height
	for _, s := range v.searchSections {
		if s.grid == v.sectionGit {
			s.originalRows[gitAdvancedRow].Value = height
		}
	}
	v.sectionGit.SetBounds(v.sectionGit.Bounds())
}

func (v *View) wireAdvanced() {
	v.applyAdvancedRow()
	v.gitAdvanced.OnExpandedChanged = func(bool) { v.applyAdvancedRow() }
}
