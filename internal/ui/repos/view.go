package repos

import (
	"image"
	"image/color"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/oops1/headless-gui/v3/widget"
	"github.com/oops1/headless-gui/v3/widget/treeview"

	"github.com/oops1/gogit/internal/repo"
	"github.com/oops1/gogit/internal/ui/icons"
)

const treeIconSize = 16

type State struct {
	Modified  bool
	Missing   bool
	Branch    string
	MutedDirs []string
}

type dirEntry struct {
	repoID string
	root   string
	rel    string
}

type stubMarker struct{}

var source = readDirectories

func readDirectories(path string) ([]string, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() || e.Name() == ".git" {
			continue
		}
		names = append(names, e.Name())
	}
	slices.SortFunc(names, func(a, b string) int {
		return strings.Compare(strings.ToLower(a), strings.ToLower(b))
	})
	return names, nil
}

func hasSubdir(path string) bool {
	names, err := source(path)
	return err == nil && len(names) > 0
}

func dirKey(repoID, rel string) string {
	return repoID + "\x00" + rel
}

func newStub() *treeview.TreeViewItem {
	item := treeview.NewItem("")
	item.Tag = stubMarker{}
	return item
}

func isStubOnly(item *treeview.TreeViewItem) bool {
	if len(item.Children) != 1 {
		return false
	}
	_, ok := item.Children[0].Tag.(stubMarker)
	return ok
}

type View struct {
	tree              *widget.TreeViewWidget
	mu                sync.Mutex
	idByItem          map[*treeview.TreeViewItem]string
	itemByID          map[string]*treeview.TreeViewItem
	expanded          map[string]bool
	accent            color.RGBA
	muted             color.RGBA
	mutedDirs         []string
	OnActivate        func(id string)
	OnSelect          func(id string)
	OnSelectDirectory func(repoID, relPath string)
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
		var entry dirEntry
		dirOK := false
		if !ok {
			entry, dirOK = e.NewItem.Tag.(dirEntry)
		}
		v.mu.Unlock()
		if ok {
			if v.OnSelect != nil {
				v.OnSelect(id)
			}
			return
		}
		if dirOK && v.OnSelectDirectory != nil {
			v.OnSelectDirectory(entry.repoID, filepath.ToSlash(entry.rel))
		}
	}
	tree.Tree.OnExpanded = func(e treeview.ExpandedEvent) {
		v.handleExpanded(e.Item)
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

func (v *View) SetMuted(c color.RGBA) {
	v.mu.Lock()
	v.muted = c
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
	v.mutedDirs = state[activeID].MutedDirs

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
			} else if entry, ok := item.Tag.(dirEntry); ok {
				v.expanded[dirKey(entry.repoID, entry.rel)] = item.Expanded
			}
			walk(item.Children)
		}
	}
	walk(v.tree.Tree.Roots())
}

func (v *View) buildItemsLocked(nodes []*repo.Node, state map[string]State, activeID string) []*treeview.TreeViewItem {
	items := make([]*treeview.TreeViewItem, 0, len(nodes))
	for _, n := range nodes {
		st := state[n.ID]
		item := treeview.NewItem(displayName(n, st.Branch))
		v.idByItem[item] = n.ID
		v.itemByID[n.ID] = item
		switch n.Kind {
		case repo.KindGroup:
			item.Expanded = v.expandedDefaultLocked(n.ID)
		case repo.KindRepository, repo.KindWorktree:
			item.Expanded = v.expanded[n.ID]
			item.Tag = dirEntry{repoID: n.ID, root: n.Path, rel: ""}
		}
		item.Icon = v.iconForLocked(n, item.Expanded, st, n.ID == activeID)
		for _, child := range v.buildItemsLocked(n.Children, state, activeID) {
			item.AddChild(child)
		}
		if n.ID == activeID && (n.Kind == repo.KindRepository || n.Kind == repo.KindWorktree) {
			v.attachDirChildrenLocked(item, n.ID, n.Path, "")
		}
		items = append(items, item)
	}
	return items
}

func displayName(n *repo.Node, branch string) string {
	if branch == "" || n.Kind == repo.KindGroup {
		return n.Name
	}
	return n.Name + " (" + branch + ")"
}

func (v *View) attachDirChildrenLocked(item *treeview.TreeViewItem, repoID, root, rel string) {
	if item.Expanded {
		v.appendDirChildrenLocked(item, repoID, root, rel)
		return
	}
	if hasSubdir(filepath.Join(root, rel)) {
		item.AddChild(newStub())
	}
}

func (v *View) appendDirChildrenLocked(item *treeview.TreeViewItem, repoID, root, rel string) {
	names, err := source(filepath.Join(root, rel))
	if err != nil {
		return
	}
	for _, name := range names {
		item.AddChild(v.buildDirItemLocked(repoID, root, filepath.Join(rel, name)))
	}
}

func (v *View) buildDirItemLocked(repoID, root, rel string) *treeview.TreeViewItem {
	item := treeview.NewItem(filepath.Base(rel))
	item.Tag = dirEntry{repoID: repoID, root: root, rel: rel}
	item.Icon = icons.Tree(directoryIconLocked(v.mutedDirs, rel), treeIconSize)
	item.Expanded = v.expanded[dirKey(repoID, rel)]
	v.attachDirChildrenLocked(item, repoID, root, rel)
	return item
}

func (v *View) handleExpanded(item *treeview.TreeViewItem) {
	entry, ok := item.Tag.(dirEntry)
	if !ok {
		return
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if !isStubOnly(item) {
		return
	}
	item.ClearChildren()
	v.appendDirChildrenLocked(item, entry.repoID, entry.root, entry.rel)
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
	if v.muted.A > 0 {
		return icons.TreeTinted(name, treeIconSize, v.muted)
	}
	return icons.Tree(name, treeIconSize)
}

func directoryIconLocked(muted []string, rel string) string {
	switch {
	case isMutedDir(muted, rel):
		return "directory_muted"
	case isDotDir(rel):
		return "directory_dim"
	default:
		return "directory"
	}
}

func isDotDir(rel string) bool {
	return strings.HasPrefix(filepath.Base(rel), ".")
}

func isMutedDir(muted []string, rel string) bool {
	path := filepath.ToSlash(rel)
	for _, prefix := range muted {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return false
}
