package app

import (
	"slices"
	"strings"
	"testing"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/config"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/dialogs/toolbar"
	"github.com/oops1/gogit/internal/ui/settings"
)

func toolbarItemNamed(t *testing.T, a *App, name string) toolbarButton {
	t.Helper()
	for _, item := range a.toolbarButtons {
		if item.entry.Name == name {
			return item
		}
	}
	t.Fatalf("the toolbar has no button %q", name)
	return toolbarButton{}
}

func toolbarButtonNamed(t *testing.T, a *App, name string) *widget.Button {
	t.Helper()
	return toolbarItemNamed(t, a, name).button()
}

func toolbarItemIDs(a *App) []string {
	panel, _ := a.toolbarPanel()
	ids := make([]string, 0, len(panel.Children()))
	for _, child := range panel.Children() {
		switch child.(type) {
		case *toolbarSeparator:
			ids = append(ids, toolbar.SeparatorID)
		case *toolbarStretch:
			ids = append(ids, toolbar.StretchID)
		default:
			for _, item := range a.toolbarButtons {
				if item.widget() == child {
					ids = append(ids, item.entry.ID)
				}
			}
		}
	}
	return ids
}

func TestTheDefaultToolbarRepeatsTheSmartGitRow(t *testing.T) {
	a := newTestApp(t)
	if got := toolbarItemIDs(a); !slices.Equal(got, defaultToolbarItems()) {
		t.Fatalf("toolbar = %v, want %v", got, defaultToolbarItems())
	}
	groups := [][]string{
		{string(CmdPull), string(CmdSync), string(CmdPush)},
		{string(CmdCommit)},
		{string(CmdStage), string(CmdIndexEditor), string(CmdUnstage)},
		{string(CmdDiscard)},
		{string(CmdStashSave), string(CmdStashApply)},
	}
	if got := toolbarGroups(a, toolbar.SeparatorID); !slices.EqualFunc(got[:len(groups)], groups, slices.Equal) {
		t.Fatalf("groups before the first stretch = %v", got[:len(groups)])
	}
	stretched := toolbarGroups(a, toolbar.StretchID)
	if len(stretched) != 3 {
		t.Fatalf("the row must be split by two stretches, got %d parts", len(stretched))
	}
	if !slices.Equal(stretched[1], []string{string(CmdLog), string(CmdBlame), string(CmdInvestigate)}) {
		t.Fatalf("the middle group = %v", stretched[1])
	}
	if !slices.Equal(stretched[2], []string{toolbarFlowID, toolbar.SeparatorID, string(CmdMerge), string(CmdRebase)}) {
		t.Fatalf("the trailing group = %v", stretched[2])
	}
}

func toolbarGroups(a *App, at string) [][]string {
	groups := [][]string{{}}
	for _, id := range toolbarItemIDs(a) {
		if id == at {
			groups = append(groups, []string{})
			continue
		}
		if at == toolbar.SeparatorID && id == toolbar.StretchID {
			break
		}
		groups[len(groups)-1] = append(groups[len(groups)-1], id)
	}
	return groups
}

func TestTheToolbarGivesTheArrowedButtonsAMenu(t *testing.T) {
	a := newTestApp(t)
	withArrow := []string{"btnPull", "btnSync", "btnPush", "btnSaveStash", "btnApplyStash", "btnLog", "btnGitFlow"}
	for _, name := range withArrow {
		item := toolbarItemNamed(t, a, name)
		if item.menu == nil {
			t.Fatalf("button %q must carry a drop-down menu", name)
		}
		if want := name != "btnGitFlow"; item.menu.Split != want {
			t.Fatalf("button %q split = %v, want %v", name, item.menu.Split, want)
		}
		items := readOnDispatcher(t, a, func() []widget.MenuItem {
			item.menu.OnOpening()
			return item.menu.Items
		})
		if len(items) == 0 && name != "btnApplyStash" {
			t.Fatalf("button %q opens an empty menu", name)
		}
	}
	for _, name := range []string{"btnCommit", "btnStage", "btnMerge"} {
		if toolbarItemNamed(t, a, name).menu != nil {
			t.Fatalf("button %q must be a plain button", name)
		}
	}
}

func TestEveryToolbarButtonEitherRunsACommandOrStaysDisabled(t *testing.T) {
	a := newTestApp(t)
	full := State{
		ActiveRepository: "r1", ActiveIsWorktree: true, FilesSelected: true,
		HasStagedChanges: true, HasChanges: true, HasStashable: true, HasRemotes: true,
		HasStashes: true, HasSubmodules: true, FlowConfigured: true,
	}
	seen := map[string]bool{}
	for _, entry := range toolbarCatalog() {
		if seen[entry.ID] {
			t.Fatalf("catalog entry %q is listed twice", entry.ID)
		}
		seen[entry.ID] = true
		if entry.Command == "" {
			continue
		}
		if handlerOf(a, entry.Command) == nil && full.Enabled(entry.Command) {
			t.Fatalf("button %q has no handler yet, so it must stay disabled", entry.Name)
		}
	}
}

func TestEveryToolbarCatalogEntryIsTranslatedAndDrawn(t *testing.T) {
	a := newTestApp(t)
	for _, lang := range []string{"en", "ru"} {
		a.SetLanguage(lang)
		for _, entry := range toolbarCatalog() {
			checkTranslated(t, lang, entry.LabelKey, i18n.T(entry.LabelKey))
			checkTranslated(t, lang, entry.TipKey, i18n.T(entry.TipKey))
			if toolbarIcon(entry.Icon) == nil {
				t.Fatalf("catalog entry %q has no icon %q", entry.ID, entry.Icon)
			}
		}
	}
}

func TestToolbarButtonWidthFitsCaptionInEveryLanguage(t *testing.T) {
	a := newTestApp(t)
	for _, lang := range []string{"en", "ru"} {
		a.SetLanguage(lang)
		for _, item := range a.toolbarButtons {
			btn := item.button()
			want := toolbarCaptionButtonWidth(btn.Text)
			if got := btn.Bounds().Dx(); got < want {
				t.Fatalf("lang %q: button %q width = %d, want at least %d for caption %q",
					lang, item.entry.Name, got, want, btn.Text)
			}
		}
	}
}

func TestTheDefaultToolbarFitsTheSmallestWindow(t *testing.T) {
	a := newTestApp(t)
	for _, lang := range []string{"en", "ru"} {
		a.SetLanguage(lang)
		if got := a.toolbarWidth(); got > config.MinWindowWidth {
			t.Fatalf("lang %q: the toolbar wants %d points, the window is %d wide",
				lang, got, config.MinWindowWidth)
		}
	}
}

func TestToolbarButtonWidthIsClampedToTheMaximum(t *testing.T) {
	if got := toolbarCaptionButtonWidth(strings.Repeat("Ж", 100)); got != toolbarButtonMaxWidth {
		t.Fatalf("width = %d, want the clamped maximum %d", got, toolbarButtonMaxWidth)
	}
	if got := toolbarCaptionButtonWidth(""); got != toolbarButtonMinWidth {
		t.Fatalf("width = %d, want the minimum %d", got, toolbarButtonMinWidth)
	}
}

func TestToolbarButtonWidthStaysCompactWithoutCaptions(t *testing.T) {
	a := newTestApp(t)
	a.cfg.UI.ToolbarCaptions = false
	a.SetLanguage("ru")
	for _, item := range a.toolbarButtons {
		want := toolbarCompactWidth
		if item.menu != nil {
			want += toolbarMenuArrowWidth
		}
		if got := item.button().Bounds().Dx(); got != want {
			t.Fatalf("button %q width = %d, want %d", item.entry.Name, got, want)
		}
	}
}

func TestApplySettingsRecomputesToolbarWidthWhenCaptionsAreToggled(t *testing.T) {
	a := newTestApp(t)

	m := settings.FromConfig(a.cfg)
	m.ToolbarCaptions = false
	a.applySettings(m, true)
	for _, item := range a.toolbarButtons {
		btn := item.button()
		if btn.Bounds().Dy() != toolbarCompactHeight {
			t.Fatalf("captions off: button %q height = %d, want %d", item.entry.Name, btn.Bounds().Dy(), toolbarCompactHeight)
		}
		if btn.IconPos != widget.IconOnly {
			t.Fatalf("captions off: button %q icon position = %v, want IconOnly", item.entry.Name, btn.IconPos)
		}
	}

	m.ToolbarCaptions = true
	a.applySettings(m, true)
	for _, item := range a.toolbarButtons {
		btn := item.button()
		if got, want := btn.Bounds().Dx(), toolbarCaptionButtonWidth(btn.Text); got < want {
			t.Fatalf("captions on: button %q width = %d, want at least %d for caption %q",
				item.entry.Name, got, want, btn.Text)
		}
		if btn.IconPos != widget.IconTop {
			t.Fatalf("captions on: button %q icon position = %v, want IconTop", item.entry.Name, btn.IconPos)
		}
	}
}

func TestTheStretchesShareWhateverTheButtonsLeaveOver(t *testing.T) {
	a := newTestApp(t)
	panel, ok := a.toolbarPanel()
	if !ok {
		t.Fatal("the toolbar panel is missing")
	}
	panel.SetBounds(rectOfSize(0, 0, 2000, 60))
	stretches := []*toolbarStretch{}
	fixed := panel.Padding * 2
	for _, child := range panel.Children() {
		fixed += toolbarItemMargin * 2
		if stretch, is := child.(*toolbarStretch); is {
			stretches = append(stretches, stretch)
			continue
		}
		width, _ := toolbarItemSize(child)
		fixed += width
	}
	if len(stretches) != 2 {
		t.Fatalf("stretches = %d, want 2", len(stretches))
	}
	want := (2000 - fixed) / 2
	for i, stretch := range stretches {
		if got, _ := stretch.DesiredSize(); got != want {
			t.Fatalf("stretch %d takes %d points, want %d", i, got, want)
		}
		if stretch.Bounds().Dx() != want {
			t.Fatalf("stretch %d is laid out %d wide, want %d", i, stretch.Bounds().Dx(), want)
		}
	}
	if last := panel.Children()[len(panel.Children())-1]; last.Bounds().Max.X > 2000 {
		t.Fatalf("the row ends at %d, past the panel", last.Bounds().Max.X)
	}
}

func TestAStretchWithoutAPanelAsksForNothing(t *testing.T) {
	lonely := &toolbarStretch{}
	if got, _ := lonely.DesiredSize(); got != 1 {
		t.Fatalf("width = %d, want 1", got)
	}
	panel := widget.NewStackPanel(widget.OrientationHorizontal)
	orphan := &toolbarStretch{panel: panel}
	if got, _ := orphan.DesiredSize(); got != 1 {
		t.Fatalf("width without stretches in the panel = %d, want 1", got)
	}
}

func TestTheToolbarSkipsItemsItDoesNotKnow(t *testing.T) {
	cfg := config.Default()
	cfg.UI.ToolbarItems = []string{"no.such.command", string(CmdCommit), toolbar.SeparatorID}
	a := newTestAppWithConfig(t, cfg)
	if got := toolbarItemIDs(a); !slices.Equal(got, []string{string(CmdCommit), toolbar.SeparatorID}) {
		t.Fatalf("toolbar = %v", got)
	}
}

func TestASpacerDrawsNothingWhereThereIsNothingToDraw(t *testing.T) {
	(&toolbarStretch{}).Draw(nil)
	(&toolbarSeparator{}).Draw(nil)
	invisible := &toolbarSeparator{}
	invisible.SetBounds(rectOfSize(0, 0, 9, 40))
	invisible.Draw(nil)
}

func TestAToolbarBuiltUnderATestedThemeWearsIt(t *testing.T) {
	for _, theme := range []string{config.ThemeLight, config.ThemeDark} {
		cfg := config.Default()
		cfg.Theme = theme
		a := newTestAppWithConfig(t, cfg)
		want := a.theme()
		for _, item := range a.toolbarButtons {
			if got := item.button().Background; got != want.BtnBG {
				t.Fatalf("theme %q: button %q background = %v, want %v", theme, item.entry.Name, got, want.BtnBG)
			}
		}
		a.applyToolbarConfiguration(toolbarConfigurationOf(defaultToolbarItems(), true))
		for _, item := range a.toolbarButtons {
			if got := item.button().Background; got != want.BtnBG {
				t.Fatalf("theme %q: rebuilt button %q background = %v, want %v", theme, item.entry.Name, got, want.BtnBG)
			}
		}
	}
}

func toolbarConfigurationOf(items []string, captions bool) toolbar.Result {
	return toolbar.Result{Items: items, Captions: captions}
}

func TestTheSeparatorTakesItsColourFromTheTheme(t *testing.T) {
	separator := &toolbarSeparator{}
	for _, theme := range []*widget.Theme{widget.Win11LightTheme(), widget.Win11DarkTheme()} {
		separator.ApplyTheme(theme)
		if separator.Color != theme.Border {
			t.Fatalf("colour = %v, want %v", separator.Color, theme.Border)
		}
	}
}

func TestClickingASplitButtonAndItsMenuRunsTheCommand(t *testing.T) {
	a := newTestApp(t)
	called := 0
	a.SetHandler(CmdPull, func() { called++ })
	a.SetActiveRepository("r", false)
	a.setHasRemotes(true)

	item := toolbarItemNamed(t, a, "btnPull")
	runOnDispatcher(t, a, item.button().OnClick)
	items := readOnDispatcher(t, a, func() []widget.MenuItem {
		item.menu.OnOpening()
		return item.menu.Items
	})
	pull, found := findMenuItem(items, i18n.T("Menu.Remote.Pull"))
	if !found {
		t.Fatalf("the pull menu = %v", items)
	}
	runOnDispatcher(t, a, pull.OnClick)
	if called != 2 {
		t.Fatalf("the command ran %d times, want 2", called)
	}

	commits := 0
	a.SetHandler(CmdCommit, func() { commits++ })
	a.setHasStagedChanges(true)
	runOnDispatcher(t, a, toolbarButtonNamed(t, a, "btnCommit").OnClick)
	if commits != 1 {
		t.Fatalf("the plain button ran the command %d times, want 1", commits)
	}
}

func TestTheToolbarSurvivesAWindowWithoutOne(t *testing.T) {
	a := newTestApp(t)
	delete(a.named, "toolbar")
	a.buildToolbar()
	a.relayoutToolbar()
	if got := a.toolbarWidth(); got != 0 {
		t.Fatalf("width without a panel = %d", got)
	}
}
