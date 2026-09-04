package repos

import (
	"image"
	"image/color"
	"sync"

	"github.com/oops1/headless-gui/v3/widget"
	"github.com/oops1/headless-gui/v3/widget/treeview"

	"github.com/oops1/gogit/internal/repo"
	"github.com/oops1/gogit/internal/ui/icons"
)

const treeIconSize = 16

type State struct {
	Modified bool
	Missing  bool
}

type View struct {
	tree       *widget.TreeViewWidget
	mu         sync.Mutex
	idByItem   map[*treeview.TreeViewItem]string
	itemByID   map[string]*treeview.TreeViewItem
	expanded   map[string]bool
	accent     color.RGBA
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
		v.mu.Lock()
		id, ok := v.idByItem[e.Item]
		v.mu.Unlock()
		if ok && v.OnActivate != nil {
			v.OnActivate(id)
		}
	}
	tree.Tree.OnSelectedItemChanged = func(e treeview.SelectedItemChangedEvent) {
		if e.NewItem == nil {
			return
		}
		v.mu.Lock()
		id, ok := v.idByItem[e.NewItem]
		v.mu.Unlock()
		if ok && v.OnSelect != nil {
			v.OnSelect(id)
		}
	}
}

func (v *View) Item(id string) (*treeview.TreeViewItem, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	item, ok := v.itemByID[id]
	return item, ok
}

func (v *View) SetAccent(c color.RGBA) {
	v.mu.Lock()
	v.accent = c
	v.mu.Unlock()
}

func (v *View) Render(reg *repo.Registry, state map[string]State) {
	v.mu.Lock()
	defer v.mu.Unlock()

	v.captureExpandedLocked()

	v.tree.BeginUpdate()
	v.tree.ClearRoots()

	v.idByItem = map[*treeview.TreeViewItem]string{}
	v.itemByID = map[string]*treeview.TreeViewItem{}

	activeID := ""
	if n, ok := reg.Active(); ok {
		activeID = n.ID
	}

	for _, item := range v.buildItemsLocked(reg.Roots(), state, activeID) {
		v.tree.AddRoot(item)
	}
	v.tree.EndUpdate()
}

func (v *View) captureExpandedLocked() {
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

func (v *View) buildItemsLocked(nodes []*repo.Node, state map[string]State, activeID string) []*treeview.TreeViewItem {
	items := make([]*treeview.TreeViewItem, 0, len(nodes))
	for _, n := range nodes {
		item := treeview.NewItem(n.Name)
		v.idByItem[item] = n.ID
		v.itemByID[n.ID] = item
		if n.Kind == repo.KindGroup {
			item.Expanded = v.expandedDefaultLocked(n.ID)
		}
		item.Icon = v.iconForLocked(n, item.Expanded, state[n.ID], n.ID == activeID)
		for _, child := range v.buildItemsLocked(n.Children, state, activeID) {
			item.AddChild(child)
		}
		items = append(items, item)
	}
	return items
}

func (v *View) expandedDefaultLocked(id string) bool {
	exp, ok := v.expanded[id]
	if !ok {
		return true
	}
	return exp
}

func (v *View) iconForLocked(n *repo.Node, expanded bool, st State, active bool) image.Image {
	switch n.Kind {
	case repo.KindGroup:
		if expanded {
			return icons.Tree("group_open", treeIconSize)
		}
		return icons.Tree("group", treeIconSize)
	case repo.KindWorktree:
		return v.iconLocked("worktree", active)
	default:
		name := "repository"
		switch {
		case st.Missing:
			name = "repository_missing"
		case st.Modified:
			name = "repository_modified"
		}
		return v.iconLocked(name, active)
	}
}

func (v *View) iconLocked(name string, active bool) image.Image {
	if active {
		return icons.TreeTinted(name, treeIconSize, v.accent)
	}
	return icons.Tree(name, treeIconSize)
}
