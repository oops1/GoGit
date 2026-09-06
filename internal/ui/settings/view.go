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

var credentialSourceOrder = []string{
	config.CredentialSourceVault,
	config.CredentialSourceVaultThenHelper,
	config.CredentialSourceHelper,
}

type View struct {
	dlg *widget.Dialog

	root         *widget.Grid
	sectionArea  *widget.Grid
	sectionHost  *widget.Grid
	scroll       *sectionScroller
	search       *widget.TextInput
	sectionTitle *widget.Label

	sectionGeneral     *widget.Grid
	sectionGit         *widget.Grid
	sectionCredentials *widget.Grid
	sectionSSH         *widget.Grid

	nav     *navPanel
	section string
	initial Model

	unsavedDialog *widget.Dialog

	language              *widget.Dropdown
	theme                 *widget.Dropdown
	showToolbar           *widget.CheckBox
	toolbarCaptions       *widget.CheckBox
	showStatusBar         *widget.CheckBox
	journalFullAuthorName *widget.CheckBox
	logMaxCount           *widget.NumericUpDown
	autoFetch             *widget.CheckBox
	fetchInterval         *widget.NumericUpDown
	workTreeDepth         *widget.NumericUpDown
	pullStrategy          *widget.TextInput
	defaultRemote         *widget.TextInput
	pruneOnFetch          *widget.CheckBox
	shallowDepth          *widget.NumericUpDown
	okBtn                 *widget.Button
	cancelBtn             *widget.Button

	gitAdvanced        *widget.Expander
	gitAdvancedContent *widget.Grid

	credentialForm        *widget.Expander
	credentialFormContent *widget.Grid
	sshForm               *widget.Expander
	sshFormContent        *widget.Grid

	credentialSource              *widget.Dropdown
	credentialSourceStorePath     *widget.Label
	credentialSourceKeyProtection *widget.Label
	credentialSourceHelpers       *widget.Label

	credentialsTable      *widget.DataGridWidget
	credentialResource    *widget.TextInput
	credentialUsername    *widget.TextInput
	credentialType        *widget.Dropdown
	credentialSecret      *widget.TextInput
	credentialAddBtn      *widget.Button
	credentialEditBtn     *widget.Button
	credentialRemoveBtn   *widget.Button
	credentialTestBtn     *widget.Button
	masterPasswordBtn     *widget.Button
	credentialsStatus     *widget.Label
	credentialsUnlockBtn  *widget.Button
	credentialsLockIcon   *widget.ImageWidget
	credentialsSecureNote *widget.Label

	sshTable              *widget.DataGridWidget
	sshHostInput          *widget.TextInput
	sshPathInput          *widget.TextInput
	sshPassphraseInput    *widget.TextInput
	sshUseDefaultCheckBox *widget.CheckBox
	sshAddBtn             *widget.Button
	sshEditBtn            *widget.Button
	sshBrowseBtn          *widget.Button
	sshRemoveBtn          *widget.Button
	sshTestBtn            *widget.Button
	sshStatus             *widget.Label
	sshUnlockBtn          *widget.Button
	sshLockIcon           *widget.ImageWidget
	sshSecureNote         *widget.Label

	credentials      []SecretEntry
	keys             []KeyEntry
	selectedResource string
	hasCredSelection bool
	selectedHost     string
	hasKeySelection  bool
	secretsLocked    bool

	searchFields                 []searchField
	searchGroups                 []searchGroupSpec
	searchSections               []*searchSectionState
	searchAdvancedOriginalRows   []widget.GridDefinition
	searchActive                 bool
	searchAdvancedExpandedBefore bool

	eng       widget.ModalShower
	languages []string

	OnOK     func(Model)
	OnCancel func()

	OnAddCredential     func(resource, username string, secret []byte)
	OnRemoveCredential  func(resource string)
	OnAddKey            func(host, path string, passphrase []byte)
	OnRemoveKey         func(host string)
	OnSetMasterPassword func()
	OnUnlockSecrets     func()
	OnBrowseKeyFile     func()
	OnTestConnection    func(section string)
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
	v.attachContent()
	v.populateLanguages()
	norm := initial.Normalized()
	v.apply(norm)
	v.initial = norm
	v.wire()
	v.buildSecretsColumns()
	v.wireSecrets()
	v.buildSecretNotes()
	v.SetSecretsLocked(false)
	v.attachScroll()
	v.buildNav()
	v.search.LeadingIcon = buildSearchIcon(widget.CurrentTheme().InputPlaceholder)
	v.wireModifiedTracking()
	v.refreshSaveEnabled()
	v.buildHints()
	v.wireExpanders()
	v.buildSearchIndex()
	v.buildSearchSections()
	v.search.OnChange = v.applySearch
	return v, nil
}

func (v *View) Dialog() *widget.Dialog { return v.dlg }

func (v *View) bind(named map[string]widget.Widget) error {
	var ok bool
	if v.root, ok = named["root"].(*widget.Grid); !ok {
		return fmt.Errorf("%w: root", ErrWidgetMissing)
	}
	if v.search, ok = named["search"].(*widget.TextInput); !ok {
		return fmt.Errorf("%w: search", ErrWidgetMissing)
	}
	if v.sectionTitle, ok = named["sectionTitle"].(*widget.Label); !ok {
		return fmt.Errorf("%w: sectionTitle", ErrWidgetMissing)
	}
	if v.sectionArea, ok = named["sectionArea"].(*widget.Grid); !ok {
		return fmt.Errorf("%w: sectionArea", ErrWidgetMissing)
	}
	if v.sectionHost, ok = named["sectionHost"].(*widget.Grid); !ok {
		return fmt.Errorf("%w: sectionHost", ErrWidgetMissing)
	}
	if v.sectionGeneral, ok = named["sectionGeneral"].(*widget.Grid); !ok {
		return fmt.Errorf("%w: sectionGeneral", ErrWidgetMissing)
	}
	if v.sectionGit, ok = named["sectionGit"].(*widget.Grid); !ok {
		return fmt.Errorf("%w: sectionGit", ErrWidgetMissing)
	}
	if v.sectionCredentials, ok = named["sectionCredentials"].(*widget.Grid); !ok {
		return fmt.Errorf("%w: sectionCredentials", ErrWidgetMissing)
	}
	if v.sectionSSH, ok = named["sectionSSH"].(*widget.Grid); !ok {
		return fmt.Errorf("%w: sectionSSH", ErrWidgetMissing)
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
	if v.toolbarCaptions, ok = named["toolbarCaptions"].(*widget.CheckBox); !ok {
		return fmt.Errorf("%w: toolbarCaptions", ErrWidgetMissing)
	}
	if v.showStatusBar, ok = named["showStatusBar"].(*widget.CheckBox); !ok {
		return fmt.Errorf("%w: showStatusBar", ErrWidgetMissing)
	}
	if v.journalFullAuthorName, ok = named["journalFullAuthorName"].(*widget.CheckBox); !ok {
		return fmt.Errorf("%w: journalFullAuthorName", ErrWidgetMissing)
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
	if v.workTreeDepth, ok = named["workTreeDepth"].(*widget.NumericUpDown); !ok {
		return fmt.Errorf("%w: workTreeDepth", ErrWidgetMissing)
	}
	if v.pullStrategy, ok = named["pullStrategy"].(*widget.TextInput); !ok {
		return fmt.Errorf("%w: pullStrategy", ErrWidgetMissing)
	}
	if v.defaultRemote, ok = named["defaultRemote"].(*widget.TextInput); !ok {
		return fmt.Errorf("%w: defaultRemote", ErrWidgetMissing)
	}
	if v.pruneOnFetch, ok = named["pruneOnFetch"].(*widget.CheckBox); !ok {
		return fmt.Errorf("%w: pruneOnFetch", ErrWidgetMissing)
	}
	if v.shallowDepth, ok = named["shallowDepth"].(*widget.NumericUpDown); !ok {
		return fmt.Errorf("%w: shallowDepth", ErrWidgetMissing)
	}
	if v.gitAdvanced, ok = named["gitAdvanced"].(*widget.Expander); !ok {
		return fmt.Errorf("%w: gitAdvanced", ErrWidgetMissing)
	}
	if v.gitAdvancedContent, ok = named["gitAdvancedContent"].(*widget.Grid); !ok {
		return fmt.Errorf("%w: gitAdvancedContent", ErrWidgetMissing)
	}
	if v.credentialForm, ok = named["credentialForm"].(*widget.Expander); !ok {
		return fmt.Errorf("%w: credentialForm", ErrWidgetMissing)
	}
	if v.credentialFormContent, ok = named["credentialFormContent"].(*widget.Grid); !ok {
		return fmt.Errorf("%w: credentialFormContent", ErrWidgetMissing)
	}
	if v.sshForm, ok = named["sshForm"].(*widget.Expander); !ok {
		return fmt.Errorf("%w: sshForm", ErrWidgetMissing)
	}
	if v.sshFormContent, ok = named["sshFormContent"].(*widget.Grid); !ok {
		return fmt.Errorf("%w: sshFormContent", ErrWidgetMissing)
	}
	if v.okBtn, ok = named["ok"].(*widget.Button); !ok {
		return fmt.Errorf("%w: ok", ErrWidgetMissing)
	}
	if v.cancelBtn, ok = named["cancel"].(*widget.Button); !ok {
		return fmt.Errorf("%w: cancel", ErrWidgetMissing)
	}
	if v.credentialSource, ok = named["credentialSource"].(*widget.Dropdown); !ok {
		return fmt.Errorf("%w: credentialSource", ErrWidgetMissing)
	}
	if v.credentialSourceStorePath, ok = named["credentialSourceStorePath"].(*widget.Label); !ok {
		return fmt.Errorf("%w: credentialSourceStorePath", ErrWidgetMissing)
	}
	if v.credentialSourceKeyProtection, ok = named["credentialSourceKeyProtection"].(*widget.Label); !ok {
		return fmt.Errorf("%w: credentialSourceKeyProtection", ErrWidgetMissing)
	}
	if v.credentialSourceHelpers, ok = named["credentialSourceHelpers"].(*widget.Label); !ok {
		return fmt.Errorf("%w: credentialSourceHelpers", ErrWidgetMissing)
	}
	return v.bindSecrets(named)
}

func (v *View) attachContent() {
	v.dlg.RemoveChild(v.root)
	v.root.RemoveChild(v.search)
	v.dlg.SetContent(v.root)
	v.dlg.SetResizable(true)
	v.dlg.SetMinSize(dialogMinWidth, dialogMinHeight)
	v.dlg.SetTitleBarContent(v.search)
	v.dlg.SetWindowButtons(true)
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
	v.toolbarCaptions.SetChecked(m.ToolbarCaptions)
	v.showStatusBar.SetChecked(m.ShowStatusBar)
	v.journalFullAuthorName.SetChecked(m.JournalFullAuthorName)
	v.logMaxCount.SetValue(float64(m.LogMaxCount))
	v.autoFetch.SetChecked(m.AutoFetch)
	v.fetchInterval.SetValue(float64(m.FetchInterval))
	v.workTreeDepth.SetValue(float64(m.WorkTreeDepth))
	v.pullStrategy.SetText(m.PullStrategy)
	v.defaultRemote.SetText(m.DefaultRemote)
	v.pruneOnFetch.SetChecked(m.PruneOnFetch)
	v.shallowDepth.SetValue(float64(m.ShallowDepth))
	v.credentialSource.SetSelected(credentialSourceIndex(m.CredentialSource))
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

func credentialSourceIndex(source string) int {
	for i, s := range credentialSourceOrder {
		if s == source {
			return i
		}
	}
	return 0
}

func credentialSourceAt(idx int) string {
	if idx < 0 || idx >= len(credentialSourceOrder) {
		return config.CredentialSourceVault
	}
	return credentialSourceOrder[idx]
}

func (v *View) request() Model {
	code := ""
	if idx := v.language.Selected(); idx >= 0 && idx < len(v.languages) {
		code = v.languages[idx]
	}
	return Model{
		Language:              code,
		Theme:                 themeAt(v.theme.Selected()),
		ShowToolbar:           v.showToolbar.IsChecked(),
		ToolbarCaptions:       v.toolbarCaptions.IsChecked(),
		ShowStatusBar:         v.showStatusBar.IsChecked(),
		JournalFullAuthorName: v.journalFullAuthorName.IsChecked(),
		LogMaxCount:           int(v.logMaxCount.Value()),
		AutoFetch:             v.autoFetch.IsChecked(),
		FetchInterval:         int(v.fetchInterval.Value()),
		WorkTreeDepth:         int(v.workTreeDepth.Value()),
		PullStrategy:          v.pullStrategy.GetText(),
		DefaultRemote:         v.defaultRemote.GetText(),
		PruneOnFetch:          v.pruneOnFetch.IsChecked(),
		ShallowDepth:          int(v.shallowDepth.Value()),
		CredentialSource:      credentialSourceAt(v.credentialSource.Selected()),
	}.Normalized()
}

func (v *View) wire() {
	v.okBtn.OnClick = v.confirm
	v.cancelBtn.OnClick = v.cancel
	v.dlg.DefaultAction = v.confirm
	v.dlg.CancelAction = v.doCancel
	v.dlg.OnClosing = v.allowClose
}

func (v *View) allowClose() bool {
	if !v.Modified() {
		return true
	}
	v.confirmUnsavedChanges()
	return false
}

func (v *View) confirm() {
	if v.OnOK != nil {
		v.OnOK(v.request())
	}
}

func (v *View) cancel() {
	if v.Modified() {
		v.confirmUnsavedChanges()
		return
	}
	v.doCancel()
}

func (v *View) doCancel() {
	if v.OnCancel != nil {
		v.OnCancel()
	}
}
