package app

import (
	"image"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/ui/icons"
)

const menuIconSize = 16

var commandIcons = map[CommandID]string{
	CmdAddOrCreate:     "repo_add",
	CmdAddGroup:        "group_add",
	CmdClone:           "clone",
	CmdSearch:          "search",
	CmdCloseRepository: "close_repo",
	CmdAddWorktree:     "worktree_add",
	CmdRemoveWorktree:  "worktree_remove",
	CmdPruneWorktrees:  "worktree_prune",
	CmdRepoSettings:    "repo_settings",
	CmdSettings:        "settings",
	CmdClose:           "exit",
	CmdStage:           "stage",
	CmdUnstage:         "unstage",
	CmdDiscard:         "discard",
	CmdCommit:          "commit",
	CmdFetch:           "fetch",
	CmdPull:            "pull",
	CmdPush:            "push",
	CmdSync:            "sync",
	CmdPrune:           "prune_remote",
	CmdManageRemotes:   "remotes",
	CmdResetLayout:     "layout",
	CmdRefresh:         "refresh",
	CmdCheckUpdates:    "update",
	CmdAbout:           "about",
}

var menuGroupIcons = map[string]string{
	"Menu.View.Panes":    "panes",
	"Menu.View.Theme":    "theme",
	"Menu.View.Language": "language",
}

var contextIcons = map[string]string{
	"Menu.Context.Open":        "open",
	"Menu.Context.Reveal":      "reveal",
	"Menu.Context.Terminal":    "terminal",
	"Menu.Context.CopyPath":    "copy",
	"Menu.Context.CopyHash":    "copy",
	"Menu.Context.CopyMessage": "copy",
}

func commandIcon(id CommandID) image.Image {
	return menuIcon(commandIcons[id])
}

func menuKeyIcon(key string) image.Image {
	if name, ok := contextIcons[key]; ok {
		return menuIcon(name)
	}
	return menuIcon(menuGroupIcons[key])
}

func menuIcon(name string) image.Image {
	if name == "" {
		return nil
	}
	tint := widget.CurrentTheme().LabelText
	if drawn := icons.Menu(name, menuIconSize, tint); drawn != nil {
		return drawn
	}
	return icons.Toolbar(name, menuIconSize, tint)
}
