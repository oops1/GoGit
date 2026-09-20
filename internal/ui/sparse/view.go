package sparse

import (
	"errors"
	"fmt"
	"strings"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/dialogs"
	"github.com/oops1/gogit/internal/ui/style"
)

const dialogName = "sparse_checkout"

var ErrWidgetMissing = errors.New("sparse: named widget missing")

var loadDialog = dialogs.Load

const (
	hintDisabled   = "Dialog.Sparse.Hint.Disabled"
	hintEmpty      = "Dialog.Sparse.Hint.Empty"
	hintCone       = "Dialog.Sparse.Hint.Cone"
	hintPatterns   = "Dialog.Sparse.Hint.Patterns"
	hintGlobInCone = "Dialog.Sparse.Hint.GlobInCone"

	labelCone     = "Dialog.Sparse.ConeLabel"
	labelPatterns = "Dialog.Sparse.PatternsLabel"

	coneGlobCharacters = "*?[]"
)

type Model struct {
	Enabled  bool
	Cone     bool
	Patterns []string
}

type Hint struct {
	Key  string
	Args []any
	OK   bool
}

func Validate(model Model) Hint {
	if !model.Enabled {
		return Hint{Key: hintDisabled, OK: true}
	}
	if len(model.Patterns) == 0 {
		return Hint{Key: hintEmpty, OK: true}
	}
	if model.Cone {
		for _, pattern := range model.Patterns {
			if strings.ContainsAny(pattern, coneGlobCharacters) {
				return Hint{Key: hintGlobInCone, Args: []any{pattern}}
			}
		}
		return Hint{Key: hintCone, Args: []any{len(model.Patterns)}, OK: true}
	}
	return Hint{Key: hintPatterns, Args: []any{len(model.Patterns)}, OK: true}
}

func ParsePatterns(text string) []string {
	var lines []string
	for line := range strings.SplitSeq(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			lines = append(lines, trimmed)
		}
	}
	return lines
}

type View struct {
	dlg           *widget.Dialog
	enabledCheck  *widget.CheckBox
	coneCheck     *widget.CheckBox
	patternsLabel *widget.Label
	patternsBox   *widget.TextBox
	hintLabel     *widget.Label
	okBtn         *widget.Button
	cancelBtn     *widget.Button

	current Hint

	OnOK     func(Model)
	OnCancel func()
}

func NewView(model Model) (*View, error) {
	dlg, named, err := loadDialog(dialogName, i18n.T("Dialog.Sparse.Title"))
	if err != nil {
		return nil, err
	}
	v := &View{dlg: dlg}
	if err := v.bind(named); err != nil {
		return nil, err
	}
	v.hintLabel.Muted = true
	v.apply(model)
	v.wire()
	v.refresh()
	return v, nil
}

func (v *View) Dialog() *widget.Dialog { return v.dlg }

func (v *View) Restyle(t *widget.Theme) {
	p := style.Of(t)
	p.Areas(v.patternsBox)
	p.Quiet(v.cancelBtn)
	p.Primary(v.okBtn)
	p.Body(v.patternsLabel)
	p.Hints(v.hintLabel)
}

func (v *View) bind(named map[string]widget.Widget) error {
	var ok bool
	if v.enabledCheck, ok = named["enabled"].(*widget.CheckBox); !ok {
		return fmt.Errorf("%w: enabled", ErrWidgetMissing)
	}
	if v.coneCheck, ok = named["cone"].(*widget.CheckBox); !ok {
		return fmt.Errorf("%w: cone", ErrWidgetMissing)
	}
	if v.patternsLabel, ok = named["patternsLabel"].(*widget.Label); !ok {
		return fmt.Errorf("%w: patternsLabel", ErrWidgetMissing)
	}
	if v.patternsBox, ok = named["patterns"].(*widget.TextBox); !ok {
		return fmt.Errorf("%w: patterns", ErrWidgetMissing)
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

func (v *View) apply(model Model) {
	v.enabledCheck.SetChecked(model.Enabled)
	v.coneCheck.SetChecked(model.Cone)
	v.patternsBox.SetText(strings.Join(model.Patterns, "\n"))
}

func (v *View) wire() {
	v.enabledCheck.OnChange = func(bool) { v.refresh() }
	v.coneCheck.OnChange = func(bool) { v.refresh() }
	v.patternsBox.OnChange = func(string) { v.refresh() }
	v.okBtn.OnClick = v.confirm
	v.cancelBtn.OnClick = v.cancel
	v.dlg.CancelAction = v.cancel
}

func (v *View) Model() Model {
	return Model{
		Enabled:  v.enabledCheck.IsChecked(),
		Cone:     v.coneCheck.IsChecked(),
		Patterns: ParsePatterns(v.patternsBox.GetText()),
	}
}

func (v *View) refresh() {
	model := v.Model()
	v.coneCheck.SetEnabled(model.Enabled)
	v.patternsBox.SetEnabled(model.Enabled)
	v.patternsLabel.SetText(i18n.T(patternsLabelKey(model.Cone)))
	v.current = Validate(model)
	v.hintLabel.SetText(i18n.Tf(v.current.Key, v.current.Args...))
	v.okBtn.SetEnabled(v.current.OK)
}

func patternsLabelKey(cone bool) string {
	if cone {
		return labelCone
	}
	return labelPatterns
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
