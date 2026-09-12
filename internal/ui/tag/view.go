package tag

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

const dialogName = "tag"

var ErrWidgetMissing = errors.New("tag: named widget missing")

var loadDialog = dialogs.Load

const (
	hintNameRequired = "Dialog.Tag.Hint.NameRequired"
	hintNameTaken    = "Dialog.Tag.Hint.NameTaken"
	hintNameInvalid  = "Dialog.Tag.Hint.NameInvalid"
	hintLightweight  = "Dialog.Tag.Hint.Lightweight"
	hintAnnotated    = "Dialog.Tag.Hint.Annotated"
)

type Known struct {
	Commit string
	Taken  []string
}

type Hint struct {
	Key  string
	Args []any
	OK   bool
}

type Model struct {
	Name    string
	Message string
}

func Validate(model Model, known Known) Hint {
	name := strings.TrimSpace(model.Name)
	switch {
	case name == "":
		return Hint{Key: hintNameRequired}
	case !validName(name):
		return Hint{Key: hintNameInvalid, Args: []any{name}}
	case slices.Contains(known.Taken, name):
		return Hint{Key: hintNameTaken, Args: []any{name}}
	case strings.TrimSpace(model.Message) == "":
		return Hint{Key: hintLightweight, Args: []any{name, known.Commit}, OK: true}
	}
	return Hint{Key: hintAnnotated, Args: []any{name, known.Commit}, OK: true}
}

func validName(name string) bool {
	if strings.Contains(name, "..") || strings.HasPrefix(name, "-") || strings.HasSuffix(name, ".") || strings.HasSuffix(name, ".lock") {
		return false
	}
	return !strings.ContainsAny(name, " ~^:?*[\\\t")
}

type View struct {
	dlg          *widget.Dialog
	nameLabel    *widget.Label
	nameBox      *widget.TextInput
	messageLabel *widget.Label
	messageBox   *widget.TextBox
	hintLabel    *widget.Label
	okBtn        *widget.Button
	cancelBtn    *widget.Button

	known   Known
	current Hint

	OnOK     func(Model)
	OnCancel func()
}

func NewView() (*View, error) {
	dlg, named, err := loadDialog(dialogName, i18n.T("Dialog.Tag.Title"))
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
	p.Fields(v.nameBox)
	p.Areas(v.messageBox)
	p.Quiet(v.cancelBtn)
	p.Primary(v.okBtn)
	p.Body(v.nameLabel, v.messageLabel)
	p.Hints(v.hintLabel)
}

func (v *View) bind(named map[string]widget.Widget) error {
	var ok bool
	if v.nameLabel, ok = named["nameLabel"].(*widget.Label); !ok {
		return fmt.Errorf("%w: nameLabel", ErrWidgetMissing)
	}
	if v.nameBox, ok = named["name"].(*widget.TextInput); !ok {
		return fmt.Errorf("%w: name", ErrWidgetMissing)
	}
	if v.messageLabel, ok = named["messageLabel"].(*widget.Label); !ok {
		return fmt.Errorf("%w: messageLabel", ErrWidgetMissing)
	}
	if v.messageBox, ok = named["message"].(*widget.TextBox); !ok {
		return fmt.Errorf("%w: message", ErrWidgetMissing)
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
	v.nameBox.OnChange = func(string) { v.refresh() }
	v.messageBox.OnChange = func(string) { v.refresh() }
	v.okBtn.OnClick = v.confirm
	v.cancelBtn.OnClick = v.cancel
	v.dlg.CancelAction = v.cancel
}

func (v *View) SetKnown(known Known) {
	v.known = known
	v.nameLabel.SetText(i18n.Tf("Dialog.Tag.Name", known.Commit))
	v.refresh()
}

func (v *View) Model() Model {
	return Model{Name: strings.TrimSpace(v.nameBox.GetText()), Message: v.messageBox.GetText()}
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
