package app

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/config"
	"github.com/oops1/gogit/internal/systheme"
)

func newTestApp(t *testing.T) *App {
	t.Helper()
	widget.ClearStrings()
	t.Cleanup(widget.ClearStrings)
	cfg := config.Default()
	a, err := New(cfg, config.Paths{Dir: t.TempDir()}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)
	return a
}

func TestNewLoadsMainWindow(t *testing.T) {
	a := newTestApp(t)
	if a.Root() == nil || a.Engine() == nil || a.Config() == nil {
		t.Fatal("app not initialized")
	}
	if a.Root().MinWidth != config.MinWindowWidth || a.Root().MinHeight != config.MinWindowHeight {
		t.Fatalf("min size = %dx%d", a.Root().MinWidth, a.Root().MinHeight)
	}
	if a.Root().Title != "Go.Git" {
		t.Fatalf("title = %q", a.Root().Title)
	}
	for _, name := range []string{"dock", "reposTree", "branchesTree", "filesGrid", "journalGrid", "btnPull", "btnSync", "btnPush", "btnCommit"} {
		if a.Widget(name) == nil {
			t.Fatalf("widget %q missing", name)
		}
	}
	if _, ok := a.Widget("dock").(*widget.DockManager); !ok {
		t.Fatalf("dock is %T", a.Widget("dock"))
	}
	for side, size := range dockSideSizes {
		if got := a.Dock().SideSize(side); got != size {
			t.Fatalf("side %v size = %d, want %d", side, got, size)
		}
	}
	if len(a.Dock().Panes()) != 4 {
		t.Fatalf("panes = %d", len(a.Dock().Panes()))
	}
}

func TestMenuStructure(t *testing.T) {
	a := newTestApp(t)
	items := a.menu.Items()
	if len(items) != 2 {
		t.Fatalf("top menus = %d", len(items))
	}
	if len(items[0].Items) != len(repositoryMenu) {
		t.Fatalf("sub items = %d, want %d", len(items[0].Items), len(repositoryMenu))
	}
	for i, id := range repositoryMenu {
		item := items[0].Items[i]
		if id == cmdSeparator {
			if !item.Separator {
				t.Fatalf("item %d should be a separator", i)
			}
			continue
		}
		if item.Separator {
			t.Fatalf("item %d should not be a separator", i)
		}
		if item.Text != widget.Tr(menuKeys[id]) {
			t.Fatalf("item %d text = %q, want %q", i, item.Text, widget.Tr(menuKeys[id]))
		}
	}
}

func TestViewMenuStructure(t *testing.T) {
	a := newTestApp(t)
	items := a.menu.Items()
	if items[repositoryMenuIndex].Text != widget.Tr("Menu.Repository") {
		t.Fatalf("repository menu text = %q", items[repositoryMenuIndex].Text)
	}
	if items[viewMenuIndex].Text != widget.Tr("Menu.View") {
		t.Fatalf("view menu text = %q", items[viewMenuIndex].Text)
	}
	subs := items[viewMenuIndex].Items
	if len(subs) != len(a.viewMenu) {
		t.Fatalf("view sub items = %d, want %d", len(subs), len(a.viewMenu))
	}
	wantSeparators := map[int]bool{4: true, 6: true, 10: true, 13: true}
	for i, id := range a.viewMenu {
		item := subs[i]
		if wantSeparators[i] {
			if !item.Separator {
				t.Fatalf("item %d should be a separator", i)
			}
			continue
		}
		if item.Separator {
			t.Fatalf("item %d should not be a separator", i)
		}
		if item.Text != a.viewItemText(id) {
			t.Fatalf("item %d text = %q, want %q", i, item.Text, a.viewItemText(id))
		}
	}
	if a.viewMenu[0] != cmdPane("repositories") || a.viewMenu[3] != cmdPane("journal") {
		t.Fatal("pane commands out of order")
	}
	if a.viewMenu[5] != CmdResetLayout {
		t.Fatal("reset layout command out of place")
	}
	if a.viewMenu[7] != cmdTheme("system") || a.viewMenu[8] != cmdTheme("dark") || a.viewMenu[9] != cmdTheme("light") {
		t.Fatal("theme commands out of order")
	}
	if a.viewMenu[11] != cmdLanguage("en") || a.viewMenu[12] != cmdLanguage("ru") {
		t.Fatal("language commands out of order")
	}
	if a.viewMenu[14] != CmdRefresh {
		t.Fatal("refresh command out of place")
	}
}

func TestViewMenuItemsStartChecked(t *testing.T) {
	a := newTestApp(t)
	for _, id := range viewPaneIDs {
		idx := paneMenuIndex(t, a, id)
		if a.ViewItemText(idx)[:len(checkedPrefix)] != checkedPrefix {
			t.Fatalf("pane %s must start checked", id)
		}
	}
	systemIdx := indexOf(t, a.viewMenu, cmdTheme("system"))
	if a.ViewItemText(systemIdx)[:len(checkedPrefix)] != checkedPrefix {
		t.Fatal("system theme must start checked")
	}
	darkIdx := indexOf(t, a.viewMenu, cmdTheme("dark"))
	if a.ViewItemText(darkIdx)[:len(checkedPrefix)] == checkedPrefix {
		t.Fatal("dark theme must not start checked")
	}
	enIdx := indexOf(t, a.viewMenu, cmdLanguage("en"))
	if a.ViewItemText(enIdx)[:len(checkedPrefix)] != checkedPrefix {
		t.Fatal("english must start checked")
	}
}

func paneMenuIndex(t *testing.T, a *App, paneID string) int {
	t.Helper()
	return indexOf(t, a.viewMenu, cmdPane(paneID))
}

func indexOf(t *testing.T, ids []CommandID, id CommandID) int {
	t.Helper()
	for i, v := range ids {
		if v == id {
			return i
		}
	}
	t.Fatalf("%q not found in menu", id)
	return -1
}

func TestTogglingPaneFlipsCheckmark(t *testing.T) {
	a := newTestApp(t)
	idx := paneMenuIndex(t, a, "journal")
	before := a.ViewItemText(idx)
	if before[:len(checkedPrefix)] != checkedPrefix {
		t.Fatal("journal should start checked")
	}
	a.menu.OnSelect(viewMenuIndex, idx, "")
	after := a.ViewItemText(idx)
	if after[:len(checkedPrefix)] == checkedPrefix {
		t.Fatal("journal should be unchecked after toggling off")
	}
	if a.PaneVisible("journal") {
		t.Fatal("journal pane should be hidden")
	}
	a.menu.OnSelect(viewMenuIndex, idx, "")
	if !a.PaneVisible("journal") {
		t.Fatal("journal pane should be visible again")
	}
	if a.ViewItemText(idx)[:len(checkedPrefix)] != checkedPrefix {
		t.Fatal("journal should be checked again")
	}
}

func TestSelectingThemeUpdatesConfigAndCheckmarks(t *testing.T) {
	a := newTestApp(t)
	darkIdx := indexOf(t, a.viewMenu, cmdTheme("dark"))
	systemIdx := indexOf(t, a.viewMenu, cmdTheme("system"))
	a.menu.OnSelect(viewMenuIndex, darkIdx, "")
	if a.Config().Theme != config.ThemeDark {
		t.Fatalf("theme = %q", a.Config().Theme)
	}
	if a.ViewItemText(darkIdx)[:len(checkedPrefix)] != checkedPrefix {
		t.Fatal("dark theme should be checked")
	}
	if a.ViewItemText(systemIdx)[:len(checkedPrefix)] == checkedPrefix {
		t.Fatal("system theme should no longer be checked")
	}
}

func TestSelectingLanguageUpdatesMenusAndConfig(t *testing.T) {
	a := newTestApp(t)
	ruIdx := indexOf(t, a.viewMenu, cmdLanguage("ru"))
	a.menu.OnSelect(viewMenuIndex, ruIdx, "")
	if a.Config().Language != "ru" {
		t.Fatalf("language = %q", a.Config().Language)
	}
	if a.ViewItemText(ruIdx)[:len(checkedPrefix)] != checkedPrefix {
		t.Fatal("ru should be checked")
	}
	if a.MenuItemText(0) != "Добавить или создать..." {
		t.Fatalf("repository menu not retranslated: %q", a.MenuItemText(0))
	}
	if a.subItemText(viewMenuIndex, indexOf(t, a.viewMenu, CmdResetLayout)) != "Сбросить раскладку" {
		t.Fatal("view menu not retranslated")
	}
}

func TestRefreshCommandFollowsActiveRepositoryAndF5Dispatches(t *testing.T) {
	a := newTestApp(t)
	refreshIdx := indexOf(t, a.viewMenu, CmdRefresh)
	if a.ViewItemEnabled(refreshIdx) {
		t.Fatal("refresh must be disabled without an active repository")
	}
	called := 0
	a.SetHandler(CmdRefresh, func() { called++ })
	a.Engine().SendKeyEvent(widget.KeyEvent{Code: widget.KeyF5, Pressed: true})
	if called != 0 {
		t.Fatal("F5 must not dispatch refresh without an active repository")
	}
	a.SetActiveRepository("r1", false)
	if !a.ViewItemEnabled(refreshIdx) {
		t.Fatal("refresh must be enabled with an active repository")
	}
	a.Engine().SendKeyEvent(widget.KeyEvent{Code: widget.KeyF5, Pressed: true})
	if called != 1 {
		t.Fatalf("F5 must dispatch refresh, called = %d", called)
	}
}

func TestLanguageLabelFallsBackToCodeWhenKeyMissing(t *testing.T) {
	newTestApp(t)
	if got := languageLabel("xx"); got != "xx" {
		t.Fatalf("languageLabel(xx) = %q", got)
	}
	if got := languageLabel("en"); got != "English" {
		t.Fatalf("languageLabel(en) = %q", got)
	}
}

func TestMenuCommandMapping(t *testing.T) {
	a := newTestApp(t)
	if _, ok := a.menuCommand(2, 0); ok {
		t.Fatal("unknown top menu should not map")
	}
	if _, ok := a.menuCommand(0, -1); ok {
		t.Fatal("negative index")
	}
	if _, ok := a.menuCommand(0, len(repositoryMenu)); ok {
		t.Fatal("out of range")
	}
	if _, ok := a.menuCommand(0, 4); ok {
		t.Fatal("separator should not map")
	}
	if id, ok := a.menuCommand(0, 0); !ok || id != CmdAddOrCreate {
		t.Fatalf("first item = %q", id)
	}
	if id, ok := a.menuCommand(1, 0); !ok || id != cmdPane("repositories") {
		t.Fatalf("first view item = %q", id)
	}
	if _, ok := a.menuCommand(1, 4); ok {
		t.Fatal("view separator should not map")
	}
}

func TestCommandStatesFollowActiveRepository(t *testing.T) {
	a := newTestApp(t)
	closeIdx, removeIdx, addIdx := 3, 6, 0
	if a.MenuItemEnabled(closeIdx) || a.MenuItemEnabled(removeIdx) {
		t.Fatal("repository commands must be disabled without a repository")
	}
	if !a.MenuItemEnabled(addIdx) {
		t.Fatal("add must be enabled")
	}
	for _, name := range toolbarButtons {
		if a.Widget(name).(*widget.Button).IsEnabled() {
			t.Fatalf("%s must be disabled", name)
		}
	}
	a.SetActiveRepository("r1", false)
	if !a.MenuItemEnabled(closeIdx) || a.MenuItemEnabled(removeIdx) {
		t.Fatal("close enabled, remove worktree disabled for a plain repo")
	}
	a.SetActiveRepository("w1", true)
	if !a.MenuItemEnabled(removeIdx) {
		t.Fatal("remove worktree must be enabled on a worktree")
	}
	for _, name := range toolbarButtons {
		if !a.Widget(name).(*widget.Button).IsEnabled() {
			t.Fatalf("%s must be enabled", name)
		}
	}
	a.CloseRepository()
	if a.State().ActiveRepository != "" || a.MenuItemEnabled(closeIdx) {
		t.Fatal("close repository should reset state")
	}
	if a.MenuItemEnabled(-1) || a.MenuItemEnabled(100) {
		t.Fatal("out of range must be false")
	}
	if a.MenuItemText(-1) != "" || a.MenuItemText(0) == "" {
		t.Fatal("menu item text bounds")
	}
}

func TestDispatch(t *testing.T) {
	a := newTestApp(t)
	called := 0
	a.SetHandler(CmdAddOrCreate, func() { called++ })
	if !a.Dispatch(CmdAddOrCreate) || called != 1 {
		t.Fatal("handler not called")
	}
	if a.Dispatch(CmdSearch) {
		t.Fatal("no handler registered")
	}
	a.SetHandler(CmdPull, func() { called++ })
	if a.Dispatch(CmdPull) {
		t.Fatal("disabled command must not run")
	}
	a.SetActiveRepository("r", false)
	if !a.Dispatch(CmdPull) || called != 2 {
		t.Fatal("enabled command must run")
	}
	a.menu.OnSelect(0, 0, "")
	if called != 3 {
		t.Fatal("menu select should dispatch")
	}
	a.menu.OnSelect(0, 4, "")
	if called != 3 {
		t.Fatal("separator should not dispatch")
	}
	a.Widget("btnPull").(*widget.Button).OnClick()
	if called != 4 {
		t.Fatal("toolbar should dispatch")
	}
}

func TestExitAndCloseRepositoryHandlers(t *testing.T) {
	a := newTestApp(t)
	exited := false
	a.OnExit = func() { exited = true }
	a.Dispatch(CmdClose)
	if !exited {
		t.Fatal("exit handler not called")
	}
	a.OnExit = nil
	a.exit()
	a.SetActiveRepository("r", false)
	a.Dispatch(CmdCloseRepository)
	if a.State().ActiveRepository != "" {
		t.Fatal("close repository via dispatch")
	}
}

func TestThemeAndLanguageSwitch(t *testing.T) {
	a := newTestApp(t)
	a.SetTheme(config.ThemeLight)
	if a.Config().Theme != config.ThemeLight || a.EffectiveTheme() != config.ThemeLight {
		t.Fatal("light theme not stored")
	}
	a.SetTheme("bogus")
	if a.Config().Theme != config.ThemeSystem {
		t.Fatal("bogus theme should fall back to system")
	}
	if themeFor(config.ThemeLight) == nil || themeFor(config.ThemeDark) == nil {
		t.Fatal("themes")
	}
	if headers := a.ColumnHeaders("filesGrid"); len(headers) != 3 || headers[0] != "Status" {
		t.Fatalf("files headers = %v", headers)
	}
	a.SetLanguage("ru")
	if a.MenuItemText(0) != "Добавить или создать..." {
		t.Fatalf("menu not retranslated: %q", a.MenuItemText(0))
	}
	if headers := a.ColumnHeaders("journalGrid"); len(headers) != 5 || headers[1] != "Сообщение" {
		t.Fatalf("journal headers = %v", headers)
	}
	if a.Config().Language != "ru" {
		t.Fatal("language not stored")
	}
	a.SetLanguage("en")
	if a.MenuItemText(0) != "Add or Create..." {
		t.Fatalf("menu not retranslated back: %q", a.MenuItemText(0))
	}
}

func TestSystemThemeFollowsDetector(t *testing.T) {
	a := newTestApp(t)
	a.SetTheme(config.ThemeSystem)
	scheme := systheme.Light
	a.SetSystemThemeDetector(func() systheme.Scheme { return scheme })
	if a.EffectiveTheme() != config.ThemeLight {
		t.Fatalf("effective = %q", a.EffectiveTheme())
	}
	scheme = systheme.Dark
	if a.EffectiveTheme() != config.ThemeDark {
		t.Fatalf("effective = %q", a.EffectiveTheme())
	}
	scheme = systheme.Unknown
	if a.EffectiveTheme() != config.ThemeDark {
		t.Fatal("unknown must fall back to dark")
	}
	a.SetTheme(config.ThemeLight)
	if a.EffectiveTheme() != config.ThemeLight {
		t.Fatal("explicit theme must ignore detector")
	}
	if effectiveTheme(config.ThemeDark, nil) != config.ThemeDark {
		t.Fatal("explicit theme must not call detector")
	}
}

func TestFollowSystemThemeStopsOnCancel(t *testing.T) {
	a := newTestApp(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	a.FollowSystemTheme(ctx)
}

func TestOnSystemThemeChanged(t *testing.T) {
	a := newTestApp(t)
	a.SetTheme(config.ThemeSystem)
	if !a.OnSystemThemeChanged(systheme.Light) {
		t.Fatal("system theme must react")
	}
	a.SetTheme(config.ThemeDark)
	if a.OnSystemThemeChanged(systheme.Light) {
		t.Fatal("explicit theme must ignore system changes")
	}
}

func TestNewFromXAMLErrors(t *testing.T) {
	widget.ClearStrings()
	t.Cleanup(widget.ClearStrings)
	paths := config.Paths{Dir: t.TempDir()}
	cases := map[string]string{
		"invalid xml":     `<Window`,
		"root not window": `<Canvas/>`,
		"no menu":         `<Window/>`,
		"no tree":         `<Window><Menu x:Name="mainMenu"/></Window>`,
		"no dock": `<Window><Menu x:Name="mainMenu"/><TreeView x:Name="reposTree"/><TreeView x:Name="branchesTree"/>` +
			`<TextBlock x:Name="statusText"/><TextBlock x:Name="statusBranch"/><ProgressBar x:Name="statusProgress"/></Window>`,
		"no toolbar": `<Window><Menu x:Name="mainMenu"/><DockManager x:Name="dock"/>` +
			`<TreeView x:Name="reposTree"/><TreeView x:Name="branchesTree"/>` +
			`<DataGrid x:Name="filesGrid"/><DataGrid x:Name="journalGrid"/>` +
			`<TextBlock x:Name="statusText"/><TextBlock x:Name="statusBranch"/><ProgressBar x:Name="statusProgress"/></Window>`,
		"grid is not a datagrid": `<Window><Menu x:Name="mainMenu"/><DockManager x:Name="dock"/>` +
			`<TreeView x:Name="reposTree"/><TreeView x:Name="branchesTree"/>` +
			`<TextBlock x:Name="filesGrid"/><DataGrid x:Name="journalGrid"/>` +
			`<TextBlock x:Name="statusText"/><TextBlock x:Name="statusBranch"/><ProgressBar x:Name="statusProgress"/>` +
			`<Button x:Name="btnPull"/><Button x:Name="btnSync"/><Button x:Name="btnPush"/><Button x:Name="btnCommit"/></Window>`,
		"repos tree is not a tree view": `<Window><Menu x:Name="mainMenu"/>` +
			`<TextBlock x:Name="reposTree"/><TreeView x:Name="branchesTree"/>` +
			`<TextBlock x:Name="statusText"/><TextBlock x:Name="statusBranch"/><ProgressBar x:Name="statusProgress"/></Window>`,
		"status text is not a label": `<Window><Menu x:Name="mainMenu"/>` +
			`<TreeView x:Name="reposTree"/><TreeView x:Name="branchesTree"/>` +
			`<Button x:Name="statusText"/><TextBlock x:Name="statusBranch"/><ProgressBar x:Name="statusProgress"/></Window>`,
	}
	for name, xaml := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := NewFromXAML(config.Default(), paths, []byte(xaml), nil); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestNewFromXAMLToleratesFewerColumns(t *testing.T) {
	widget.ClearStrings()
	t.Cleanup(widget.ClearStrings)
	xaml := `<Window><Menu x:Name="mainMenu"/><DockManager x:Name="dock"/>` +
		`<TreeView x:Name="reposTree"/><TreeView x:Name="branchesTree"/>` +
		`<DataGrid x:Name="filesGrid"><DataGrid.Columns><DataGridTextColumn Header="x" Binding="{Binding X}"/></DataGrid.Columns></DataGrid>` +
		`<DataGrid x:Name="journalGrid"/>` +
		`<TextBlock x:Name="statusText"/><TextBlock x:Name="statusBranch"/><ProgressBar x:Name="statusProgress"/>` +
		`<Button x:Name="btnPull"/><Button x:Name="btnSync"/><Button x:Name="btnPush"/><Button x:Name="btnCommit"/></Window>`
	a, err := NewFromXAML(config.Default(), config.Paths{Dir: t.TempDir()}, []byte(xaml), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)
	if headers := a.ColumnHeaders("filesGrid"); len(headers) != 1 || headers[0] != "Status" {
		t.Fatalf("headers = %v", headers)
	}
	if headers := a.ColumnHeaders("journalGrid"); len(headers) != 0 {
		t.Fatalf("headers = %v", headers)
	}
}

func TestNewFailsOnBrokenUserI18N(t *testing.T) {
	widget.ClearStrings()
	t.Cleanup(widget.ClearStrings)
	dir := t.TempDir()
	paths := config.Paths{Dir: dir}
	if err := paths.Ensure(); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(paths.UserI18NDir(), "xx.json", "{"); err != nil {
		t.Fatal(err)
	}
	if _, err := New(config.Default(), paths, nil); err == nil {
		t.Fatal("expected error")
	}
}

func TestLoggingEmitsDebugEvents(t *testing.T) {
	widget.ClearStrings()
	t.Cleanup(widget.ClearStrings)
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	a, err := New(config.Default(), config.Paths{Dir: t.TempDir()}, logger)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)
	a.SetHandler(CmdAddOrCreate, func() {})
	a.Dispatch(CmdAddOrCreate)
	a.SetTheme(config.ThemeLight)
	a.SetLanguage("ru")
	out := buf.String()
	for _, want := range []string{"app started", "command dispatched", "theme changed", "language changed"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in log: %s", want, out)
		}
	}
}
