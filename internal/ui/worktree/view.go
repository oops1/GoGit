package worktree

import (
	"errors"
	"fmt"
	"strings"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/dialogs"
)

const dialogName = "worktree_add"

var ErrWidgetMissing = errors.New("worktree: named widget missing")

var loadDialog = dialogs.Load

type View struct {
	dlg           *widget.Dialog
	pathInput     *widget.TextInput
	browseBtn     *widget.Button
	modeNew       *widget.RadioButton
	modeExisting  *widget.RadioButton
	modeDetached  *widget.RadioButton
	branchLabel   *widget.Label
	branchInput   *widget.TextInput
	branchList    *widget.Dropdown
	startLabel    *widget.Label
	startInput    *widget.TextInput
	noCheckoutBox *widget.CheckBox
	hintLabel     *widget.Label
	okBtn         *widget.Button
	cancelBtn     *widget.Button

	eng        widget.ModalShower
	known      Known
	parentDir  string
	pathEdited bool
	current    Hint

	OnOK     func(Request)
	OnCancel func()
}

func NewView(eng widget.ModalShower) (*View, error) {
	dlg, named, err := loadDialog(dialogName, i18n.T("Dialog.Worktree.Title"))
	if err != nil {
		return nil, err
	}
	v := &View{dlg: dlg, eng: eng}
	if err := v.bind(named); err != nil {
		return nil, err
	}
	v.hintLabel.Muted = true
	v.wire()
	v.applyMode()
	v.refresh()
	return v, nil
}

func (v *View) Dialog() *widget.Dialog { return v.dlg }

func (v *View) bind(named map[string]widget.Widget) error {
	var ok bool
	if v.pathInput, ok = named["path"].(*widget.TextInput); !ok {
		return fmt.Errorf("%w: path", ErrWidgetMissing)
	}
	if v.browseBtn, ok = named["browse"].(*widget.Button); !ok {
		return fmt.Errorf("%w: browse", ErrWidgetMissing)
	}
	if v.modeNew, ok = named["modeNew"].(*widget.RadioButton); !ok {
		return fmt.Errorf("%w: modeNew", ErrWidgetMissing)
	}
	if v.modeExisting, ok = named["modeExisting"].(*widget.RadioButton); !ok {
		return fmt.Errorf("%w: modeExisting", ErrWidgetMissing)
	}
	if v.modeDetached, ok = named["modeDetached"].(*widget.RadioButton); !ok {
		return fmt.Errorf("%w: modeDetached", ErrWidgetMissing)
	}
	if v.branchLabel, ok = named["branchLabel"].(*widget.Label); !ok {
		return fmt.Errorf("%w: branchLabel", ErrWidgetMissing)
	}
	if v.branchInput, ok = named["branchName"].(*widget.TextInput); !ok {
		return fmt.Errorf("%w: branchName", ErrWidgetMissing)
	}
	if v.branchList, ok = named["branchList"].(*widget.Dropdown); !ok {
		return fmt.Errorf("%w: branchList", ErrWidgetMissing)
	}
	if v.startLabel, ok = named["startLabel"].(*widget.Label); !ok {
		return fmt.Errorf("%w: startLabel", ErrWidgetMissing)
	}
	if v.startInput, ok = named["startPoint"].(*widget.TextInput); !ok {
		return fmt.Errorf("%w: startPoint", ErrWidgetMissing)
	}
	if v.noCheckoutBox, ok = named["noCheckout"].(*widget.CheckBox); !ok {
		return fmt.Errorf("%w: noCheckout", ErrWidgetMissing)
	}
	if v.hintLabel, ok = named["hint"].(*widget.Label); !ok {
		return fmt.Errorf("%w: hint", ErrWidgetMissing)
	}
	if v.okBtn, ok = named["ok"].(*widget.Button); !ok {
		return fmt.Errorf("%w: ok", ErrWidgetMissing)
	}
	if v.cancelBtn, ok = named["cancel"].(*widget.Button); !ok {
		return fmt.Errorf("%w: cancel", ErrWidgetMissing)
	}
	return nil
}

func (v *View) wire() {
	v.pathInput.OnChange = func(string) { v.onPathTyped() }
	v.branchInput.OnChange = func(string) { v.onBranchTyped() }
	v.startInput.OnChange = func(string) { v.onStartTyped() }
	v.branchList.OnChange = func(int, string) { v.onBranchPicked() }
	v.modeNew.OnChange = func(bool) { v.onModeChanged() }
	v.modeExisting.OnChange = func(bool) { v.onModeChanged() }
	v.modeDetached.OnChange = func(bool) { v.onModeChanged() }
	v.noCheckoutBox.OnChange = func(bool) { v.refresh() }
	v.browseBtn.OnClick = v.browse
	v.okBtn.OnClick = v.confirm
	v.cancelBtn.OnClick = v.cancel
	v.dlg.DefaultAction = v.confirm
	v.dlg.CancelAction = v.cancel
}

func (v *View) SetKnown(known Known) {
	v.known = known
	v.branchList.SetItems(known.Branches)
	v.refresh()
}

func (v *View) Known() Known { return v.known }

func (v *View) ParentDirectory() string { return v.parentDir }

func (v *View) SetParentDirectory(dir string) {
	v.parentDir = dir
	v.autoFillPath()
	v.refresh()
}

func (v *View) Mode() Mode {
	switch {
	case v.modeExisting.IsSelected():
		return ModeExistingBranch
	case v.modeDetached.IsSelected():
		return ModeDetached
	default:
		return ModeNewBranch
	}
}

func (v *View) Request() Request {
	mode := v.Mode()
	branch := v.branchInput.GetText()
	if mode == ModeExistingBranch {
		branch = v.branchList.SelectedText()
	}
	return Request{
		Path:       v.pathInput.GetText(),
		Mode:       mode,
		Branch:     branch,
		StartPoint: v.startInput.GetText(),
		NoCheckout: v.noCheckoutBox.IsChecked(),
	}
}

func (v *View) onPathTyped() {
	v.pathEdited = strings.TrimSpace(v.pathInput.GetText()) != ""
	v.refresh()
}

func (v *View) onBranchTyped() {
	v.autoFillPath()
	v.refresh()
}

func (v *View) onStartTyped() {
	v.autoFillPath()
	v.refresh()
}

func (v *View) onBranchPicked() {
	v.autoFillPath()
	v.refresh()
}

func (v *View) onModeChanged() {
	v.applyMode()
	v.autoFillPath()
	v.refresh()
}

func (v *View) applyMode() {
	mode := v.Mode()
	v.branchInput.SetVisible(mode == ModeNewBranch)
	v.branchList.SetVisible(mode == ModeExistingBranch)
	v.branchLabel.SetVisible(mode != ModeDetached)
	v.startLabel.SetVisible(mode != ModeExistingBranch)
	v.startInput.SetVisible(mode != ModeExistingBranch)
	v.branchLabel.SetText(i18n.T(branchLabelKey(mode)))
}

func branchLabelKey(mode Mode) string {
	if mode == ModeExistingBranch {
		return "Dialog.Worktree.BranchPickLabel"
	}
	return "Dialog.Worktree.BranchLabel"
}

func (v *View) autoFillPath() {
	if v.pathEdited {
		return
	}
	name := v.selectedBranch()
	if dir := DirectoryFor(v.parentDir, name); dir != "" {
		v.pathInput.SetText(dir)
	}
}

func (v *View) selectedBranch() string {
	switch v.Mode() {
	case ModeExistingBranch:
		return v.branchList.SelectedText()
	case ModeDetached:
		return v.startInput.GetText()
	default:
		return v.branchInput.GetText()
	}
}

func (v *View) refresh() {
	v.current = Validate(v.Request(), v.known)
	v.hintLabel.SetText(i18n.Tf(v.current.Key, v.current.Args...))
	v.okBtn.SetEnabled(v.current.OK)
}

func (v *View) browse() {
	widget.NewMessageBox(v.eng).ShowPickFolder(widget.FileDialogOptions{StartDir: v.pathInput.GetText()}, v.onFolderPicked)
}

func (v *View) onFolderPicked(path string, ok bool) {
	if !ok {
		return
	}
	v.pathInput.SetText(path)
	v.onPathTyped()
}

func (v *View) confirm() {
	if !v.current.OK {
		return
	}
	if v.OnOK != nil {
		v.OnOK(v.Request())
	}
}

func (v *View) cancel() {
	if v.OnCancel != nil {
		v.OnCancel()
	}
}
