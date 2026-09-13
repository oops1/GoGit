package app

import (
	"slices"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/i18n"
)

const flowToolbarIcon = "gitflow"

var flowMenuLeaves = []menuLeafEntry{
	{Key: "Menu.Branch.GitFlow.StartFeature", Command: CmdFlowStartFeature},
	{Key: "Menu.Branch.GitFlow.IntegrateDevelop", Command: CmdFlowIntegrateDevelop},
	{Key: "Menu.Branch.GitFlow.FinishFeature", Command: CmdFlowFinishFeature},
	{},
	{Key: "Menu.Branch.GitFlow.StartHotfix", Command: CmdFlowStartHotfix},
	{Key: "Menu.Branch.GitFlow.FinishHotfix", Command: CmdFlowFinishHotfix},
	{},
	{Key: "Menu.Branch.GitFlow.StartRelease", Command: CmdFlowStartRelease},
	{Key: "Menu.Branch.GitFlow.FinishRelease", Command: CmdFlowFinishRelease},
	{},
	{Key: "Menu.Branch.GitFlow.StartSupport", Command: CmdFlowStartSupport},
	{},
	{Key: "Menu.Branch.GitFlow.Configure", Command: CmdFlowConfigure},
}

const flowLightLeaves = 4

func flowMenuEntries(light bool) []menuLeafEntry {
	if light {
		return append(slices.Clone(flowMenuLeaves[:flowLightLeaves]), flowMenuLeaves[len(flowMenuLeaves)-1])
	}
	return flowMenuLeaves
}

func (a *App) flowButton() (*widget.MenuButton, bool) {
	btn, ok := a.named["btnGitFlow"].(*widget.MenuButton)
	return btn, ok
}

func (a *App) toolbarCaptionButtons() []*widget.Button {
	buttons := make([]*widget.Button, 0, len(toolbarButtons)+1)
	for _, name := range toolbarButtons {
		buttons = append(buttons, a.named[name].(*widget.Button))
	}
	if flow, ok := a.flowButton(); ok {
		buttons = append(buttons, flow.Button)
	}
	return buttons
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

func (a *App) wireFlowButton() {
	if flow, ok := a.flowButton(); ok {
		flow.OnOpening = func() { flow.Items = a.flowMenuItems() }
	}
}

func (a *App) refreshFlowButton(state State) {
	if flow, ok := a.flowButton(); ok {
		flow.SetEnabled(state.ActiveRepository != "")
	}
}
