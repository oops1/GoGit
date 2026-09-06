package settings

import (
	"image"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/i18n"
)

type hintLabel struct {
	*widget.Label
}

func newHintLabel(key string) *hintLabel {
	lbl := widget.NewLabel(i18n.T(key), widget.CurrentTheme().SecondaryText)
	lbl.FontSize = hintFontSize
	return &hintLabel{Label: lbl}
}

func (h *hintLabel) ApplyTheme(t *widget.Theme) {
	h.TextColor = t.SecondaryText
}

func addHint(grid *widget.Grid, key string, row, col, colSpan int) *hintLabel {
	h := newHintLabel(key)
	h.SetGridProps(row, col, 1, colSpan)
	grid.AddChild(h)
	return h
}

func addFieldHint(grid *widget.Grid, key string, row, maxLines int) *hintLabel {
	h := addHint(grid, key, row, 2, 1)
	h.WrapText = true
	h.SetXAMLSize(0, maxLines*hintLineHeight)
	h.SetVAlign(widget.VAlignCenter)
	h.SetMargin(widget.Margin{Left: hintLeftGapFromControl})
	return h
}

func addCheckboxHint(grid *widget.Grid, key string, row int) *hintLabel {
	h := addHint(grid, key, row, 0, 3)
	h.SetXAMLSize(0, hintLineHeight)
	h.SetMargin(widget.Margin{Left: checkboxTextIndent, Top: hintTopGapUnderCheckbox})
	return h
}

func addGroupDescription(grid *widget.Grid, key string, row int) *hintLabel {
	h := addHint(grid, key, row, 0, 3)
	h.SetXAMLSize(0, hintLineHeight)
	return h
}

func (v *View) buildHints() {
	addGroupDescription(v.sectionGeneral, "Dialog.Settings.Group.LanguageAppearance.Desc", 1)
	addGroupDescription(v.sectionGeneral, "Dialog.Settings.Group.Interface.Desc", 10)
	addCheckboxHint(v.sectionGeneral, "Dialog.Settings.ShowToolbar.Hint", 13)
	addCheckboxHint(v.sectionGeneral, "Dialog.Settings.ShowStatusBar.Hint", 16)
	addCheckboxHint(v.sectionGeneral, "Dialog.Settings.ToolbarCaptions.Hint", 19)
	addCheckboxHint(v.sectionGeneral, "Dialog.Settings.JournalFullAuthorName.Hint", 22)
	v.sectionGeneral.SetBounds(v.sectionGeneral.Bounds())

	addFieldHint(v.sectionGit, "Dialog.Settings.LogMaxCount.Hint", 2, 5)
	addCheckboxHint(v.sectionGit, "Dialog.Settings.AutoFetch.Hint", 5)
	addFieldHint(v.sectionGit, "Dialog.Settings.FetchInterval.Hint", 7, 4)
	addFieldHint(v.sectionGit, "Dialog.Settings.DefaultRemote.Hint", 9, 5)
	addCheckboxHint(v.sectionGit, "Dialog.Settings.PruneOnFetch.Hint", 12)
	v.sectionGit.SetBounds(v.sectionGit.Bounds())

	addFieldHint(v.gitAdvancedContent, "Dialog.Settings.WorkTreeDepth.Hint", 0, 5)
	addFieldHint(v.gitAdvancedContent, "Dialog.Settings.PullStrategy.Hint", 2, 7)
	addFieldHint(v.gitAdvancedContent, "Dialog.Settings.ShallowDepth.Hint", 4, 7)
	v.gitAdvancedContent.SetBounds(v.gitAdvancedContent.Bounds())
}

func (v *View) syncAdvancedWidth() {
	want := v.gitAdvancedStack.Bounds().Dx()
	b := v.gitAdvanced.Bounds()
	if b.Dx() == want {
		return
	}
	v.gitAdvanced.SetBounds(image.Rect(b.Min.X, b.Min.Y, b.Min.X+want, b.Max.Y))
}

func (v *View) wireAdvanced() {
	v.syncAdvancedWidth()
	v.gitAdvanced.OnExpandedChanged = func(bool) {
		before := v.gitAdvanced.Bounds().Dy()
		v.syncAdvancedWidth()
		v.gitAdvancedStack.Relayout()
		v.syncAdvancedWidth()
		after := v.gitAdvanced.Bounds().Dy()
		if delta := after - before; delta != 0 {
			b := v.dlg.Bounds()
			v.dlg.Resize(b.Dx(), b.Dy()+delta)
		}
	}
}
