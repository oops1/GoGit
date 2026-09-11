package app

import (
	"image"
	"image/color"

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
	CmdCompareFiles:    "compare",
	CmdMerge:           "merge",
	CmdAbortMerge:      "merge_abort",
	CmdRebase:          "rebase",
	CmdContinue:        "continue",
	CmdSkip:            "skip",
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
	"Menu.Context.Open":             "open",
	"Menu.Context.Reveal":           "reveal",
	"Menu.Context.Terminal":         "terminal",
	"Menu.Context.CopyPath":         "copy",
	"Menu.Context.CopyHash":         "copy",
	"Menu.Context.CopyMessage":      "copy",
	"Menu.Context.MergeIntoCurrent": "merge",
	"Menu.Context.TakeOurs":         "take_ours",
	"Menu.Context.TakeTheirs":       "take_theirs",
	"Menu.Context.MarkResolved":     "resolved",
	"Menu.Context.CherryPick":       "cherry_pick",
	"Menu.Context.Revert":           "revert",
	"Menu.Context.Reset":            "reset",
}

func commandIcon(id CommandID) image.Image {
	return menuIcon(commandIcons[id])
}

func commandIconFor(id CommandID, enabled bool) image.Image {
	if enabled {
		return commandIcon(id)
	}
	return mutedIcon(commandIcons[id])
}

func menuKeyIcon(key string) image.Image {
	if name, ok := contextIcons[key]; ok {
		return menuIcon(name)
	}
	if name, ok := menuGroupIcons[key]; ok {
		return menuIcon(name)
	}
	if id, ok := commandOfKey(key); ok {
		return commandIcon(id)
	}
	return nil
}

func commandOfKey(key string) (CommandID, bool) {
	for _, def := range menuBarDefs {
		for _, entry := range def.Tree {
			if entry.Leaf != nil && entry.Leaf.Key == key {
				return entry.Leaf.Command, true
			}
		}
	}
	return "", false
}

func (a *App) applyMenuIcons() {
	state := a.State()
	items := a.menu.Items()
	for i, def := range menuBarDefs {
		if i >= len(items) {
			continue
		}
		applyTreeIcons(items[i].Items, def.Tree, state)
	}
}

func applyTreeIcons(subs []widget.MenuItem, tree []menuTreeEntry, state State) {
	for i, entry := range tree {
		if i >= len(subs) {
			continue
		}
		switch {
		case entry.Leaf != nil:
			subs[i].Icon = commandIconFor(entry.Leaf.Command, state.Enabled(entry.Leaf.Command))
		case entry.Group != nil:
			subs[i].Icon = menuKeyIcon(entry.Group.Key)
		}
	}
}

func mutedIcon(name string) image.Image {
	return tintedIcon(name, widget.CurrentTheme().Disabled)
}

func menuIcon(name string) image.Image {
	return tintedIcon(name, widget.CurrentTheme().LabelText)
}

func tintedIcon(name string, tint color.RGBA) image.Image {
	if name == "" {
		return nil
	}
	if drawn := icons.Menu(name, menuIconSize, tint); drawn != nil {
		return drawn
	}
	return icons.Toolbar(name, menuIconSize, tint)
}
