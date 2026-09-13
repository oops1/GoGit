package flow

import (
	"github.com/oops1/gogit/internal/gitcore/ops"
)

type kindTexts struct {
	tagged       bool
	startTitle   string
	nameLabel    string
	nameRequired string
	finishTitle  string
	branchLabel  string
	message      string
}

var kindKeys = map[string]kindTexts{
	ops.FlowKindFeature: {
		startTitle:   "Dialog.FlowStart.Title.Feature",
		nameLabel:    "Dialog.FlowStart.NameLabel.Feature",
		nameRequired: "Dialog.FlowStart.Hint.NameRequired.Feature",
		finishTitle:  "Dialog.FlowFinish.Title.Feature",
		branchLabel:  "Dialog.FlowFinish.Branch.Feature",
		message:      "Dialog.FlowFinish.DefaultMessage.Feature",
	},
	ops.FlowKindRelease: {
		tagged:       true,
		startTitle:   "Dialog.FlowStart.Title.Release",
		nameLabel:    "Dialog.FlowStart.NameLabel.Release",
		nameRequired: "Dialog.FlowStart.Hint.NameRequired.Release",
		finishTitle:  "Dialog.FlowFinish.Title.Release",
		branchLabel:  "Dialog.FlowFinish.Branch.Release",
		message:      "Dialog.FlowFinish.DefaultMessage.Release",
	},
	ops.FlowKindHotfix: {
		tagged:       true,
		startTitle:   "Dialog.FlowStart.Title.Hotfix",
		nameLabel:    "Dialog.FlowStart.NameLabel.Hotfix",
		nameRequired: "Dialog.FlowStart.Hint.NameRequired.Hotfix",
		finishTitle:  "Dialog.FlowFinish.Title.Hotfix",
		branchLabel:  "Dialog.FlowFinish.Branch.Hotfix",
		message:      "Dialog.FlowFinish.DefaultMessage.Hotfix",
	},
}
