package settings

import (
	"github.com/oops1/headless-gui/v3/widget"

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

type navPanel struct {
	*widget.NavPanel

	items []widget.NavPanelItem
}

func newNavPanel() *navPanel {
	n := &navPanel{NavPanel: widget.NewNavPanel()}
	n.ExpandedWidth = navWidth
	n.ItemHeight = navItemHeight
	n.IconSize = navIconSize
	for _, s := range sectionOrder {
		n.items = append(n.items, widget.NavPanelItem{
			Icon: icons.ToolbarPlain(s.icon, navIconSize),
			Text: i18n.T(s.navKey),
			Tag:  s.id,
		})
	}
	n.SetItems(n.items)
	return n
}

func (n *navPanel) SetSelectedSection(id string) {
	for i, s := range sectionOrder {
		if s.id == id {
			n.SetSelected(i)
			return
		}
	}
}

func (n *navPanel) SetCaption(index int, text string) {
	if index < 0 || index >= len(n.items) {
		return
	}
	n.items[index].Text = text
	n.SetItems(n.items)
}

func (n *navPanel) Caption(index int) string {
	if index < 0 || index >= len(n.items) {
		return ""
	}
	return n.items[index].Text
}
