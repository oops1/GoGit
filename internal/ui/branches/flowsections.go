package branches

import (
	"strings"

	"github.com/oops1/headless-gui/v3/widget/treeview"

	"github.com/oops1/gogit/internal/gitcore/ops"
	"github.com/oops1/gogit/internal/i18n"
)

const flowGroupKey = "flow"

var flowSectionKeys = map[string]string{
	ops.FlowKindFeature: "Pane.Branches.Flow.Features",
	ops.FlowKindRelease: "Pane.Branches.Flow.Releases",
	ops.FlowKindHotfix:  "Pane.Branches.Flow.Hotfixes",
	ops.FlowKindSupport: "Pane.Branches.Flow.Support",
}

func (v *View) SetFlow(cfg ops.FlowConfig, configured bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.flow, v.flowConfigured = cfg, configured
	if v.tree != nil {
		v.render(v.last)
	}
}

func (v *View) Flow() (ops.FlowConfig, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.flow, v.flowConfigured
}

func (v *View) flowKindOf(short string) (string, bool) {
	if !v.flowConfigured {
		return "", false
	}
	kind, _, ok := v.flow.BranchKind(short)
	return kind, ok
}

func (v *View) flowHoldsRemote(relative string) bool {
	if !v.options.FlowSections {
		return false
	}
	_, ok := v.flowKindOf(relative)
	return ok
}

func (v *View) buildFlowSections(s Snapshot) []*treeview.TreeViewItem {
	if !v.flowConfigured {
		return nil
	}
	var sections []*treeview.TreeViewItem
	for _, kind := range ops.FlowKinds {
		entries := v.flowEntries(s, kind)
		if len(entries) == 0 {
			continue
		}
		key := flowGroupKey + "/" + kind
		section := v.newGroupItem(key, i18n.Tf(flowSectionKeys[kind], len(entries)))
		v.buildPathTree(section, key, entries)
		sections = append(sections, section)
	}
	return sections
}

func (v *View) flowEntries(s Snapshot, kind string) []pathEntry {
	var entries []pathEntry
	for _, b := range s.Local {
		short := b.Name.Short()
		found, ok := v.flowKindOf(short)
		if !ok || found != kind {
			continue
		}
		current := !s.Detached && short == s.Current
		icon := "branch"
		if current {
			icon = "branch_current"
		}
		entries = append(entries, pathEntry{
			path:    v.flowBranchName(short),
			ref:     b.Name,
			current: current,
			icon:    icon,
			when:    b.When,
		})
	}
	if !v.options.FlowSections {
		return entries
	}
	for _, remote := range s.Remotes {
		for _, b := range remote.Branches {
			if isRemoteHead(b.Name) {
				continue
			}
			relative := strings.TrimPrefix(b.Name.Short(), remote.Name+"/")
			found, ok := v.flowKindOf(relative)
			if !ok || found != kind {
				continue
			}
			entries = append(entries, pathEntry{
				path: remote.Name + "/" + v.flowBranchName(relative),
				ref:  b.Name,
				icon: "branch_remote",
				when: b.When,
			})
		}
	}
	return entries
}

func (v *View) flowBranchName(short string) string {
	_, name, ok := v.flow.BranchKind(short)
	if !ok {
		return short
	}
	return name
}
