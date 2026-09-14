package flow

import (
	"github.com/oops1/gogit/internal/gitcore/ops"
)

type kindTexts struct {
	startTitle   string
	startHeader  string
	startText    string
	nameLabel    string
	nameRequired string
	finishDialog string
	finishTitle  string
	finishHeader string
	finishText   string
	deleteBranch string
	pushRemove   string
}

var kindKeys = map[string]kindTexts{
	ops.FlowKindFeature: {
		startTitle:   "Dialog.FlowStart.Title.Feature",
		startHeader:  "Dialog.FlowStart.Header.Feature",
		startText:    "Dialog.FlowStart.Text.Feature",
		nameLabel:    "Dialog.FlowStart.NameLabel.Feature",
		nameRequired: "Dialog.FlowStart.Hint.NameRequired.Feature",
		finishDialog: "flow_finish_feature",
		finishTitle:  "Dialog.FlowFinish.Title.Feature",
		finishHeader: "Dialog.FlowFinish.Header.Feature",
		finishText:   "Dialog.FlowFinish.Text.Feature",
		deleteBranch: "Dialog.FlowFinish.DeleteBranch.Feature",
		pushRemove:   "Dialog.FlowFinish.FetchRemove.Feature",
	},
	ops.FlowKindRelease: {
		startTitle:   "Dialog.FlowStart.Title.Release",
		startHeader:  "Dialog.FlowStart.Header.Release",
		startText:    "Dialog.FlowStart.Text.Release",
		nameLabel:    "Dialog.FlowStart.NameLabel.Release",
		nameRequired: "Dialog.FlowStart.Hint.NameRequired.Release",
		finishDialog: "flow_finish_release",
		finishTitle:  "Dialog.FlowFinish.Title.Release",
		finishHeader: "Dialog.FlowFinish.Header.Release",
		finishText:   "Dialog.FlowFinish.Text.Release",
		deleteBranch: "Dialog.FlowFinish.DeleteBranch.Release",
		pushRemove:   "Dialog.FlowFinish.PushRemove.Release",
	},
	ops.FlowKindHotfix: {
		startTitle:   "Dialog.FlowStart.Title.Hotfix",
		startHeader:  "Dialog.FlowStart.Header.Hotfix",
		startText:    "Dialog.FlowStart.Text.Hotfix",
		nameLabel:    "Dialog.FlowStart.NameLabel.Hotfix",
		nameRequired: "Dialog.FlowStart.Hint.NameRequired.Hotfix",
		finishDialog: "flow_finish_hotfix",
		finishTitle:  "Dialog.FlowFinish.Title.Hotfix",
		finishHeader: "Dialog.FlowFinish.Header.Hotfix",
		finishText:   "Dialog.FlowFinish.Text.Hotfix",
		deleteBranch: "Dialog.FlowFinish.DeleteBranch.Hotfix",
		pushRemove:   "Dialog.FlowFinish.PushRemove.Hotfix",
	},
	ops.FlowKindSupport: {
		startTitle:   "Dialog.FlowStart.Title.Support",
		startHeader:  "Dialog.FlowStart.Header.Support",
		startText:    "Dialog.FlowStart.Text.Support",
		nameLabel:    "Dialog.FlowStart.NameLabel.Support",
		nameRequired: "Dialog.FlowStart.Hint.NameRequired.Support",
	},
}
