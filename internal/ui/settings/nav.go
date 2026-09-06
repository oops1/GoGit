package settings

import (
	"image/color"

	"github.com/oops1/headless-gui/v3/widget"
	"github.com/oops1/headless-gui/v3/widget/treeview"

	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/icons"
)

type sectionDescriptor struct {
	id       string
	navKey   string
	titleKey string
	icon     string
}

const defaultSectionID = "general"

var sectionOrder = []sectionDescriptor{
	{id: "general", navKey: "Dialog.Settings.Nav.General", titleKey: "Dialog.Settings.General.Title", icon: "settings_general"},
	{id: "git", navKey: "Dialog.Settings.Nav.Git", titleKey: "Dialog.Settings.Git.Title", icon: "settings_git"},
	{id: "credentials", navKey: "Dialog.Settings.Nav.Credentials", titleKey: "Dialog.Settings.Credentials.Title", icon: "settings_credentials"},
	{id: "ssh", navKey: "Dialog.Settings.Nav.SSH", titleKey: "Dialog.Settings.SSH.Title", icon: "settings_ssh"},
}

func sectionTitleKey(id string) string {
	for _, s := range sectionOrder {
		if s.id == id {
			return s.titleKey
		}
	}
	return ""
}

func isKnownSection(id string) bool {
	for _, s := range sectionOrder {
		if s.id == id {
			return true
		}
	}
	return false
}

type navBackdrop struct {
	*widget.Panel
}

func (b *navBackdrop) ApplyTheme(t *widget.Theme) {
	b.Panel.ApplyTheme(t)
	b.Background = t.PanelBG
}

type navTree struct {
	*widget.TreeViewWidget

	items []*treeview.TreeViewItem
}

func newNavTree() *navTree {
	n := &navTree{TreeViewWidget: widget.NewTreeViewWidget()}
	n.Tree.ItemHeight = navItemHeight
	n.Tree.IconSize = navIconSize
	for _, s := range sectionOrder {
		item := treeview.NewItem(i18n.T(s.navKey))
		item.Tag = s.id
		item.Icon = icons.ToolbarPlain(s.icon, navIconSize)
		n.items = append(n.items, item)
		n.AddRoot(item)
	}
	return n
}

func (n *navTree) ApplyTheme(t *widget.Theme) {
	n.TreeViewWidget.ApplyTheme(t)
	accent := t.Accent
	n.Tree.Theme.SelectColor = premultiplyAlpha(accent, navSelectionAlpha)
	n.Tree.Theme.HoverColor = premultiplyAlpha(accent, navHoverAlpha)
	n.Tree.ItemStyle = func(item *treeview.TreeViewItem) (color.RGBA, bool, bool) {
		if item.IsSelected {
			return accent, false, true
		}
		return color.RGBA{}, false, false
	}
}

func (n *navTree) SetSelected(id string) {
	for _, item := range n.items {
		if item.Tag == id {
			n.Tree.SetSelectedItem(item)
			return
		}
	}
}

func (n *navTree) SetCaption(index int, text string) {
	if index < 0 || index >= len(n.items) {
		return
	}
	n.items[index].Header = text
}

func (n *navTree) Caption(index int) string {
	if index < 0 || index >= len(n.items) {
		return ""
	}
	return n.items[index].Header
}

func premultiplyAlpha(c color.RGBA, alpha uint8) color.RGBA {
	return color.RGBA{
		R: uint8(uint32(c.R) * uint32(alpha) / 255),
		G: uint8(uint32(c.G) * uint32(alpha) / 255),
		B: uint8(uint32(c.B) * uint32(alpha) / 255),
		A: alpha,
	}
}
