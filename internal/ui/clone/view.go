package clone

import (
	"errors"
	"fmt"
	"strings"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/dialogs"
	"github.com/oops1/gogit/internal/ui/style"
)

const dialogName = "clone"

var ErrWidgetMissing = errors.New("clone: named widget missing")

var loadDialog = dialogs.Load

type Request struct {
	URL       string
	Directory string
}

type Result struct {
	URL       string
	Directory string
	Branch    string
	Depth     int
}

type View struct {
	dlg          *widget.Dialog
	urlInput     *widget.TextInput
	dirInput     *widget.TextInput
	browseBtn    *widget.Button
	checkBtn     *widget.Button
	statusLabel  *widget.Label
	branchDrop   *widget.Dropdown
	shallowCheck *widget.CheckBox
	depthInput   *widget.NumericUpDown
	okBtn        *widget.Button
	cancelBtn    *widget.Button

	eng  widget.ModalShower
	busy bool

	OnOK     func(Result)
	OnCancel func()
	OnCheck  func(url string)
}

func NewView(eng widget.ModalShower, req Request) (*View, error) {
	dlg, named, err := loadDialog(dialogName, i18n.T("Dialog.Clone.Title"))
	if err != nil {
		return nil, err
	}
	v := &View{dlg: dlg, eng: eng}
	if err := v.bind(named); err != nil {
		return nil, err
	}
	v.apply(req)
	v.wire()
	v.refresh()
	return v, nil
}

func (v *View) Dialog() *widget.Dialog { return v.dlg }

func (v *View) Restyle(t *widget.Theme) {
	p := style.Of(t)
	p.Fields(v.urlInput, v.dirInput)
	p.Lists(v.branchDrop)
	p.Quiet(v.browseBtn, v.checkBtn, v.cancelBtn)
	p.Primary(v.okBtn)
	p.Hints(v.statusLabel)
}

func (v *View) bind(named map[string]widget.Widget) error {
	var ok bool
	if v.urlInput, ok = named["url"].(*widget.TextInput); !ok {
		return fmt.Errorf("%w: url", ErrWidgetMissing)
	}
	if v.dirInput, ok = named["directory"].(*widget.TextInput); !ok {
		return fmt.Errorf("%w: directory", ErrWidgetMissing)
	}
	if v.browseBtn, ok = named["browse"].(*widget.Button); !ok {
		return fmt.Errorf("%w: browse", ErrWidgetMissing)
	}
	if v.checkBtn, ok = named["check"].(*widget.Button); !ok {
		return fmt.Errorf("%w: check", ErrWidgetMissing)
	}
	if v.statusLabel, ok = named["status"].(*widget.Label); !ok {
		return fmt.Errorf("%w: status", ErrWidgetMissing)
	}
	if v.branchDrop, ok = named["branch"].(*widget.Dropdown); !ok {
		return fmt.Errorf("%w: branch", ErrWidgetMissing)
	}
	if v.shallowCheck, ok = named["shallow"].(*widget.CheckBox); !ok {
		return fmt.Errorf("%w: shallow", ErrWidgetMissing)
	}
	if v.depthInput, ok = named["depth"].(*widget.NumericUpDown); !ok {
		return fmt.Errorf("%w: depth", ErrWidgetMissing)
	}
	if v.okBtn, ok = named["ok"].(*widget.Button); !ok {
		return fmt.Errorf("%w: ok", ErrWidgetMissing)
	}
	if v.cancelBtn, ok = named["cancel"].(*widget.Button); !ok {
		return fmt.Errorf("%w: cancel", ErrWidgetMissing)
	}
	return nil
}

func (v *View) apply(req Request) {
	v.urlInput.SetText(req.URL)
	v.dirInput.SetText(req.Directory)
	v.depthInput.SetEnabled(v.shallowCheck.IsChecked())
}

func (v *View) wire() {
	v.urlInput.OnChange = func(string) { v.refresh() }
	v.dirInput.OnChange = func(string) { v.refresh() }
	v.browseBtn.OnClick = v.browse
	v.checkBtn.OnClick = v.onCheckClicked
	v.shallowCheck.OnChange = func(bool) { v.onShallowChanged() }
	v.okBtn.OnClick = v.confirm
	v.cancelBtn.OnClick = v.cancel
	v.dlg.DefaultAction = v.confirm
	v.dlg.CancelAction = v.cancel
}

func (v *View) onShallowChanged() {
	v.depthInput.SetEnabled(v.shallowCheck.IsChecked())
}

func (v *View) refresh() {
	canConfirm := strings.TrimSpace(v.urlInput.GetText()) != "" &&
		strings.TrimSpace(v.dirInput.GetText()) != ""
	if v.busy {
		canConfirm = false
	}
	v.okBtn.SetEnabled(canConfirm)
}

func (v *View) browse() {
	widget.NewMessageBox(v.eng).ShowPickFolder(widget.FileDialogOptions{StartDir: v.dirInput.GetText()}, v.onFolderPicked)
}

func (v *View) onFolderPicked(path string, ok bool) {
	if !ok {
		return
	}
	v.dirInput.SetText(path)
	v.refresh()
}

func (v *View) onCheckClicked() {
	url := strings.TrimSpace(v.urlInput.GetText())
	if url == "" {
		v.SetStatus(i18n.T("Dialog.Clone.Error.URL"))
		return
	}
	v.SetBusy(true)
	if v.OnCheck != nil {
		v.OnCheck(url)
	}
}

func (v *View) SetBranches(branches []string, head string) {
	v.branchDrop.SetItems(branches)
	for i, b := range branches {
		if b == head {
			v.branchDrop.SetSelected(i)
			return
		}
	}
}

func (v *View) SetStatus(text string) {
	v.statusLabel.SetText(text)
}

func (v *View) SetBusy(busy bool) {
	v.busy = busy
	v.checkBtn.SetEnabled(!busy)
	v.branchDrop.SetEnabled(!busy)
	v.shallowCheck.SetEnabled(!busy)
	if busy {
		v.depthInput.SetEnabled(false)
	} else {
		v.depthInput.SetEnabled(v.shallowCheck.IsChecked())
	}
	v.refresh()
}

func (v *View) depth() int {
	if !v.shallowCheck.IsChecked() {
		return 0
	}
	return int(v.depthInput.Value())
}

func (v *View) result() Result {
	return Result{
		URL:       strings.TrimSpace(v.urlInput.GetText()),
		Directory: strings.TrimSpace(v.dirInput.GetText()),
		Branch:    v.branchDrop.SelectedText(),
		Depth:     v.depth(),
	}
}

func (v *View) confirm() {
	if v.busy {
		return
	}
	url := strings.TrimSpace(v.urlInput.GetText())
	if url == "" {
		v.SetStatus(i18n.T("Dialog.Clone.Error.URL"))
		return
	}
	dir := strings.TrimSpace(v.dirInput.GetText())
	if dir == "" {
		v.SetStatus(i18n.T("Dialog.Clone.Error.Directory"))
		return
	}
	if v.OnOK != nil {
		v.OnOK(v.result())
	}
}

func (v *View) cancel() {
	if v.OnCancel != nil {
		v.OnCancel()
	}
}
