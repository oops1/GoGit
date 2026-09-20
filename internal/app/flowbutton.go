package app

import (
	"slices"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/i18n"
)

const flowToolbarIcon = "gitflow"

var flowMenuLeaves = []menuLeafEntry{
	{Key: "Menu.Tools.GitFlow.StartFeature", Command: CmdFlowStartFeature},
	{Key: "Menu.Tools.GitFlow.IntegrateDevelop", Command: CmdFlowIntegrateDevelop},
	{Key: "Menu.Tools.GitFlow.FinishFeature", Command: CmdFlowFinishFeature},
	{},
	{Key: "Menu.Tools.GitFlow.StartHotfix", Command: CmdFlowStartHotfix},
	{Key: "Menu.Tools.GitFlow.FinishHotfix", Command: CmdFlowFinishHotfix},
	{},
	{Key: "Menu.Tools.GitFlow.StartRelease", Command: CmdFlowStartRelease},
	{Key: "Menu.Tools.GitFlow.FinishRelease", Command: CmdFlowFinishRelease},
	{},
	{Key: "Menu.Tools.GitFlow.StartSupport", Command: CmdFlowStartSupport},
	{},
	{Key: "Menu.Tools.GitFlow.Configure", Command: CmdFlowConfigure},
}

const flowLightLeaves = 4

func flowMenuEntries(light bool) []menuLeafEntry {
	if light {
		return append(slices.Clone(flowMenuLeaves[:flowLightLeaves]), flowMenuLeaves[len(flowMenuLeaves)-1])
	}
	return flowMenuLeaves
}

func (a *App) flowMenuItems() []widget.MenuItem {
	state := a.State()
	entries := flowMenuEntries(state.FlowLight)
	items := make([]widget.MenuItem, 0, len(entries))
	for _, entry := range entries {
		if entry.Command == "" {
			items = append(items, widget.MenuItem{Separator: true})
			continue
		}
		cmd := entry.Command
		items = append(items, widget.MenuItem{
			Text:     i18n.T(entry.Key),
			Disabled: !state.Enabled(cmd),
			OnClick:  func() { a.Dispatch(cmd) },
		})
	}
	return items
}
