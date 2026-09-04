package branches

import (
	"strings"

	"github.com/oops1/headless-gui/v3/widget"
	"github.com/oops1/headless-gui/v3/widget/treeview"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/icons"
)

const (
	localGroupKey   = "local"
	remotesGroupKey = "remotes"
	tagsGroupKey    = "tags"

	remoteArrow   = " → "
	shortIDLength = 7

	treeIconSize = 16
)

type pathEntry struct {
	path    string
	ref     refs.Name
	label   string
	current bool
	icon    string
}

type View struct {
	tree       *widget.TreeViewWidget
	idByItem   map[*treeview.TreeViewItem]refs.Name
	itemByRef  map[refs.Name]*treeview.TreeViewItem
	keyByItem  map[*treeview.TreeViewItem]string
	expanded   map[string]bool
	OnSelect   func(ref refs.Name)
	OnActivate func(ref refs.Name)
}

func NewView() *View {
	return &View{
		idByItem:  map[*treeview.TreeViewItem]refs.Name{},
		itemByRef: map[refs.Name]*treeview.TreeViewItem{},
		keyByItem: map[*treeview.TreeViewItem]string{},
		expanded:  map[string]bool{},
	}
}

func (v *View) Bind(tree *widget.TreeViewWidget) {
	v.tree = tree
	tree.Tree.OnItemInvoked = func(e treeview.ItemInvokedEvent) {
		if ref, ok := v.idByItem[e.Item]; ok && v.OnActivate != nil {
			v.OnActivate(ref)
		}
	}
	tree.Tree.OnSelectedItemChanged = func(e treeview.SelectedItemChangedEvent) {
		if e.NewItem == nil {
			return
		}
		if ref, ok := v.idByItem[e.NewItem]; ok && v.OnSelect != nil {
			v.OnSelect(ref)
		}
	}
}

func (v *View) Item(ref refs.Name) (*treeview.TreeViewItem, bool) {
	item, ok := v.itemByRef[ref]
	return item, ok
}

func (v *View) Render(s Snapshot) {
	v.captureExpanded()

	v.tree.BeginUpdate()
	v.tree.ClearRoots()

	v.idByItem = map[*treeview.TreeViewItem]refs.Name{}
	v.itemByRef = map[refs.Name]*treeview.TreeViewItem{}
	v.keyByItem = map[*treeview.TreeViewItem]string{}

	v.tree.AddRoot(v.buildLocal(s))
	v.tree.AddRoot(v.buildRemotes(s))
	v.tree.AddRoot(v.buildTags(s))
	if s.HasStash {
		v.tree.AddRoot(v.buildStash())
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
			if key, ok := v.keyByItem[item]; ok {
				v.expanded[key] = item.Expanded
			}
			walk(item.Children)
		}
	}
	walk(v.tree.Tree.Roots())
}

func (v *View) expandedDefault(key string) bool {
	if exp, ok := v.expanded[key]; ok {
		return exp
	}
	return key != tagsGroupKey
}

func (v *View) newGroupItem(key, label string) *treeview.TreeViewItem {
	item := treeview.NewItem(label)
	item.Expanded = v.expandedDefault(key)
	item.Icon = icons.Tree("folder", treeIconSize)
	v.keyByItem[item] = key
	return item
}

func (v *View) track(item *treeview.TreeViewItem, ref refs.Name) {
	v.idByItem[item] = ref
	v.itemByRef[ref] = item
}

func (v *View) buildLocal(s Snapshot) *treeview.TreeViewItem {
	root := v.newGroupItem(localGroupKey, i18n.T("Pane.Branches.Local"))
	if s.Detached {
		leaf := treeview.NewItem(i18n.T("Pane.Branches.Detached") + " " + shortID(s.HeadID))
		leaf.Icon = icons.Tree("branch_current", treeIconSize)
		v.track(leaf, refs.HEAD)
		root.AddChild(leaf)
	}
	entries := make([]pathEntry, 0, len(s.Local))
	for _, b := range s.Local {
		short := b.Name.Short()
		current := !s.Detached && short == s.Current
		icon := "branch"
		if current {
			icon = "branch_current"
		}
		entries = append(entries, pathEntry{
			path:    short,
			ref:     b.Name,
			current: current,
			icon:    icon,
		})
	}
	v.buildPathTree(root, localGroupKey, entries)
	return root
}

func (v *View) buildRemotes(s Snapshot) *treeview.TreeViewItem {
	root := v.newGroupItem(remotesGroupKey, i18n.T("Pane.Branches.Remotes"))
	for _, remote := range s.Remotes {
		key := remotesGroupKey + "/" + remote.Name
		node := v.newGroupItem(key, remote.Name)
		root.AddChild(node)

		entries := make([]pathEntry, 0, len(remote.Branches))
		for _, b := range remote.Branches {
			relative := strings.TrimPrefix(b.Name.Short(), remote.Name+"/")
			label := ""
			if b.SymbolicTarget != "" {
				label = relative + remoteArrow + b.SymbolicTarget.Short()
			}
			entries = append(entries, pathEntry{path: relative, ref: b.Name, label: label, icon: "branch_remote"})
		}
		v.buildPathTree(node, key, entries)
	}
	return root
}

func (v *View) buildTags(s Snapshot) *treeview.TreeViewItem {
	root := v.newGroupItem(tagsGroupKey, i18n.T("Pane.Branches.Tags"))
	entries := make([]pathEntry, 0, len(s.Tags))
	for _, t := range s.Tags {
		entries = append(entries, pathEntry{path: t.Name.Short(), ref: t.Name, icon: "tag"})
	}
	v.buildPathTree(root, tagsGroupKey, entries)
	return root
}

func (v *View) buildStash() *treeview.TreeViewItem {
	item := treeview.NewItem(i18n.T("Pane.Branches.Stash"))
	item.Icon = icons.Tree("stash", treeIconSize)
	v.track(item, stashRefName)
	return item
}

func (v *View) buildPathTree(root *treeview.TreeViewItem, rootKey string, entries []pathEntry) {
	nodes := map[string]*treeview.TreeViewItem{rootKey: root}
	for _, e := range entries {
		segments := strings.Split(e.path, "/")
		key := rootKey
		parent := root
		for i, segment := range segments {
			key = key + "/" + segment
			if i == len(segments)-1 {
				parent.AddChild(v.leafItem(e, segment))
				continue
			}
			node, ok := nodes[key]
			if !ok {
				node = v.newGroupItem(key, segment)
				parent.AddChild(node)
				nodes[key] = node
			}
			parent = node
		}
	}
}

func (v *View) leafItem(e pathEntry, segment string) *treeview.TreeViewItem {
	label := e.label
	if label == "" {
		label = segment
	}
	item := treeview.NewItem(label)
	item.Icon = icons.Tree(e.icon, treeIconSize)
	v.track(item, e.ref)
	return item
}

func shortID(id hash.ObjectID) string {
	return id.String()[:shortIDLength]
}
