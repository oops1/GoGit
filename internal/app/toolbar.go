package app

import (
	"image"
	"image/color"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/dialogs/toolbar"
	"github.com/oops1/gogit/internal/ui/icons"
)

const (
	toolbarIconSize       = 24
	toolbarButtonMinWidth = 44
	toolbarButtonMaxWidth = 170
	toolbarButtonPadding  = 12
	toolbarButtonHeight   = 54
	toolbarCompactWidth   = 44
	toolbarCompactHeight  = 36
	toolbarSeparatorWidth = 9
	toolbarMenuArrowWidth = 16
	toolbarItemMargin     = 2
)

const toolbarCaptionFontSize = 8.0

type toolbarSeparator struct {
	widget.Base
	Color color.RGBA
}

func (s *toolbarSeparator) Draw(ctx widget.DrawContext) {
	b := s.Bounds()
	if b.Empty() || s.Color.A == 0 {
		return
	}
	ctx.FillRect(b.Min.X+b.Dx()/2, b.Min.Y+4, 1, max(b.Dy()-8, 1), s.Color)
}

func (s *toolbarSeparator) ApplyTheme(t *widget.Theme) { s.Color = t.Border }

func (*toolbarSeparator) DesiredSize() (int, int) { return toolbarSeparatorWidth, 0 }

type toolbarStretch struct {
	widget.Base
	panel *widget.StackPanel
}

func (*toolbarStretch) Draw(widget.DrawContext) {}

func (s *toolbarStretch) DesiredSize() (int, int) { return s.share(), 0 }

func (s *toolbarStretch) share() int {
	if s.panel == nil {
		return 1
	}
	fixed, stretches := 0, 0
	for _, child := range s.panel.Children() {
		fixed += toolbarItemMargin * 2
		if _, ok := child.(*toolbarStretch); ok {
			stretches++
			continue
		}
		width, _ := toolbarItemSize(child)
		fixed += width
	}
	if stretches == 0 {
		return 1
	}
	free := s.panel.Bounds().Dx() - s.panel.Padding*2 - fixed
	return max(free/stretches, 1)
}

func toolbarItemSize(w widget.Widget) (int, int) {
	if sizer, ok := w.(widget.DesiredSizer); ok {
		if width, height := sizer.DesiredSize(); width > 0 {
			return width, height
		}
	}
	b := w.Bounds()
	return b.Dx(), b.Dy()
}

type toolbarButton struct {
	entry toolbarEntry
	plain *widget.Button
	menu  *widget.MenuButton
}

func (b toolbarButton) button() *widget.Button {
	if b.menu != nil {
		return b.menu.Button
	}
	return b.plain
}

func (b toolbarButton) widget() widget.Widget {
	if b.menu != nil {
		return b.menu
	}
	return b.plain
}

func (a *App) toolbarPanel() (*widget.StackPanel, bool) {
	panel, ok := a.named["toolbar"].(*widget.StackPanel)
	return panel, ok
}

func (a *App) buildToolbar() {
	panel, ok := a.toolbarPanel()
	if !ok {
		return
	}
	panel.ClearChildren()
	panel.Spacing = 0
	a.toolbarButtons = nil
	for _, id := range a.configuredToolbarItems() {
		switch id {
		case toolbar.SeparatorID:
			separator := &toolbarSeparator{Color: widget.CurrentTheme().Border}
			setToolbarMargin(separator)
			panel.AddChild(separator)
		case toolbar.StretchID:
			stretch := &toolbarStretch{panel: panel}
			setToolbarMargin(stretch)
			panel.AddChild(stretch)
		default:
			entry, found := toolbarEntryByID(id)
			if !found {
				continue
			}
			a.addToolbarButton(panel, entry)
		}
	}
	widget.ApplyThemeTree(panel, a.theme())
	a.applyToolbarIcons(a.theme())
	a.refreshToolbarButtons(a.State())
}

type toolbarMarginSetter interface {
	SetMargin(widget.Margin)
}

func setToolbarMargin(w toolbarMarginSetter) {
	w.SetMargin(widget.Margin{Left: toolbarItemMargin, Right: toolbarItemMargin})
}

func (a *App) addToolbarButton(panel *widget.StackPanel, entry toolbarEntry) {
	item := toolbarButton{entry: entry}
	switch {
	case entry.hasMenu() && entry.Command == "":
		item.menu = widget.NewMenuButton(i18n.T(entry.LabelKey))
	case entry.hasMenu():
		cmd := entry.Command
		item.menu = widget.NewSplitButton(i18n.T(entry.LabelKey), func() { a.Dispatch(cmd) })
	default:
		cmd := entry.Command
		item.plain = widget.NewButton(i18n.T(entry.LabelKey))
		item.plain.OnClick = func() { a.Dispatch(cmd) }
	}
	if item.menu != nil {
		menu, items := item.menu, entry.Items
		menu.OnOpening = func() { menu.Items = items(a) }
	}
	button := item.button()
	button.SetToolTip(i18n.T(entry.TipKey))
	setToolbarMargin(button)
	a.named[entry.Name] = item.widget()
	a.toolbarButtons = append(a.toolbarButtons, item)
	panel.AddChild(item.widget())
}

func (a *App) applyToolbarIcons(*widget.Theme) {
	captions := a.cfg.UI.ToolbarCaptions
	for _, item := range a.toolbarButtons {
		styleToolbarButton(item.button(), item.entry.Icon, captions, a.toolbarButtonWidth(item), a.toolbarButtonHeight())
	}
	a.relayoutToolbar()
}

func (a *App) toolbarButtonHeight() int {
	if a.cfg.UI.ToolbarCaptions {
		return toolbarButtonHeight
	}
	return toolbarCompactHeight
}

func (a *App) toolbarButtonWidth(item toolbarButton) int {
	arrow := 0
	if item.menu != nil {
		arrow = toolbarMenuArrowWidth
	}
	if !a.cfg.UI.ToolbarCaptions {
		return toolbarCompactWidth + arrow
	}
	return toolbarCaptionButtonWidth(item.button().Text) + arrow
}

func styleToolbarButton(btn *widget.Button, icon string, captions bool, width, height int) {
	btn.Icon = toolbarIcon(icon)
	btn.IconSize = toolbarIconSize
	btn.FontSize = toolbarCaptionFontSize
	if captions {
		btn.IconPos = widget.IconTop
	} else {
		btn.IconPos = widget.IconOnly
	}
	resizeToolbarButton(btn, width, height)
}

func toolbarIcon(name string) image.Image {
	if img := icons.ToolbarPlain(name, toolbarIconSize); img != nil {
		return img
	}
	return icons.Menu(name, toolbarIconSize, widget.CurrentTheme().LabelText)
}

func toolbarCaptionButtonWidth(text string) int {
	w := max(toolbarIconSize, widget.MeasureUIText(text, toolbarCaptionFontSize)) + toolbarButtonPadding
	return min(max(w, toolbarButtonMinWidth), toolbarButtonMaxWidth)
}

func (a *App) toolbarWidth() int {
	panel, ok := a.toolbarPanel()
	if !ok {
		return 0
	}
	total := panel.Padding * 2
	for _, child := range panel.Children() {
		width, _ := toolbarItemSize(child)
		total += width + toolbarItemMargin*2
	}
	return total
}

func (a *App) relayoutToolbar() {
	panel, ok := a.toolbarPanel()
	if !ok {
		return
	}
	panel.SetBounds(panel.Bounds())
}

func resizeToolbarButton(btn *widget.Button, w, h int) {
	b := btn.Bounds()
	btn.SetBounds(rectOfSize(b.Min.X, b.Min.Y, w, h))
}

func rectOfSize(x, y, w, h int) image.Rectangle {
	return image.Rect(x, y, x+w, y+h)
}

func (a *App) retranslateToolbar() {
	for _, item := range a.toolbarButtons {
		button := item.button()
		button.SetText(i18n.T(item.entry.LabelKey))
		button.SetToolTip(i18n.T(item.entry.TipKey))
	}
	a.applyToolbarIcons(nil)
}

func (a *App) refreshToolbarButtons(state State) {
	for _, item := range a.toolbarButtons {
		entry := item.entry
		if item.menu == nil {
			item.plain.SetEnabled(state.Enabled(entry.Command))
			continue
		}
		item.menu.SetEnabled(state.ActiveRepository != "")
		if entry.Command != "" {
			item.menu.SetActionEnabled(state.Enabled(entry.Command))
		}
		if entry.Arrow != nil {
			item.menu.SetMenuEnabled(entry.Arrow(state))
		}
	}
}

func (a *App) toolbarMenuButton(name string) (*widget.MenuButton, bool) {
	btn, ok := a.named[name].(*widget.MenuButton)
	return btn, ok
}
