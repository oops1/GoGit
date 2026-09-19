package branches

import (
	"strings"
	"sync"
	"time"

	"github.com/oops1/headless-gui/v3/widget"
	"github.com/oops1/headless-gui/v3/widget/treeview"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/ops"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/icons"
)

const (
	localGroupKey   = "local"
	remotesGroupKey = "remotes"
	tagsGroupKey    = "tags"
	stashGroupKey   = "stash"

	shortIDLength = 7

	treeIconSize = 16
)

type pathEntry struct {
	path    string
	ref     refs.Name
	label   string
	current bool
	icon    string
	when    time.Time
}

type View struct {
	tree       *widget.TreeViewWidget
	idByItem   map[*treeview.TreeViewItem]refs.Name
	itemByRef  map[refs.Name]*treeview.TreeViewItem
	keyByItem  map[*treeview.TreeViewItem]string
	expanded   map[string]bool
	OnSelect   func(ref refs.Name)
	OnActivate func(ref refs.Name)
	OnMenu     func(ref refs.Name) []widget.MenuItem

	mu              sync.Mutex
	last            Snapshot
	options         Options
	flow            ops.FlowConfig
	flowConfigured  bool
	submodules      []ops.Submodule
	submoduleByItem map[*treeview.TreeViewItem]ops.Submodule

	OnSubmoduleActivate func(ops.Submodule)
	OnSubmoduleMenu     func(ops.Submodule) []widget.MenuItem
}

func NewView() *View {
	return &View{
		idByItem:        map[*treeview.TreeViewItem]refs.Name{},
		itemByRef:       map[refs.Name]*treeview.TreeViewItem{},
		keyByItem:       map[*treeview.TreeViewItem]string{},
		expanded:        map[string]bool{},
		submoduleByItem: map[*treeview.TreeViewItem]ops.Submodule{},
		options:         DefaultOptions(),
	}
}

func (v *View) Bind(tree *widget.TreeViewWidget) {
	v.tree = tree
	tree.Tree.SelectionMode = treeview.SelectionExtended
	tree.Tree.OnItemInvoked = func(e treeview.ItemInvokedEvent) {
		ref, isRef, sub, isSub := v.lookup(e.Item)
		if isRef && v.OnActivate != nil {
			v.OnActivate(ref)
		}
		if isSub && v.OnSubmoduleActivate != nil {
			v.OnSubmoduleActivate(sub)
		}
	}
	tree.NodeContextMenu = v.nodeMenu
	tree.Tree.OnSelectedItemChanged = func(e treeview.SelectedItemChangedEvent) {
		if e.NewItem == nil {
			return
		}
		if ref, ok, _, _ := v.lookup(e.NewItem); ok && v.OnSelect != nil {
			v.OnSelect(ref)
		}
	}
}

func (v *View) lookup(item *treeview.TreeViewItem) (refs.Name, bool, ops.Submodule, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	ref, isRef := v.idByItem[item]
	sub, isSub := v.submoduleByItem[item]
	return ref, isRef, sub, isSub
}

func (v *View) nodeMenu(item *treeview.TreeViewItem) []widget.MenuItem {
	ref, isRef, sub, isSub := v.lookup(item)
	if isSub && v.OnSubmoduleMenu != nil {
		return v.OnSubmoduleMenu(sub)
	}
	if !isRef || v.OnMenu == nil {
		return nil
	}
	return v.OnMenu(ref)
}

func (v *View) Item(ref refs.Name) (*treeview.TreeViewItem, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	item, ok := v.itemByRef[ref]
	return item, ok
}

func (v *View) Render(s Snapshot) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.render(s)
}

func (v *View) render(s Snapshot) {
	v.last = s
	v.captureExpanded()
	selectedRefs, currentRef := v.captureSelection()
	scroll := v.tree.Tree.ScrollY()
	selectedSubmodule, submoduleSelected := v.submoduleByItem[v.tree.Tree.SelectedItem()]

	v.tree.BeginUpdate()
	v.tree.ClearRoots()

	v.idByItem = map[*treeview.TreeViewItem]refs.Name{}
	v.itemByRef = map[refs.Name]*treeview.TreeViewItem{}
	v.keyByItem = map[*treeview.TreeViewItem]string{}
	v.submoduleByItem = map[*treeview.TreeViewItem]ops.Submodule{}

	if base := v.buildFlowBase(s); base != nil {
		v.tree.AddRoot(base)
	}
	for _, section := range v.buildFlowSections(s) {
		v.tree.AddRoot(section)
	}
	v.tree.AddRoot(v.buildLocal(s))
	v.tree.AddRoot(v.buildRemotes(s))
	v.tree.AddRoot(v.buildTags(s))
	if len(s.Stashes) > 0 {
		v.tree.AddRoot(v.buildStash(s))
	}
	if len(v.submodules) > 0 {
		v.tree.AddRoot(v.buildSubmodules())
	}

	v.tree.EndUpdate()
	v.restoreSelection(selectedRefs, currentRef)
	for item, sub := range v.submoduleByItem {
		if submoduleSelected && sub.Path == selectedSubmodule.Path {
			selectQuietly(v.tree.Tree, item)
		}
	}
	v.tree.ScrollBy(scroll)
}

func (v *View) captureSelection() ([]refs.Name, refs.Name) {
	items := v.tree.Tree.SelectedItems()
	selected := make([]refs.Name, 0, len(items))
	for _, item := range items {
		if ref, ok := v.idByItem[item]; ok {
			selected = append(selected, ref)
		}
	}
	current := v.idByItem[v.tree.Tree.SelectedItem()]
	return selected, current
}

func (v *View) restoreSelection(selected []refs.Name, current refs.Name) {
	items := make([]*treeview.TreeViewItem, 0, len(selected))
	currentIndex := -1
	for _, ref := range selected {
		item, ok := v.itemByRef[ref]
		if !ok {
			continue
		}
		if ref == current {
			currentIndex = len(items)
		}
		items = append(items, item)
	}
	if len(items) == 0 {
		return
	}
	if last := len(items) - 1; currentIndex >= 0 && currentIndex != last {
		items[currentIndex], items[last] = items[last], items[currentIndex]
	}
	selectItemsQuietly(v.tree.Tree, items)
}

func (v *View) ClearStashSelection() {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.tree == nil {
		return
	}
	if _, ok := StashIndex(v.idByItem[v.tree.Tree.SelectedItem()]); ok {
		selectQuietly(v.tree.Tree, nil)
	}
}

func selectQuietly(tree *treeview.TreeView, item *treeview.TreeViewItem) {
	handler := tree.OnSelectedItemChanged
	tree.OnSelectedItemChanged = nil
	tree.SetSelectedItem(item)
	tree.OnSelectedItemChanged = handler
}

func selectItemsQuietly(tree *treeview.TreeView, items []*treeview.TreeViewItem) {
	handler := tree.OnSelectedItemChanged
	tree.OnSelectedItemChanged = nil
	tree.SetSelectedItems(items)
	tree.OnSelectedItemChanged = handler
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
		if _, inFlow := v.flowKindOf(short); inFlow {
			continue
		}
		if short == v.flowBaseBranch() {
			continue
		}
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
			when:    b.When,
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
			if isRemoteHead(b.Name) {
				continue
			}
			relative := strings.TrimPrefix(b.Name.Short(), remote.Name+"/")
			if v.flowHoldsRemote(relative) {
				continue
			}
			icon := "branch_remote"
			if remote.Head != "" && b.Name == remote.Head {
				icon = "branch_head"
			}
			entries = append(entries, pathEntry{path: relative, ref: b.Name, icon: icon, when: b.When})
		}
		v.buildPathTree(node, key, entries)
	}
	return root
}

func (v *View) buildTags(s Snapshot) *treeview.TreeViewItem {
	root := v.newGroupItem(tagsGroupKey, i18n.T("Pane.Branches.Tags"))
	byName := make(map[string]refs.Name, len(s.Tags))
	names := make([]string, 0, len(s.Tags))
	nested := make([]pathEntry, 0, len(s.Tags))
	for _, t := range s.Tags {
		short := t.Name.Short()
		byName[short] = t.Name
		if strings.Contains(short, "/") {
			nested = append(nested, pathEntry{path: short, ref: t.Name, icon: "tag", when: t.When})
			continue
		}
		names = append(names, short)
	}
	v.buildPathTree(root, tagsGroupKey, nested)
	recent, nodes := GroupTags(names)
	for _, name := range recent {
		root.AddChild(v.leafItem(pathEntry{path: name, ref: byName[name], icon: "tag"}, name))
	}
	v.addTagNodes(root, tagsGroupKey, nodes, byName)
	return root
}

func (v *View) addTagNodes(parent *treeview.TreeViewItem, key string, nodes []TagNode, byName map[string]refs.Name) {
	for _, n := range nodes {
		if n.IsTag() {
			parent.AddChild(v.leafItem(pathEntry{path: n.Tag, ref: byName[n.Tag], icon: "tag"}, n.Label))
			continue
		}
		group := v.newGroupItem(key+"/"+n.Label, n.Label)
		parent.AddChild(group)
		v.addTagNodes(group, key+"/"+n.Label, n.Children, byName)
	}
}

func (v *View) buildStash(s Snapshot) *treeview.TreeViewItem {
	root := v.newGroupItem(stashGroupKey, i18n.T("Pane.Branches.Stash"))
	root.Icon = icons.Tree("stash", treeIconSize)
	for _, entry := range s.Stashes {
		ref := StashRef(entry.Index)
		root.AddChild(v.leafItem(pathEntry{ref: ref, icon: "stash", label: ref.String() + ": " + entry.Message}, ""))
	}
	return root
}

func (v *View) buildPathTree(root *treeview.TreeViewItem, rootKey string, entries []pathEntry) {
	sortRefEntries(entries, v.options.Sort)
	v.addRefNodes(root, rootKey, groupRefEntries(entries, v.options.Grouping))
}

func (v *View) addRefNodes(parent *treeview.TreeViewItem, key string, nodes []refNode) {
	for _, node := range nodes {
		if node.leaf {
			parent.AddChild(v.leafItem(node.entry, node.label))
			continue
		}
		childKey := key + "/" + node.label
		group := v.newGroupItem(childKey, node.label)
		parent.AddChild(group)
		v.addRefNodes(group, childKey, node.children)
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
