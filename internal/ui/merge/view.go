package merge

import (
	"errors"
	"fmt"
	"slices"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/dialogs"
	"github.com/oops1/gogit/internal/ui/style"
)

const dialogName = "merge"

var ErrWidgetMissing = errors.New("merge: named widget missing")

var loadDialog = dialogs.Load

type View struct {
	dlg                 *widget.Dialog
	intoLabel           *widget.Label
	sourceList          *widget.Dropdown
	modeFastForward     *widget.RadioButton
	modeMergeCommit     *widget.RadioButton
	modeFastForwardOnly *widget.RadioButton
	modeSquash          *widget.RadioButton
	noCommitBox         *widget.CheckBox
	hintLabel           *widget.Label
	okBtn               *widget.Button
	cancelBtn           *widget.Button

	known   Known
	current Hint

	OnOK     func(Request)
	OnCancel func()
}

func NewView() (*View, error) {
	dlg, named, err := loadDialog(dialogName, i18n.T("Dialog.Merge.Title"))
	if err != nil {
		return nil, err
	}
	v := &View{dlg: dlg}
	if err := v.bind(named); err != nil {
		return nil, err
	}
	v.hintLabel.Muted = true
	v.wire()
	v.refresh()
	return v, nil
}

func (v *View) Dialog() *widget.Dialog { return v.dlg }

func (v *View) Restyle(t *widget.Theme) {
	p := style.Of(t)
	p.Lists(v.sourceList)
	p.Quiet(v.cancelBtn)
	p.Primary(v.okBtn)
	p.Body(v.intoLabel)
	p.Hints(v.hintLabel)
}

func (v *View) bind(named map[string]widget.Widget) error {
	var ok bool
	if v.intoLabel, ok = named["intoLabel"].(*widget.Label); !ok {
		return fmt.Errorf("%w: intoLabel", ErrWidgetMissing)
	}
	if v.sourceList, ok = named["source"].(*widget.Dropdown); !ok {
		return fmt.Errorf("%w: source", ErrWidgetMissing)
	}
	if v.modeFastForward, ok = named["modeFastForward"].(*widget.RadioButton); !ok {
		return fmt.Errorf("%w: modeFastForward", ErrWidgetMissing)
	}
	if v.modeMergeCommit, ok = named["modeMergeCommit"].(*widget.RadioButton); !ok {
		return fmt.Errorf("%w: modeMergeCommit", ErrWidgetMissing)
	}
	if v.modeFastForwardOnly, ok = named["modeFastForwardOnly"].(*widget.RadioButton); !ok {
		return fmt.Errorf("%w: modeFastForwardOnly", ErrWidgetMissing)
	}
	if v.modeSquash, ok = named["modeSquash"].(*widget.RadioButton); !ok {
		return fmt.Errorf("%w: modeSquash", ErrWidgetMissing)
	}
	if v.noCommitBox, ok = named["noCommit"].(*widget.CheckBox); !ok {
		return fmt.Errorf("%w: noCommit", ErrWidgetMissing)
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
	v.sourceList.OnChange = func(int, string) { v.refresh() }
	for _, rb := range []*widget.RadioButton{v.modeFastForward, v.modeMergeCommit, v.modeFastForwardOnly, v.modeSquash} {
		rb.OnChange = func(bool) { v.refresh() }
	}
	v.noCommitBox.OnChange = func(bool) { v.refresh() }
	v.okBtn.OnClick = v.confirm
	v.cancelBtn.OnClick = v.cancel
	v.dlg.DefaultAction = v.confirm
	v.dlg.CancelAction = v.cancel
}

func (v *View) SetKnown(known Known, selected string) {
	v.known = known
	v.intoLabel.SetText(i18n.Tf("Dialog.Merge.Into", known.Current))
	v.sourceList.SetItems(known.Candidates)
	v.sourceList.SetSelected(max(slices.Index(known.Candidates, selected), 0))
	v.refresh()
}

func (v *View) Mode() Mode {
	switch {
	case v.modeMergeCommit.IsSelected():
		return ModeMergeCommit
	case v.modeFastForwardOnly.IsSelected():
		return ModeFastForwardOnly
	case v.modeSquash.IsSelected():
		return ModeSquash
	default:
		return ModeFastForward
	}
}

func (v *View) Request() Request {
	mode := v.Mode()
	return Request{
		Source:   v.sourceList.SelectedText(),
		Mode:     mode,
		NoCommit: mode.AllowsNoCommit() && v.noCommitBox.IsChecked(),
	}
}

func (v *View) refresh() {
	v.noCommitBox.SetEnabled(v.Mode().AllowsNoCommit())
	v.current = Validate(v.Request(), v.known)
	v.hintLabel.SetText(i18n.Tf(v.current.Key, v.current.Args...))
	v.okBtn.SetEnabled(v.current.OK)
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
