package branches

import (
	"github.com/oops1/headless-gui/v3/widget/treeview"

	"github.com/oops1/gogit/internal/gitcore/refs"
)

func ObsoleteLocal(s Snapshot) []refs.Name {
	tracking := map[refs.Name]bool{}
	for _, remote := range s.Remotes {
		for _, b := range remote.Branches {
			tracking[b.Name] = true
		}
	}
	var stale []refs.Name
	for _, b := range s.Local {
		if b.Upstream == "" || tracking[b.Upstream] {
			continue
		}
		if !s.Detached && b.Name.Short() == s.Current {
			continue
		}
		stale = append(stale, b.Name)
	}
	return stale
}

func (v *View) SelectObsolete() []refs.Name {
	stale, items := v.obsoleteItems()
	if len(items) > 0 {
		v.tree.Tree.SetSelectedItems(items)
	}
	return stale
}

func (v *View) obsoleteItems() ([]refs.Name, []*treeview.TreeViewItem) {
	v.mu.Lock()
	defer v.mu.Unlock()
	stale := ObsoleteLocal(v.last)
	if v.tree == nil || len(stale) == 0 {
		return stale, nil
	}
	items := make([]*treeview.TreeViewItem, 0, len(stale))
	for _, ref := range stale {
		if item, ok := v.itemByRef[ref]; ok {
			items = append(items, item)
		}
	}
	return stale, items
}
