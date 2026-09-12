package switchbranch

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/dialogs"
	"github.com/oops1/gogit/internal/ui/style"
)

const dialogName = "switch_branch"

var ErrWidgetMissing = errors.New("switchbranch: named widget missing")

var loadDialog = dialogs.Load

const (
	hintSourceRequired = "Dialog.Switch.Hint.SourceRequired"
	hintAlreadyThere   = "Dialog.Switch.Hint.AlreadyThere"
	hintNameRequired   = "Dialog.Switch.Hint.NameRequired"
	hintNameInvalid    = "Dialog.Switch.Hint.NameInvalid"
	hintNameTaken      = "Dialog.Switch.Hint.NameTaken"
	hintWillSwitch     = "Dialog.Switch.Hint.WillSwitch"
	hintWillStart      = "Dialog.Switch.Hint.WillStart"
)

type Known struct {
	Current string
	Local   []string
	Remote  []string
}

func (k Known) Sources() []string { return append(slices.Clone(k.Local), k.Remote...) }

type Choice struct {
	Source string
	Name   string
	Track  bool
}

func (c Choice) StartsABranch() bool { return c.Name != "" }

type Hint struct {
	Key  string
	Args []any
	OK   bool
}

func Validate(choice Choice, known Known) Hint {
	source := strings.TrimSpace(choice.Source)
	switch {
	case source == "":
		return Hint{Key: hintSourceRequired}
	case slices.Contains(known.Local, source):
		return localHint(source, known)
	}
	return remoteHint(choice, source, known)
}

func localHint(source string, known Known) Hint {
	if source == known.Current {
		return Hint{Key: hintAlreadyThere, Args: []any{source}}
	}
	return Hint{Key: hintWillSwitch, Args: []any{source}, OK: true}
}

func remoteHint(choice Choice, source string, known Known) Hint {
	name := strings.TrimSpace(choice.Name)
	switch {
	case name == "":
		return Hint{Key: hintNameRequired, Args: []any{source}}
	case !validName(name):
		return Hint{Key: hintNameInvalid, Args: []any{name}}
	case slices.Contains(known.Local, name):
		return Hint{Key: hintNameTaken, Args: []any{name}}
	}
	return Hint{Key: hintWillStart, Args: []any{name, source}, OK: true}
}

func validName(name string) bool {
	if strings.Contains(name, "..") || strings.HasPrefix(name, "-") || strings.HasSuffix(name, ".lock") {
		return false
	}
	return !strings.ContainsAny(name, " ~^:?*[\\\t")
}

func LocalNameFor(remote string) string {
	_, head, found := strings.Cut(remote, "/")
	if !found {
		return remote
	}
	return head
}

type View struct {
	dlg         *widget.Dialog
	sourceLabel *widget.Label
	sourceList  *widget.Dropdown
	nameLabel   *widget.Label
	nameBox     *widget.TextInput
	trackCheck  *widget.CheckBox
	hintLabel   *widget.Label
	okBtn       *widget.Button
	cancelBtn   *widget.Button

	known   Known
	current Hint

	OnOK     func(choice Choice)
	OnCancel func()
}

func NewView() (*View, error) {
	dlg, named, err := loadDialog(dialogName, i18n.T("Dialog.Switch.Title"))
	if err != nil {
		return nil, err
	}
	v := &View{dlg: dlg}
	if err := v.bind(named); err != nil {
		return nil, err
	}
	v.hintLabel.Muted = true
	v.trackCheck.SetChecked(true)
	v.wire()
	v.refresh()
	return v, nil
}

func (v *View) Dialog() *widget.Dialog { return v.dlg }

func (v *View) Restyle(t *widget.Theme) {
	p := style.Of(t)
	p.Lists(v.sourceList)
	p.Fields(v.nameBox)
	p.Quiet(v.cancelBtn)
	p.Primary(v.okBtn)
	p.Body(v.sourceLabel, v.nameLabel)
	p.Hints(v.hintLabel)
}

func (v *View) bind(named map[string]widget.Widget) error {
	var ok bool
	if v.sourceLabel, ok = named["sourceLabel"].(*widget.Label); !ok {
		return fmt.Errorf("%w: sourceLabel", ErrWidgetMissing)
	}
	if v.sourceList, ok = named["source"].(*widget.Dropdown); !ok {
		return fmt.Errorf("%w: source", ErrWidgetMissing)
	}
	if v.nameLabel, ok = named["nameLabel"].(*widget.Label); !ok {
		return fmt.Errorf("%w: nameLabel", ErrWidgetMissing)
	}
	if v.nameBox, ok = named["name"].(*widget.TextInput); !ok {
		return fmt.Errorf("%w: name", ErrWidgetMissing)
	}
	if v.trackCheck, ok = named["track"].(*widget.CheckBox); !ok {
		return fmt.Errorf("%w: track", ErrWidgetMissing)
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
	v.sourceList.OnChange = func(int, string) { v.onSourceChanged() }
	v.nameBox.OnChange = func(string) { v.refresh() }
	v.trackCheck.OnChange = func(bool) { v.refresh() }
	v.okBtn.OnClick = v.confirm
	v.cancelBtn.OnClick = v.cancel
	v.dlg.DefaultAction = v.confirm
	v.dlg.CancelAction = v.cancel
}

func (v *View) SetKnown(known Known, selected string) {
	v.known = known
	sources := known.Sources()
	v.sourceList.SetItems(sources)
	v.sourceList.SetSelected(max(slices.Index(sources, selected), 0))
	v.onSourceChanged()
}

func (v *View) Choice() Choice {
	source := v.sourceList.SelectedText()
	choice := Choice{Source: source, Track: v.trackCheck.IsChecked()}
	if slices.Contains(v.known.Remote, source) {
		choice.Name = strings.TrimSpace(v.nameBox.GetText())
	}
	return choice
}

func (v *View) onSourceChanged() {
	source := v.sourceList.SelectedText()
	remote := slices.Contains(v.known.Remote, source)
	v.nameBox.SetText("")
	if remote {
		v.nameBox.SetText(LocalNameFor(source))
	}
	v.nameBox.SetEnabled(remote)
	v.trackCheck.SetEnabled(remote)
	v.refresh()
}

func (v *View) refresh() {
	v.current = Validate(v.Choice(), v.known)
	v.hintLabel.SetText(i18n.Tf(v.current.Key, v.current.Args...))
	v.okBtn.SetEnabled(v.current.OK)
}

func (v *View) confirm() {
	if !v.current.OK {
		return
	}
	if v.OnOK != nil {
		v.OnOK(v.Choice())
	}
}

func (v *View) cancel() {
	if v.OnCancel != nil {
		v.OnCancel()
	}
}
