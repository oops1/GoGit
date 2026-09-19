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
	stale, item := v.firstObsolete()
	if item != nil {
		v.tree.Tree.SetSelectedItem(item)
	}
	return stale
}

func (v *View) firstObsolete() ([]refs.Name, *treeview.TreeViewItem) {
	v.mu.Lock()
	defer v.mu.Unlock()
	stale := ObsoleteLocal(v.last)
	if v.tree == nil || len(stale) == 0 {
		return stale, nil
	}
	return stale, v.itemByRef[stale[0]]
}
