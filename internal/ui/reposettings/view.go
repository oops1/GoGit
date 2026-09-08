package reposettings

import (
	"errors"
	"fmt"
	"slices"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/dialogs"
)

const dialogName = "repo_settings"

var ErrWidgetMissing = errors.New("reposettings: named widget missing")

var loadDialog = dialogs.Load

type View struct {
	dlg           *widget.Dialog
	nameInput     *widget.TextInput
	pathLabel     *widget.Label
	userNameInput *widget.TextInput
	emailInput    *widget.TextInput
	remoteDrop    *widget.Dropdown
	pullDrop      *widget.Dropdown
	autoFetchDrop *widget.Dropdown
	hintLabel     *widget.Label
	okBtn         *widget.Button
	cancelBtn     *widget.Button

	remotes   []string
	inherited Inherited
	current   Hint

	OnOK     func(Settings)
	OnCancel func()
}

func NewView() (*View, error) {
	dlg, named, err := loadDialog(dialogName, i18n.T("Dialog.RepoSettings.Title"))
	if err != nil {
		return nil, err
	}
	v := &View{}
	v.dlg = dlg
	if err := v.bind(named); err != nil {
		return nil, err
	}
	v.pathLabel.Muted = true
	v.hintLabel.Muted = true
	v.fillChoices()
	v.wire()
	v.refresh()
	return v, nil
}

func (v *View) Dialog() *widget.Dialog { return v.dlg }

func (v *View) bind(named map[string]widget.Widget) error {
	var ok bool
	if v.nameInput, ok = named["name"].(*widget.TextInput); !ok {
		return fmt.Errorf("%w: name", ErrWidgetMissing)
	}
	if v.pathLabel, ok = named["path"].(*widget.Label); !ok {
		return fmt.Errorf("%w: path", ErrWidgetMissing)
	}
	if v.userNameInput, ok = named["userName"].(*widget.TextInput); !ok {
		return fmt.Errorf("%w: userName", ErrWidgetMissing)
	}
	if v.emailInput, ok = named["userEmail"].(*widget.TextInput); !ok {
		return fmt.Errorf("%w: userEmail", ErrWidgetMissing)
	}
	if v.remoteDrop, ok = named["defaultRemote"].(*widget.Dropdown); !ok {
		return fmt.Errorf("%w: defaultRemote", ErrWidgetMissing)
	}
	if v.pullDrop, ok = named["pullStrategy"].(*widget.Dropdown); !ok {
		return fmt.Errorf("%w: pullStrategy", ErrWidgetMissing)
	}
	if v.autoFetchDrop, ok = named["autoFetch"].(*widget.Dropdown); !ok {
		return fmt.Errorf("%w: autoFetch", ErrWidgetMissing)
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
	v.nameInput.OnChange = func(string) { v.refresh() }
	v.userNameInput.OnChange = func(string) { v.refresh() }
	v.emailInput.OnChange = func(string) { v.refresh() }
	v.okBtn.OnClick = v.confirm
	v.cancelBtn.OnClick = v.cancel
	v.dlg.DefaultAction = v.confirm
	v.dlg.CancelAction = v.cancel
}

func (v *View) fillChoices() {
	v.pullDrop.SetItems(labelsOf(pullOrder, pullKeys))
	v.autoFetchDrop.SetItems(labelsOf(autoFetchOrder, autoFetchKeys))
	v.remoteDrop.SetItems([]string{v.inheritedRemoteLabel()})
}

func labelsOf(order []string, keys map[string]string) []string {
	out := make([]string, 0, len(order))
	for _, value := range order {
		out = append(out, i18n.T(keys[value]))
	}
	return out
}

func (v *View) inheritedRemoteLabel() string {
	if v.inherited.DefaultRemote == "" {
		return i18n.T("Dialog.RepoSettings.Remote.Inherit")
	}
	return i18n.Tf("Dialog.RepoSettings.Remote.InheritNamed", v.inherited.DefaultRemote)
}

func (v *View) SetRemotes(names []string) {
	v.remotes = slices.Clone(names)
	v.remoteDrop.SetItems(append([]string{v.inheritedRemoteLabel()}, v.remotes...))
}

func (v *View) SetInherited(inherited Inherited) {
	v.inherited = inherited
	v.userNameInput.Placeholder = inherited.UserName
	v.emailInput.Placeholder = inherited.UserEmail
	v.autoFetchDrop.SetItems(labelsOf(autoFetchOrder, autoFetchKeys))
	v.SetRemotes(v.remotes)
}

func (v *View) Apply(s Settings) {
	v.nameInput.SetText(s.Name)
	v.pathLabel.SetText(s.Path)
	v.userNameInput.SetText(s.UserName)
	v.emailInput.SetText(s.UserEmail)
	v.pullDrop.SetSelected(indexOf(pullOrder, s.PullStrategy))
	v.autoFetchDrop.SetSelected(indexOf(autoFetchOrder, s.AutoFetch))
	v.remoteDrop.SetSelected(v.remoteIndex(s.DefaultRemote))
	v.refresh()
}

func indexOf(order []string, value string) int {
	if at := slices.Index(order, value); at >= 0 {
		return at
	}
	return 0
}

func (v *View) remoteIndex(name string) int {
	if at := slices.Index(v.remotes, name); at >= 0 {
		return at + 1
	}
	return 0
}

func (v *View) Settings() Settings {
	return Normalise(Settings{
		Name:          v.nameInput.GetText(),
		Path:          v.pathLabel.Text(),
		UserName:      v.userNameInput.GetText(),
		UserEmail:     v.emailInput.GetText(),
		DefaultRemote: v.selectedRemote(),
		PullStrategy:  pullOrder[v.pullDrop.Selected()],
		AutoFetch:     autoFetchOrder[v.autoFetchDrop.Selected()],
	})
}

func (v *View) selectedRemote() string {
	at := v.remoteDrop.Selected() - 1
	if at < 0 || at >= len(v.remotes) {
		return ""
	}
	return v.remotes[at]
}

func (v *View) refresh() {
	v.current = Validate(v.Settings())
	v.hintLabel.SetText(i18n.Tf(v.current.Key, v.current.Args...))
	v.okBtn.SetEnabled(v.current.OK)
}

func (v *View) confirm() {
	if !v.current.OK {
		return
	}
	if v.OnOK != nil {
		v.OnOK(v.Settings())
	}
}

func (v *View) cancel() {
	if v.OnCancel != nil {
		v.OnCancel()
	}
}
