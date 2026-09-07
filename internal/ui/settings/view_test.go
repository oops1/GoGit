package settings

import (
	"errors"
	"image/color"
	"testing"

	"github.com/oops1/headless-gui/v3/engine"
	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/config"
	"github.com/oops1/gogit/internal/i18n"
)

func newTestView(t *testing.T, languages []string, initial Model) *View {
	t.Helper()
	widget.ClearStrings()
	t.Cleanup(widget.ClearStrings)
	if _, err := i18n.Install(""); err != nil {
		t.Fatal(err)
	}
	i18n.Apply("en")
	eng := engine.New(800, 600, 30)
	v, err := NewView(eng, languages, initial)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func clickCheckBox(cb *widget.CheckBox) {
	cb.OnMouseButton(widget.MouseEvent{Button: widget.MouseLeft, Pressed: true})
	cb.OnMouseButton(widget.MouseEvent{Button: widget.MouseLeft, Pressed: false})
}

func fullNamedWidgets() map[string]widget.Widget {
	return map[string]widget.Widget{
		"root":               widget.NewGrid(),
		"search":             widget.NewTextInput(""),
		"sectionArea":        widget.NewGrid(),
		"sectionHost":        widget.NewGrid(),
		"navTitle":           widget.NewLabel("", color.RGBA{}),
		"navToggle":          widget.NewButton(""),
		"sectionTitle":       widget.NewWin10Label(""),
		"sectionGeneral":     widget.NewGrid(),
		"sectionGit":         widget.NewGrid(),
		"sectionCredentials": widget.NewGrid(),
		"sectionSSH":         widget.NewGrid(),

		"language":              widget.NewDropdown(),
		"theme":                 widget.NewDropdown(),
		"showToolbar":           widget.NewCheckBox(""),
		"toolbarCaptions":       widget.NewCheckBox(""),
		"showStatusBar":         widget.NewCheckBox(""),
		"journalFullAuthorName": widget.NewCheckBox(""),
		"logMaxCount":           widget.NewNumericUpDown(),
		"autoFetch":             widget.NewCheckBox(""),
		"fetchInterval":         widget.NewNumericUpDown(),
		"workTreeDepth":         widget.NewNumericUpDown(),
		"pullStrategy":          widget.NewTextInput(""),
		"defaultRemote":         widget.NewTextInput(""),
		"pruneOnFetch":          widget.NewCheckBox(""),
		"shallowDepth":          widget.NewNumericUpDown(),
		"gitAdvanced":           widget.NewExpander(""),
		"gitAdvancedContent":    widget.NewGrid(),
		"ok":                    widget.NewButton(""),
		"cancel":                widget.NewButton(""),

		"credentialSource":              widget.NewDropdown(),
		"credentialSourceStorePath":     widget.NewWin10Label(""),
		"credentialSourceKeyProtection": widget.NewWin10Label(""),
		"credentialSourceHelpers":       widget.NewWin10Label(""),

		"credentialsTable":         widget.NewDataGridWidget(),
		"credentialResource":       widget.NewTextInput(""),
		"credentialUsername":       widget.NewTextInput(""),
		"credentialType":           widget.NewDropdown(),
		"credentialSecret":         widget.NewPasswordInput(""),
		"credentialSave":           widget.NewButton(""),
		"sshSave":                  widget.NewButton(""),
		"credentialAdd":            widget.NewButton(""),
		"credentialEditStatus":     widget.NewWin10Label(""),
		"credentialEditOK":         widget.NewButton(""),
		"credentialEditCancel":     widget.NewButton(""),
		"sshEditStatus":            widget.NewWin10Label(""),
		"sshEditOK":                widget.NewButton(""),
		"sshEditCancel":            widget.NewButton(""),
		"credentialEdit":           widget.NewButton(""),
		"credentialRemove":         widget.NewButton(""),
		"credentialTestConnection": widget.NewButton(""),
		"credentialMasterPassword": widget.NewButton(""),
		"credentialsStatus":        widget.NewWin10Label(""),
		"credentialsUnlock":        widget.NewButton(""),
		"credentialsLockIcon":      widget.NewImageWidget(),
		"credentialsSecureRow":     widget.NewGrid(),

		"sshTable":          widget.NewDataGridWidget(),
		"sshHost":           widget.NewTextInput(""),
		"sshPath":           widget.NewTextInput(""),
		"sshBrowse":         widget.NewButton(""),
		"sshPassphrase":     widget.NewPasswordInput(""),
		"sshUseDefault":     widget.NewCheckBox(""),
		"sshAdd":            widget.NewButton(""),
		"sshEdit":           widget.NewButton(""),
		"sshRemove":         widget.NewButton(""),
		"sshTestConnection": widget.NewButton(""),
		"sshStatus":         widget.NewWin10Label(""),
		"sshUnlock":         widget.NewButton(""),
		"sshLockIcon":       widget.NewImageWidget(),
		"sshSecureRow":      widget.NewGrid(),
	}
}

func TestNewViewPropagatesLoadDialogError(t *testing.T) {
	widget.ClearStrings()
	defer widget.ClearStrings()
	prev := loadDialog
	wantErr := errors.New("boom")
	loadDialog = func(name, title string) (*widget.Dialog, map[string]widget.Widget, error) {
		return nil, nil, wantErr
	}
	defer func() { loadDialog = prev }()

	eng := engine.New(800, 600, 30)
	if _, err := NewView(eng, nil, Model{}); !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
}

func TestNewViewPropagatesBindError(t *testing.T) {
	widget.ClearStrings()
	defer widget.ClearStrings()
	prev := loadDialog
	loadDialog = func(name, title string) (*widget.Dialog, map[string]widget.Widget, error) {
		return widget.NewDialog(title, 10, 10), map[string]widget.Widget{}, nil
	}
	defer func() { loadDialog = prev }()

	eng := engine.New(800, 600, 30)
	if _, err := NewView(eng, nil, Model{}); !errors.Is(err, ErrWidgetMissing) {
		t.Fatalf("err = %v, want %v", err, ErrWidgetMissing)
	}
}

func TestBindReturnsErrorForEachMissingOrMistypedWidget(t *testing.T) {
	keys := []string{
		"root", "search", "sectionArea", "sectionHost", "sectionTitle", "sectionGeneral", "sectionGit", "sectionCredentials", "sectionSSH",
		"language", "theme", "showToolbar", "toolbarCaptions", "showStatusBar", "journalFullAuthorName",
		"logMaxCount", "autoFetch", "fetchInterval", "workTreeDepth", "pullStrategy", "defaultRemote", "pruneOnFetch",
		"shallowDepth", "gitAdvanced", "gitAdvancedContent", "ok", "cancel",
		"credentialSource", "credentialSourceStorePath", "credentialSourceKeyProtection", "credentialSourceHelpers",
		"credentialsTable", "credentialResource", "credentialUsername", "credentialType", "credentialSecret",
		"credentialAdd", "credentialEditStatus", "credentialEditOK", "credentialEditCancel",
		"sshEditStatus", "sshEditOK", "sshEditCancel", "credentialEdit", "credentialRemove", "credentialTestConnection", "credentialMasterPassword",
		"credentialsStatus", "credentialsUnlock", "credentialsLockIcon", "credentialsSecureRow",
		"sshTable", "sshHost", "sshPath", "sshBrowse", "sshPassphrase", "sshUseDefault", "sshAdd", "sshEdit",
		"sshRemove", "sshTestConnection", "sshStatus", "sshUnlock", "sshLockIcon", "sshSecureRow",
	}
	for _, key := range keys {
		named := fullNamedWidgets()
		delete(named, key)
		v := &View{}
		if err := v.bind(named); err == nil {
			t.Fatalf("missing %q: expected error", key)
		}
	}
	labelKeys := map[string]bool{
		"credentialSourceStorePath": true, "credentialSourceKeyProtection": true, "credentialSourceHelpers": true,
		"credentialsStatus": true, "sshStatus": true, "sectionTitle": true,
		"credentialEditStatus": true, "sshEditStatus": true,
	}
	for _, key := range keys {
		if labelKeys[key] {
			continue
		}
		named := fullNamedWidgets()
		named[key] = widget.NewWin10Label("wrong-type")
		v := &View{}
		if err := v.bind(named); err == nil {
			t.Fatalf("mistyped %q: expected error", key)
		}
	}
	for key := range labelKeys {
		named := fullNamedWidgets()
		named[key] = widget.NewButton("wrong-type")
		v := &View{}
		if err := v.bind(named); err == nil {
			t.Fatalf("mistyped %q: expected error", key)
		}
	}
}

func TestBindSucceedsWithAllWidgetsPresent(t *testing.T) {
	v := &View{}
	if err := v.bind(fullNamedWidgets()); err != nil {
		t.Fatal(err)
	}
}

func TestNewViewAppliesInitialModelToWidgets(t *testing.T) {
	initial := Model{
		Language:              "ru",
		Theme:                 config.ThemeDark,
		ShowToolbar:           false,
		ShowStatusBar:         true,
		JournalFullAuthorName: true,
		LogMaxCount:           777,
		AutoFetch:             true,
		FetchInterval:         90,
		WorkTreeDepth:         6,
		PullStrategy:          config.PullStrategyMerge,
		DefaultRemote:         "upstream",
		PruneOnFetch:          true,
		ShallowDepth:          15,
		CredentialSource:      config.CredentialSourceHelper,
	}
	v := newTestView(t, []string{"en", "ru"}, initial)

	if v.language.Selected() != 1 {
		t.Fatalf("language selection = %d, want 1", v.language.Selected())
	}
	if v.theme.Selected() != themeIndex(config.ThemeDark) {
		t.Fatalf("theme selection = %d", v.theme.Selected())
	}
	if v.showToolbar.IsChecked() {
		t.Fatal("show toolbar must be unchecked")
	}
	if !v.showStatusBar.IsChecked() {
		t.Fatal("show status bar must be checked")
	}
	if !v.journalFullAuthorName.IsChecked() {
		t.Fatal("journal full author name must be checked")
	}
	if v.logMaxCount.Value() != 777 {
		t.Fatalf("logMaxCount = %v", v.logMaxCount.Value())
	}
	if !v.autoFetch.IsChecked() {
		t.Fatal("auto-fetch must be checked")
	}
	if v.fetchInterval.Value() != 90 {
		t.Fatalf("fetchInterval = %v", v.fetchInterval.Value())
	}
	if v.workTreeDepth.Value() != 6 {
		t.Fatalf("workTreeDepth = %v", v.workTreeDepth.Value())
	}
	if v.pullStrategy.GetText() != config.PullStrategyMerge {
		t.Fatalf("pullStrategy = %q", v.pullStrategy.GetText())
	}
	if v.defaultRemote.GetText() != "upstream" {
		t.Fatalf("defaultRemote = %q", v.defaultRemote.GetText())
	}
	if !v.pruneOnFetch.IsChecked() {
		t.Fatal("pruneOnFetch must be checked")
	}
	if v.shallowDepth.Value() != 15 {
		t.Fatalf("shallowDepth = %v", v.shallowDepth.Value())
	}
	if v.credentialSource.Selected() != credentialSourceIndex(config.CredentialSourceHelper) {
		t.Fatalf("credentialSource selection = %d", v.credentialSource.Selected())
	}
}

func TestPopulateLanguagesUsesLocalizedLabelsAndFallsBackToCode(t *testing.T) {
	v := newTestView(t, []string{"en", "xx"}, Model{})
	items := v.language.Items()
	if len(items) != 2 || items[0] != "English" || items[1] != "xx" {
		t.Fatalf("language items = %v", items)
	}
}

func TestSetLanguageSelectionFallsBackWhenCodeNotInList(t *testing.T) {
	v := newTestView(t, []string{"en", "ru"}, Model{Language: "de"})
	if v.language.Selected() != 0 {
		t.Fatalf("selection = %d, want 0", v.language.Selected())
	}
}

func TestThemeIndexAndThemeAtRoundTripKnownValues(t *testing.T) {
	for i, theme := range themeOrder {
		if themeIndex(theme) != i {
			t.Fatalf("themeIndex(%q) = %d, want %d", theme, themeIndex(theme), i)
		}
		if themeAt(i) != theme {
			t.Fatalf("themeAt(%d) = %q, want %q", i, themeAt(i), theme)
		}
	}
}

func TestThemeIndexFallsBackToZeroForUnknownTheme(t *testing.T) {
	if themeIndex("bogus") != 0 {
		t.Fatal("unknown theme must map to index 0")
	}
}

func TestThemeAtFallsBackToSystemForOutOfRangeIndex(t *testing.T) {
	if themeAt(-1) != config.ThemeSystem {
		t.Fatal("negative index must fall back to system theme")
	}
	if themeAt(len(themeOrder)) != config.ThemeSystem {
		t.Fatal("index past the end must fall back to system theme")
	}
}

func TestCredentialSourceIndexAndAtRoundTripKnownValues(t *testing.T) {
	for i, source := range credentialSourceOrder {
		if credentialSourceIndex(source) != i {
			t.Fatalf("credentialSourceIndex(%q) = %d, want %d", source, credentialSourceIndex(source), i)
		}
		if credentialSourceAt(i) != source {
			t.Fatalf("credentialSourceAt(%d) = %q, want %q", i, credentialSourceAt(i), source)
		}
	}
}

func TestCredentialSourceIndexFallsBackToZeroForUnknownValue(t *testing.T) {
	if credentialSourceIndex("bogus") != 0 {
		t.Fatal("unknown credential source must map to index 0")
	}
}

func TestCredentialSourceAtFallsBackToVaultForOutOfRangeIndex(t *testing.T) {
	if credentialSourceAt(-1) != config.CredentialSourceVault {
		t.Fatal("negative index must fall back to vault")
	}
	if credentialSourceAt(len(credentialSourceOrder)) != config.CredentialSourceVault {
		t.Fatal("index past the end must fall back to vault")
	}
}

func TestRequestReadsCurrentWidgetValues(t *testing.T) {
	initial := Model{
		Language:              "en",
		Theme:                 config.ThemeSystem,
		ShowToolbar:           false,
		ShowStatusBar:         false,
		JournalFullAuthorName: false,
		LogMaxCount:           500,
		AutoFetch:             false,
		FetchInterval:         300,
		WorkTreeDepth:         0,
		PullStrategy:          config.PullStrategyFF,
		DefaultRemote:         "origin",
		PruneOnFetch:          false,
		ShallowDepth:          0,
		CredentialSource:      config.CredentialSourceVault,
	}
	v := newTestView(t, []string{"en", "ru"}, initial)
	v.language.SetSelected(1)
	v.theme.SetSelected(themeIndex(config.ThemeLight))
	clickCheckBox(v.showToolbar)
	clickCheckBox(v.autoFetch)
	clickCheckBox(v.journalFullAuthorName)
	clickCheckBox(v.pruneOnFetch)
	v.logMaxCount.SetValue(1234)
	v.fetchInterval.SetValue(456)
	v.workTreeDepth.SetValue(8)
	v.pullStrategy.SetText(config.PullStrategyRebase)
	v.defaultRemote.SetText("upstream")
	v.shallowDepth.SetValue(15)
	v.credentialSource.SetSelected(credentialSourceIndex(config.CredentialSourceHelper))

	got := v.request()
	want := Model{
		Language:              "ru",
		Theme:                 config.ThemeLight,
		ShowToolbar:           true,
		ShowStatusBar:         false,
		JournalFullAuthorName: true,
		LogMaxCount:           1234,
		AutoFetch:             true,
		FetchInterval:         456,
		WorkTreeDepth:         8,
		PullStrategy:          config.PullStrategyRebase,
		DefaultRemote:         "upstream",
		PruneOnFetch:          true,
		ShallowDepth:          15,
		CredentialSource:      config.CredentialSourceHelper,
	}
	if got != want {
		t.Fatalf("request = %+v, want %+v", got, want)
	}
}

func TestRequestFallsBackToEmptyLanguageWhenNoLanguagesConfigured(t *testing.T) {
	v := newTestView(t, nil, Model{})
	got := v.request()
	if got.Language != "en" {
		t.Fatalf("language = %q, want en (normalized fallback)", got.Language)
	}
}

func TestConfirmInvokesOnOKWithCurrentModel(t *testing.T) {
	v := newTestView(t, []string{"en", "ru"}, Model{Language: "ru", LogMaxCount: 500, FetchInterval: 300})
	var got Model
	called := 0
	v.OnOK = func(m Model) { got = m; called++ }

	v.confirm()

	if called != 1 {
		t.Fatalf("OnOK called = %d, want 1", called)
	}
	if got.Language != "ru" {
		t.Fatalf("model = %+v", got)
	}
}

func TestConfirmToleratesNilOnOK(t *testing.T) {
	v := newTestView(t, []string{"en"}, Model{})
	v.confirm()
}

func TestCancelInvokesOnCancel(t *testing.T) {
	v := newTestView(t, []string{"en"}, Model{})
	called := 0
	v.OnCancel = func() { called++ }
	v.cancel()
	if called != 1 {
		t.Fatal("cancel must invoke OnCancel")
	}
}

func TestCancelToleratesNilOnCancel(t *testing.T) {
	v := newTestView(t, []string{"en"}, Model{})
	v.cancel()
}

func TestDialogWiresDefaultAndCancelActionsToConfirmAndCancel(t *testing.T) {
	v := newTestView(t, []string{"en"}, Model{})

	okCalled, cancelCalled := 0, 0
	v.OnOK = func(Model) { okCalled++ }
	v.OnCancel = func() { cancelCalled++ }

	if !v.dlg.HandleInputBinding(widget.KeyEnter, 0) {
		t.Fatal("dialog must handle Enter")
	}
	if okCalled != 1 {
		t.Fatalf("Enter must confirm, called = %d", okCalled)
	}

	v.dlg.OnCancel()
	if cancelCalled != 1 {
		t.Fatalf("Escape must cancel, called = %d", cancelCalled)
	}
}

func TestDialogTitleUsesLocalizedText(t *testing.T) {
	v := newTestView(t, []string{"en"}, Model{})
	if v.Dialog().Title != i18n.T("Dialog.Settings.Title") {
		t.Fatalf("title = %q", v.Dialog().Title)
	}
}

func TestOKAndCancelButtonsInvokeConfirmAndCancel(t *testing.T) {
	v := newTestView(t, []string{"en"}, Model{})
	okCalled, cancelCalled := 0, 0
	v.OnOK = func(Model) { okCalled++ }
	v.OnCancel = func() { cancelCalled++ }

	v.okBtn.OnClick()
	if okCalled != 1 {
		t.Fatal("ok button must invoke confirm")
	}
	v.cancelBtn.OnClick()
	if cancelCalled != 1 {
		t.Fatal("cancel button must invoke cancel")
	}
}

func TestCheckboxTogglesWhenTheLabelBesideItIsClicked(t *testing.T) {
	v := newTestView(t, []string{"en", "ru"}, Model{Language: "en"})
	box := v.showToolbar
	before := box.IsChecked()
	b := box.Bounds()
	x := b.Min.X + b.Dx()/2
	y := b.Min.Y + b.Dy()/2
	box.OnMouseButton(widget.MouseEvent{X: x, Y: y, Button: widget.MouseLeft, Pressed: true})
	box.OnMouseButton(widget.MouseEvent{X: x, Y: y, Button: widget.MouseLeft})
	if box.IsChecked() == before {
		t.Fatal("checkbox did not toggle from a click over the area its label occupies")
	}
}

func TestNewViewPropagatesAFailureLoadingTheEditors(t *testing.T) {
	for _, failing := range []string{credentialEditorName, keyEditorName} {
		widget.ClearStrings()
		prev := loadDialog
		wantErr := errors.New("boom")
		loadDialog = func(name, title string) (*widget.Dialog, map[string]widget.Widget, error) {
			if name == failing {
				return nil, nil, wantErr
			}
			return prev(name, title)
		}

		eng := engine.New(800, 600, 30)
		_, err := NewView(eng, nil, Model{})
		loadDialog = prev
		widget.ClearStrings()

		if !errors.Is(err, wantErr) {
			t.Fatalf("loading %q: err = %v, want %v", failing, err, wantErr)
		}
	}
}
