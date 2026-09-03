package settings

import (
	"errors"
	"fmt"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/config"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/dialogs"
)

const dialogName = "settings"

var ErrWidgetMissing = errors.New("settings: named widget missing")

var loadDialog = dialogs.Load

var themeOrder = []string{config.ThemeSystem, config.ThemeDark, config.ThemeLight}

type View struct {
	dlg *widget.Dialog

	tabs          *widget.TabControl
	language      *widget.Dropdown
	theme         *widget.Dropdown
	showToolbar   *widget.CheckBox
	showStatusBar *widget.CheckBox
	logMaxCount   *widget.NumericUpDown
	autoFetch     *widget.CheckBox
	fetchInterval *widget.NumericUpDown
	okBtn         *widget.Button
	cancelBtn     *widget.Button

	eng       widget.ModalShower
	languages []string

	OnOK     func(Model)
	OnCancel func()
}

func NewView(eng widget.ModalShower, languages []string, initial Model) (*View, error) {
	dlg, named, err := loadDialog(dialogName, i18n.T("Dialog.Settings.Title"))
	if err != nil {
		return nil, err
	}
	v := &View{dlg: dlg, eng: eng, languages: languages}
	if err := v.bind(named); err != nil {
		return nil, err
	}
	v.populateLanguages()
	v.apply(initial.Normalized())
	v.wire()
	return v, nil
}

func (v *View) Dialog() *widget.Dialog { return v.dlg }

func (v *View) bind(named map[string]widget.Widget) error {
	var ok bool
	if v.tabs, ok = named["tabs"].(*widget.TabControl); !ok {
		return fmt.Errorf("%w: tabs", ErrWidgetMissing)
	}
	if v.language, ok = named["language"].(*widget.Dropdown); !ok {
		return fmt.Errorf("%w: language", ErrWidgetMissing)
	}
	if v.theme, ok = named["theme"].(*widget.Dropdown); !ok {
		return fmt.Errorf("%w: theme", ErrWidgetMissing)
	}
	if v.showToolbar, ok = named["showToolbar"].(*widget.CheckBox); !ok {
		return fmt.Errorf("%w: showToolbar", ErrWidgetMissing)
	}
	if v.showStatusBar, ok = named["showStatusBar"].(*widget.CheckBox); !ok {
		return fmt.Errorf("%w: showStatusBar", ErrWidgetMissing)
	}
	if v.logMaxCount, ok = named["logMaxCount"].(*widget.NumericUpDown); !ok {
		return fmt.Errorf("%w: logMaxCount", ErrWidgetMissing)
	}
	if v.autoFetch, ok = named["autoFetch"].(*widget.CheckBox); !ok {
		return fmt.Errorf("%w: autoFetch", ErrWidgetMissing)
	}
	if v.fetchInterval, ok = named["fetchInterval"].(*widget.NumericUpDown); !ok {
		return fmt.Errorf("%w: fetchInterval", ErrWidgetMissing)
	}
	if v.okBtn, ok = named["ok"].(*widget.Button); !ok {
		return fmt.Errorf("%w: ok", ErrWidgetMissing)
	}
	if v.cancelBtn, ok = named["cancel"].(*widget.Button); !ok {
		return fmt.Errorf("%w: cancel", ErrWidgetMissing)
	}
	return nil
}

func (v *View) populateLanguages() {
	labels := make([]string, len(v.languages))
	for i, code := range v.languages {
		labels[i] = languageLabel(code)
	}
	v.language.SetItems(labels)
}

func languageLabel(code string) string {
	key := "Language." + code
	if label := i18n.T(key); label != key {
		return label
	}
	return code
}

func (v *View) apply(m Model) {
	v.setLanguageSelection(m.Language)
	v.theme.SetSelected(themeIndex(m.Theme))
	v.showToolbar.SetChecked(m.ShowToolbar)
	v.showStatusBar.SetChecked(m.ShowStatusBar)
	v.logMaxCount.SetValue(float64(m.LogMaxCount))
	v.autoFetch.SetChecked(m.AutoFetch)
	v.fetchInterval.SetValue(float64(m.FetchInterval))
}

func (v *View) setLanguageSelection(code string) {
	for i, c := range v.languages {
		if c == code {
			v.language.SetSelected(i)
			return
		}
	}
}

func themeIndex(theme string) int {
	for i, t := range themeOrder {
		if t == theme {
			return i
		}
	}
	return 0
}

func themeAt(idx int) string {
	if idx < 0 || idx >= len(themeOrder) {
		return config.ThemeSystem
	}
	return themeOrder[idx]
}

func (v *View) request() Model {
	code := ""
	if idx := v.language.Selected(); idx >= 0 && idx < len(v.languages) {
		code = v.languages[idx]
	}
	return Model{
		Language:      code,
		Theme:         themeAt(v.theme.Selected()),
		ShowToolbar:   v.showToolbar.IsChecked(),
		ShowStatusBar: v.showStatusBar.IsChecked(),
		LogMaxCount:   int(v.logMaxCount.Value()),
		AutoFetch:     v.autoFetch.IsChecked(),
		FetchInterval: int(v.fetchInterval.Value()),
	}.Normalized()
}

func (v *View) wire() {
	v.okBtn.OnClick = v.confirm
	v.cancelBtn.OnClick = v.cancel
	v.dlg.DefaultAction = v.confirm
	v.dlg.CancelAction = v.cancel
}

func (v *View) confirm() {
	if v.OnOK != nil {
		v.OnOK(v.request())
	}
}

func (v *View) cancel() {
	if v.OnCancel != nil {
		v.OnCancel()
	}
}
