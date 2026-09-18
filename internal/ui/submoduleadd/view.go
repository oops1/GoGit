package submoduleadd

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/gitcore/submodule"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/dialogs"
	"github.com/oops1/gogit/internal/ui/style"
)

const dialogName = "submodule_add"

var ErrWidgetMissing = errors.New("submoduleadd: named widget missing")

var loadDialog = dialogs.Load

const (
	hintURLRequired    = "Dialog.SubmoduleAdd.Hint.URLRequired"
	hintURLNotAbsolute = "Dialog.SubmoduleAdd.Hint.URLNotAbsolute"
	hintPathRequired   = "Dialog.SubmoduleAdd.Hint.PathRequired"
	hintPathInvalid    = "Dialog.SubmoduleAdd.Hint.PathInvalid"
	hintPathTaken      = "Dialog.SubmoduleAdd.Hint.PathTaken"
	hintReady          = "Dialog.SubmoduleAdd.Hint.Ready"
	hintReadyBranch    = "Dialog.SubmoduleAdd.Hint.ReadyBranch"
)

type Known struct {
	Paths []string
}

type Hint struct {
	Key  string
	Args []any
	OK   bool
}

type Model struct {
	URL    string
	Path   string
	Branch string
}

func Validate(model Model, known Known) Hint {
	url := strings.TrimSpace(model.URL)
	switch {
	case url == "":
		return Hint{Key: hintURLRequired}
	case !submodule.IsRelativeURL(url) && !os.IsPathSeparator(url[0]) && !strings.Contains(url, ":"):
		return Hint{Key: hintURLNotAbsolute, Args: []any{url}}
	}
	target := strings.TrimRight(filepath.ToSlash(strings.TrimSpace(model.Path)), "/")
	if target == "" {
		guessed, err := submodule.URLBasename(url)
		if err != nil {
			return Hint{Key: hintPathRequired, Args: []any{url}}
		}
		target = guessed
	}
	clean := path.Clean(target)
	switch {
	case clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/"):
		return Hint{Key: hintPathInvalid, Args: []any{target}}
	case slices.Contains(known.Paths, clean):
		return Hint{Key: hintPathTaken, Args: []any{clean}}
	}
	if branch := strings.TrimSpace(model.Branch); branch != "" {
		return Hint{Key: hintReadyBranch, Args: []any{url, clean, branch}, OK: true}
	}
	return Hint{Key: hintReady, Args: []any{url, clean}, OK: true}
}

type enterModal struct {
	*widget.Dialog
	view *View
}

func (m *enterModal) HandleInputBinding(code widget.KeyCode, mod widget.KeyMod) bool {
	if code == widget.KeyEnter {
		m.view.confirm()
		return true
	}
	return m.Dialog.HandleInputBinding(code, mod)
}

type View struct {
	dlg         *widget.Dialog
	modal       *enterModal
	urlLabel    *widget.Label
	urlBox      *widget.TextInput
	pathLabel   *widget.Label
	pathBox     *widget.TextInput
	branchLabel *widget.Label
	branchBox   *widget.TextInput
	hintLabel   *widget.Label
	okBtn       *widget.Button
	cancelBtn   *widget.Button

	known   Known
	current Hint

	OnOK     func(Model)
	OnCancel func()
}

func NewView() (*View, error) {
	dlg, named, err := loadDialog(dialogName, i18n.T("Dialog.SubmoduleAdd.Title"))
	if err != nil {
		return nil, err
	}
	v := &View{dlg: dlg}
	if err := v.bind(named); err != nil {
		return nil, err
	}
	v.modal = &enterModal{Dialog: dlg, view: v}
	v.hintLabel.Muted = true
	v.wire()
	v.refresh()
	return v, nil
}

func (v *View) Dialog() *widget.Dialog { return v.dlg }

func (v *View) Modal() widget.ModalWidget { return v.modal }

func (v *View) Restyle(t *widget.Theme) {
	p := style.Of(t)
	p.Fields(v.urlBox, v.pathBox, v.branchBox)
	p.Quiet(v.cancelBtn)
	p.Primary(v.okBtn)
	p.Body(v.urlLabel, v.pathLabel, v.branchLabel)
	p.Hints(v.hintLabel)
}

func (v *View) bind(named map[string]widget.Widget) error {
	labels := map[string]**widget.Label{"urlLabel": &v.urlLabel, "pathLabel": &v.pathLabel, "branchLabel": &v.branchLabel, "hint": &v.hintLabel}
	for name, target := range labels {
		label, ok := named[name].(*widget.Label)
		if !ok {
			return fmt.Errorf("%w: %s", ErrWidgetMissing, name)
		}
		*target = label
	}
	inputs := map[string]**widget.TextInput{"url": &v.urlBox, "path": &v.pathBox, "branch": &v.branchBox}
	for name, target := range inputs {
		input, ok := named[name].(*widget.TextInput)
		if !ok {
			return fmt.Errorf("%w: %s", ErrWidgetMissing, name)
		}
		*target = input
	}
	buttons := map[string]**widget.Button{"ok": &v.okBtn, "cancel": &v.cancelBtn}
	for name, target := range buttons {
		button, ok := named[name].(*widget.Button)
		if !ok {
			return fmt.Errorf("%w: %s", ErrWidgetMissing, name)
		}
		*target = button
	}
	return nil
}

func (v *View) wire() {
	for _, input := range []*widget.TextInput{v.urlBox, v.pathBox, v.branchBox} {
		input.OnChange = func(string) { v.refresh() }
	}
	v.okBtn.OnClick = v.confirm
	v.cancelBtn.OnClick = v.cancel
	v.dlg.CancelAction = v.cancel
}

func (v *View) SetKnown(known Known) {
	v.known = known
	v.refresh()
}

func (v *View) Model() Model {
	return Model{
		URL:    strings.TrimSpace(v.urlBox.GetText()),
		Path:   strings.TrimSpace(v.pathBox.GetText()),
		Branch: strings.TrimSpace(v.branchBox.GetText()),
	}
}

func (v *View) refresh() {
	v.current = Validate(v.Model(), v.known)
	v.hintLabel.SetText(i18n.Tf(v.current.Key, v.current.Args...))
	v.okBtn.SetEnabled(v.current.OK)
}

func (v *View) confirm() {
	if !v.current.OK {
		return
	}
	if v.OnOK != nil {
		v.OnOK(v.Model())
	}
}

func (v *View) cancel() {
	if v.OnCancel != nil {
		v.OnCancel()
	}
}
