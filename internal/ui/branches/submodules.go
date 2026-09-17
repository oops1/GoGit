package branches

import (
	"github.com/oops1/headless-gui/v3/widget/treeview"

	"github.com/oops1/gogit/internal/gitcore/ops"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/icons"
)

const submodulesGroupKey = "submodules"

var submoduleStateKeys = map[ops.SubmoduleState]string{
	ops.SubmoduleStateNotInitialized: "Pane.Branches.Submodule.NotInitialized",
	ops.SubmoduleStateUpToDate:       "Pane.Branches.Submodule.UpToDate",
	ops.SubmoduleStateModified:       "Pane.Branches.Submodule.Modified",
	ops.SubmoduleStateNewCommits:     "Pane.Branches.Submodule.NewCommits",
	ops.SubmoduleStateConflict:       "Pane.Branches.Submodule.Conflict",
}

var submoduleStateIcons = map[ops.SubmoduleState]string{
	ops.SubmoduleStateNotInitialized: "repository_missing",
	ops.SubmoduleStateUpToDate:       "repository",
	ops.SubmoduleStateModified:       "repository_modified",
	ops.SubmoduleStateNewCommits:     "repository_ahead",
	ops.SubmoduleStateConflict:       "repository_modified",
}

func SubmoduleStateText(state ops.SubmoduleState) string {
	return i18n.T(submoduleStateKeys[state])
}

func (v *View) SetSubmodules(list []ops.Submodule) {
	v.submodules = list
	if v.tree != nil {
		v.Render(v.last)
	}
}

func (v *View) Submodules() []ops.Submodule {
	return v.submodules
}

func (v *View) SubmoduleItem(path string) (*treeview.TreeViewItem, bool) {
	for item, sub := range v.submoduleByItem {
		if sub.Path == path {
			return item, true
		}
	}
	return nil, false
}

func (v *View) buildSubmodules() *treeview.TreeViewItem {
	root := v.newGroupItem(submodulesGroupKey, i18n.T("Pane.Branches.Submodules"))
	root.Icon = icons.Tree("repository", treeIconSize)
	for _, sub := range v.submodules {
		item := treeview.NewItem(sub.Path + " (" + SubmoduleStateText(sub.State) + ")")
		item.Icon = icons.Tree(submoduleStateIcons[sub.State], treeIconSize)
		v.submoduleByItem[item] = sub
		root.AddChild(item)
	}
	return root
}
