package branches

import (
	"strings"

	"github.com/oops1/headless-gui/v4/widget/treeview"

	"github.com/oops1/gogit/internal/gitcore/ops"
	"github.com/oops1/gogit/internal/i18n"
)

const (
	mainGroupKey   = "main"
	mainBranchIcon = "branch_main"
)

var conventionalMainBranches = []string{"main", "master", "develop"}

func MainBranchNames(s Snapshot, flow ops.FlowConfig, flowConfigured bool) map[string]bool {
	names := make(map[string]bool, len(conventionalMainBranches)+2)
	for _, name := range conventionalMainBranches {
		names[name] = true
	}
	if flowConfigured {
		for _, name := range []string{flow.Develop, flow.Master} {
			if name != "" {
				names[name] = true
			}
		}
	}
	for _, remote := range s.Remotes {
		if remote.Head != "" {
			names[strings.TrimPrefix(remote.Head.Short(), remote.Name+"/")] = true
		}
	}
	return names
}

func (v *View) inMainSection(short string) bool {
	return v.options.MainSection && v.mainNames[short]
}

func (v *View) localEntry(s Snapshot, b Branch, path string) pathEntry {
	short := b.Name.Short()
	current := !s.Detached && short == s.Current
	icon := "branch"
	switch {
	case current:
		icon = "branch_current"
	case v.mainNames[short]:
		icon = mainBranchIcon
	}
	return pathEntry{
		path:          path,
		ref:           b.Name,
		current:       current,
		icon:          icon,
		when:          b.When,
		trackedRemote: v.pairedRemote[b.Name],
	}
}

func (v *View) buildMain(s Snapshot) *treeview.TreeViewItem {
	if !v.options.MainSection {
		return nil
	}
	var entries []pathEntry
	for _, b := range s.Local {
		if short := b.Name.Short(); v.mainNames[short] {
			entries = append(entries, v.localEntry(s, b, short))
		}
	}
	if len(entries) == 0 {
		return nil
	}
	root := v.newGroupItem(mainGroupKey, i18n.T("Pane.Branches.Main"))
	root.Bold = true
	sortRefEntries(entries, v.options.Sort)
	for _, entry := range entries {
		root.AddChild(v.leafItem(entry, entry.path))
	}
	return root
}
