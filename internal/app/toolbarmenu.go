package app

import (
	"github.com/oops1/headless-gui/v3/widget"
)

const toolbarMenuArrowWidth = 16

type toolbarMenuEntry struct {
	Name   string
	Icon   string
	Action CommandID
	Items  func(*App) []widget.MenuItem
	Menu   func(State) bool
}

func toolbarMenuButtons() []toolbarMenuEntry {
	return []toolbarMenuEntry{
		{Name: "btnSaveStash", Icon: "stash_save", Action: CmdStashSave, Items: (*App).saveStashMenuItems},
		{Name: "btnApplyStash", Icon: "stash_apply", Action: CmdStashApply, Items: (*App).applyStashMenuItems, Menu: stashesListed},
		{Name: "btnGitFlow", Icon: flowToolbarIcon, Items: (*App).flowMenuItems},
	}
}

func stashesListed(state State) bool {
	return state.HasStashes
}

func (a *App) toolbarMenuButton(name string) (*widget.MenuButton, bool) {
	btn, ok := a.named[name].(*widget.MenuButton)
	return btn, ok
}

func (a *App) wireToolbarMenus() {
	for _, entry := range toolbarMenuButtons() {
		btn, ok := a.toolbarMenuButton(entry.Name)
		if !ok {
			continue
		}
		items := entry.Items
		btn.OnOpening = func() { btn.Items = items(a) }
		if entry.Action != "" {
			cmd := entry.Action
			btn.OnClick = func() { a.Dispatch(cmd) }
		}
	}
}

func (a *App) refreshToolbarMenus(state State) {
	for _, entry := range toolbarMenuButtons() {
		btn, ok := a.toolbarMenuButton(entry.Name)
		if !ok {
			continue
		}
		btn.SetEnabled(state.ActiveRepository != "")
		if entry.Action != "" {
			btn.SetActionEnabled(state.Enabled(entry.Action))
		}
		if entry.Menu != nil {
			btn.SetMenuEnabled(entry.Menu(state))
		}
	}
}

func (a *App) styleToolbarMenus(captions bool, width, height int) {
	for _, entry := range toolbarMenuButtons() {
		if btn, ok := a.toolbarMenuButton(entry.Name); ok {
			styleToolbarButton(btn.Button, entry.Icon, captions, width, height)
		}
	}
}

func (a *App) toolbarCaptionWidths() []int {
	menus := toolbarMenuButtons()
	widths := make([]int, 0, len(toolbarButtons)+len(menus))
	for _, name := range toolbarButtons {
		widths = append(widths, toolbarCaptionButtonWidth(a.named[name].(*widget.Button).Text))
	}
	for _, entry := range menus {
		if btn, ok := a.toolbarMenuButton(entry.Name); ok {
			widths = append(widths, toolbarCaptionButtonWidth(btn.Text)+toolbarMenuArrowWidth)
		}
	}
	return widths
}
