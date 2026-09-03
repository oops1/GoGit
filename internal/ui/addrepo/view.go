package addrepo

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/dialogs"
)

const dialogName = "add_repo"

var ErrWidgetMissing = errors.New("addrepo: named widget missing")

var loadDialog = dialogs.Load

type View struct {
	dlg        *widget.Dialog
	pathInput  *widget.TextInput
	browseBtn  *widget.Button
	hintLabel  *widget.Label
	modeOpen   *widget.RadioButton
	modeCreate *widget.RadioButton
	bareCheck  *widget.CheckBox
	nameInput  *widget.TextInput
	okBtn      *widget.Button
	cancelBtn  *widget.Button

	eng     widget.ModalShower
	current Hint

	OnOK     func(Request)
	OnCancel func()
}

func NewView(eng widget.ModalShower, initial Request) (*View, error) {
	dlg, named, err := loadDialog(dialogName, i18n.T("Dialog.AddRepo.Title"))
	if err != nil {
		return nil, err
	}
	v := &View{dlg: dlg, eng: eng}
	if err := v.bind(named); err != nil {
		return nil, err
	}
	v.applyRequest(initial)
	v.wire()
	v.autoFillName()
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
	if v.hintLabel, ok = named["hint"].(*widget.Label); !ok {
		return fmt.Errorf("%w: hint", ErrWidgetMissing)
	}
	if v.modeOpen, ok = named["modeOpen"].(*widget.RadioButton); !ok {
		return fmt.Errorf("%w: modeOpen", ErrWidgetMissing)
	}
	if v.modeCreate, ok = named["modeCreate"].(*widget.RadioButton); !ok {
		return fmt.Errorf("%w: modeCreate", ErrWidgetMissing)
	}
	if v.bareCheck, ok = named["bare"].(*widget.CheckBox); !ok {
		return fmt.Errorf("%w: bare", ErrWidgetMissing)
	}
	if v.nameInput, ok = named["name"].(*widget.TextInput); !ok {
		return fmt.Errorf("%w: name", ErrWidgetMissing)
	}
	if v.okBtn, ok = named["ok"].(*widget.Button); !ok {
		return fmt.Errorf("%w: ok", ErrWidgetMissing)
	}
	if v.cancelBtn, ok = named["cancel"].(*widget.Button); !ok {
		return fmt.Errorf("%w: cancel", ErrWidgetMissing)
	}
	return nil
}

func (v *View) applyRequest(req Request) {
	v.pathInput.SetText(req.Path)
	v.nameInput.SetText(req.Name)
	v.bareCheck.SetChecked(req.Bare)
	if req.Mode == ModeCreate {
		v.modeCreate.SetSelected(true)
	} else {
		v.modeOpen.SetSelected(true)
	}
	v.applyModeVisibility()
}

func (v *View) wire() {
	v.pathInput.OnChange = func(string) { v.onPathChanged() }
	v.nameInput.OnChange = func(string) { v.refresh() }
	v.modeOpen.OnChange = func(bool) { v.onModeChanged() }
	v.modeCreate.OnChange = func(bool) { v.onModeChanged() }
	v.bareCheck.OnChange = func(bool) { v.refresh() }
	v.browseBtn.OnClick = v.browse
	v.okBtn.OnClick = v.confirm
	v.cancelBtn.OnClick = v.cancel
	v.dlg.DefaultAction = v.confirm
	v.dlg.CancelAction = v.cancel
}

func (v *View) onPathChanged() {
	v.autoFillName()
	v.refresh()
}

func (v *View) onModeChanged() {
	v.applyModeVisibility()
	v.autoFillName()
	v.refresh()
}

func (v *View) applyModeVisibility() {
	create := v.modeCreate.IsSelected()
	v.bareCheck.SetVisible(create)
	v.nameInput.SetVisible(create)
}

func (v *View) autoFillName() {
	if !v.modeCreate.IsSelected() || strings.TrimSpace(v.nameInput.GetText()) != "" {
		return
	}
	base := filepath.Base(filepath.Clean(v.pathInput.GetText()))
	if base == "." || base == string(filepath.Separator) {
		return
	}
	v.nameInput.SetText(base)
}

func (v *View) request() Request {
	mode := ModeOpen
	if v.modeCreate.IsSelected() {
		mode = ModeCreate
	}
	return Request{
		Path: v.pathInput.GetText(),
		Name: v.nameInput.GetText(),
		Bare: v.bareCheck.IsChecked(),
		Mode: mode,
	}
}

func (v *View) refresh() {
	v.current = Validate(v.request())
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
	v.onPathChanged()
}

func (v *View) confirm() {
	if !v.current.OK {
		return
	}
	req := v.request()
	if v.OnOK != nil {
		v.OnOK(req)
	}
}

func (v *View) cancel() {
	if v.OnCancel != nil {
		v.OnCancel()
	}
}
