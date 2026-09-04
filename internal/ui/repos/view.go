package repos

import (
	"image"

	"github.com/oops1/headless-gui/v3/widget"
	"github.com/oops1/headless-gui/v3/widget/treeview"

	"github.com/oops1/gogit/internal/repo"
	"github.com/oops1/gogit/internal/ui/icons"
)

const treeIconSize = 16

type View struct {
	tree       *widget.TreeViewWidget
	idByItem   map[*treeview.TreeViewItem]string
	itemByID   map[string]*treeview.TreeViewItem
	expanded   map[string]bool
	Modified   map[string]bool
	OnActivate func(id string)
	OnSelect   func(id string)
}

func NewView() *View {
	return &View{
		idByItem: map[*treeview.TreeViewItem]string{},
		itemByID: map[string]*treeview.TreeViewItem{},
		expanded: map[string]bool{},
	}
}

func (v *View) Bind(tree *widget.TreeViewWidget) {
	v.tree = tree
	tree.Tree.OnItemInvoked = func(e treeview.ItemInvokedEvent) {
		id, ok := v.idByItem[e.Item]
		if ok && v.OnActivate != nil {
			v.OnActivate(id)
		}
	}
	tree.Tree.OnSelectedItemChanged = func(e treeview.SelectedItemChangedEvent) {
		if e.NewItem == nil {
			return
		}
		id, ok := v.idByItem[e.NewItem]
		if ok && v.OnSelect != nil {
			v.OnSelect(id)
		}
	}
}

func (v *View) Item(id string) (*treeview.TreeViewItem, bool) {
	item, ok := v.itemByID[id]
	return item, ok
}

func (v *View) Render(reg *repo.Registry) {
	v.captureExpanded()

	v.tree.BeginUpdate()
	v.tree.ClearRoots()

	v.idByItem = map[*treeview.TreeViewItem]string{}
	v.itemByID = map[string]*treeview.TreeViewItem{}

	for _, item := range v.buildItems(reg.Roots()) {
		v.tree.AddRoot(item)
	}
	v.tree.EndUpdate()
}

func (v *View) captureExpanded() {
	if v.tree == nil {
		return
	}
	var walk func(items []*treeview.TreeViewItem)
	walk = func(items []*treeview.TreeViewItem) {
		for _, item := range items {
			if id, ok := v.idByItem[item]; ok {
				v.expanded[id] = item.Expanded
			}
			walk(item.Children)
		}
	}
	walk(v.tree.Tree.Roots())
}

func (v *View) buildItems(nodes []*repo.Node) []*treeview.TreeViewItem {
	items := make([]*treeview.TreeViewItem, 0, len(nodes))
	for _, n := range nodes {
		item := treeview.NewItem(n.Name)
		v.idByItem[item] = n.ID
		v.itemByID[n.ID] = item
		if n.Kind == repo.KindGroup {
			item.Expanded = v.expandedDefault(n.ID)
		}
		item.Icon = v.iconFor(n, item.Expanded)
		for _, child := range v.buildItems(n.Children) {
			item.AddChild(child)
		}
		items = append(items, item)
	}
	return items
}

func (v *View) expandedDefault(id string) bool {
	exp, ok := v.expanded[id]
	if !ok {
		return true
	}
	return exp
}

func (v *View) iconFor(n *repo.Node, expanded bool) image.Image {
	switch n.Kind {
	case repo.KindGroup:
		if expanded {
			return icons.Tree("group_open", treeIconSize)
		}
		return icons.Tree("group", treeIconSize)
	case repo.KindWorktree:
		return icons.Tree("worktree", treeIconSize)
	default:
		if v.Modified[n.ID] {
			return icons.Tree("repository_modified", treeIconSize)
		}
		return icons.Tree("repository", treeIconSize)
	}
}
