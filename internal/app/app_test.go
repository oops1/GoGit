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
	if len(items[0].Items) != len(repositoryMenuTree) {
		t.Fatalf("sub items = %d, want %d", len(items[0].Items), len(repositoryMenuTree))
	}
	for i, entry := range repositoryMenuTree {
		item := items[0].Items[i]
		if entry.Separator {
			if !item.Separator {
				t.Fatalf("item %d should be a separator", i)
			}
			continue
		}
		if item.Separator {
			t.Fatalf("item %d should not be a separator", i)
		}
		if entry.Leaf == nil {
			t.Fatalf("item %d must be a leaf", i)
		}
		if item.Text != widget.Tr(entry.Leaf.Key) {
			t.Fatalf("item %d text = %q, want %q", i, item.Text, widget.Tr(entry.Leaf.Key))
		}
	}
}

func repositoryItemIndex(t *testing.T, cmd CommandID) int {
	t.Helper()
	for i, entry := range repositoryMenuTree {
		if entry.Leaf != nil && entry.Leaf.Command == cmd {
			return i
		}
	}
	t.Fatalf("%q not found in repository menu", cmd)
	return -1
}

func TestRepositoryMenuSeparatorsHaveNoClickHandler(t *testing.T) {
	a := newTestApp(t)
	items := a.menu.Items()[repositoryMenuIndex].Items
	for i, entry := range repositoryMenuTree {
		if entry.Separator && items[i].OnClick != nil {
			t.Fatalf("separator %d must not have a click handler", i)
		}
	}
}

func TestMenuItemByCommandFindsRepositoryAndViewEntries(t *testing.T) {
	a := newTestApp(t)
	if _, _, ok := a.MenuItemByCommand(CommandID("unknown")); ok {
		t.Fatal("unknown command should not map")
	}
	text, enabled, ok := a.MenuItemByCommand(CmdAddOrCreate)
	if !ok || !enabled || text != widget.Tr("Menu.Repository.AddOrCreate") {
		t.Fatalf("add-or-create lookup = %q, %v, %v", text, enabled, ok)
	}
	if _, _, ok := a.MenuItemByCommand(CmdResetLayout); !ok {
		t.Fatal("view menu command should also be found")
	}
}

func TestCommandStatesFollowActiveRepository(t *testing.T) {
	a := newTestApp(t)
	if _, enabled, ok := a.MenuItemByCommand(CmdCloseRepository); !ok || enabled {
		t.Fatal("close repository must be disabled without an active repository")
	}
	if _, enabled, ok := a.MenuItemByCommand(CmdRemoveWorktree); !ok || enabled {
		t.Fatal("remove worktree must be disabled without an active repository")
	}
	if _, enabled, ok := a.MenuItemByCommand(CmdAddOrCreate); !ok || !enabled {
		t.Fatal("add must be enabled")
	}
	for _, name := range toolbarButtons {
		if a.Widget(name).(*widget.Button).IsEnabled() {
			t.Fatalf("%s must be disabled", name)
		}
	}
	a.SetActiveRepository("r1", false)
	if _, enabled, ok := a.MenuItemByCommand(CmdCloseRepository); !ok || !enabled {
		t.Fatal("close must be enabled for a plain repo")
	}
	if _, enabled, ok := a.MenuItemByCommand(CmdRemoveWorktree); !ok || enabled {
		t.Fatal("remove worktree must be disabled for a plain repo")
	}
	a.SetActiveRepository("w1", true)
	if _, enabled, ok := a.MenuItemByCommand(CmdRemoveWorktree); !ok || !enabled {
		t.Fatal("remove worktree must be enabled on a worktree")
	}
	for _, name := range toolbarButtons {
		if !a.Widget(name).(*widget.Button).IsEnabled() {
			t.Fatalf("%s must be enabled", name)
		}
	}
	a.CloseRepository()
	if a.State().ActiveRepository != "" {
		t.Fatal("close repository should reset state")
	}
	if _, enabled, ok := a.MenuItemByCommand(CmdCloseRepository); !ok || enabled {
		t.Fatal("close repository should be disabled again")
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
	addIdx := repositoryItemIndex(t, CmdAddOrCreate)
	a.menu.Items()[repositoryMenuIndex].Items[addIdx].OnClick()
	if called != 3 {
		t.Fatal("clicking the repository menu item should dispatch")
	}
	a.Widget("btnPull").(*widget.Button).OnClick()
	if called != 4 {
		t.Fatal("toolbar should dispatch")
	}
}

func TestClickingDisabledRepositoryMenuItemDoesNothing(t *testing.T) {
	a := newTestApp(t)
	called := 0
	a.SetHandler(CmdCloseRepository, func() { called++ })
	closeIdx := repositoryItemIndex(t, CmdCloseRepository)
	a.menu.Items()[repositoryMenuIndex].Items[closeIdx].OnClick()
	if called != 0 {
		t.Fatal("close repository must not dispatch without an active repository")
	}
	a.SetActiveRepository("r1", false)
	a.menu.Items()[repositoryMenuIndex].Items[closeIdx].OnClick()
	if called != 1 {
		t.Fatal("close repository must dispatch with an active repository")
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
	if headers := a.ColumnHeaders("filesGrid"); len(headers) != 3 || headers[0] != "Name" {
		t.Fatalf("files headers = %v", headers)
	}
	a.SetLanguage("ru")
	if text, _, _ := a.MenuItemByCommand(CmdAddOrCreate); text != "Добавить или создать..." {
		t.Fatalf("menu not retranslated: %q", text)
	}
	if headers := a.ColumnHeaders("filesGrid"); len(headers) != 3 || headers[0] != "Имя" {
		t.Fatalf("files headers = %v", headers)
	}
	if headers := a.ColumnHeaders("journalGrid"); len(headers) != 5 || headers[1] != "Сообщение" {
		t.Fatalf("journal headers = %v", headers)
	}
	if a.Config().Language != "ru" {
		t.Fatal("language not stored")
	}
	a.SetLanguage("en")
	if text, _, _ := a.MenuItemByCommand(CmdAddOrCreate); text != "Add or Create..." {
		t.Fatalf("menu not retranslated back: %q", text)
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
			`<FilesGrid x:Name="filesGrid"/><DataGrid x:Name="journalGrid"/>` +
			`<TextBlock x:Name="statusText"/><TextBlock x:Name="statusBranch"/><ProgressBar x:Name="statusProgress"/></Window>`,
		"diff view is not a diff view": `<Window><Menu x:Name="mainMenu"/><DockManager x:Name="dock"/>` +
			`<TreeView x:Name="reposTree"/><TreeView x:Name="branchesTree"/>` +
			`<FilesGrid x:Name="filesGrid"/><DataGrid x:Name="journalGrid"/><TextBlock x:Name="diffView"/>` +
			`<TextBlock x:Name="statusText"/><TextBlock x:Name="statusBranch"/><ProgressBar x:Name="statusProgress"/>` +
			`<Button x:Name="btnPull"/><Button x:Name="btnSync"/><Button x:Name="btnPush"/><Button x:Name="btnCommit"/></Window>`,
		"files grid is not a files grid": `<Window><Menu x:Name="mainMenu"/><DockManager x:Name="dock"/>` +
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

func TestDiffViewIsTakenFromTheMainWindow(t *testing.T) {
	a := newTestApp(t)
	if a.DiffView() == nil {
		t.Fatal("changes pane widget missing")
	}
	if a.DiffView() != a.Widget("diffView") {
		t.Fatal("DiffView must return the named widget")
	}
}

func TestNewFromXAMLBuildsTheFilesGridWithItsDefaultColumns(t *testing.T) {
	widget.ClearStrings()
	t.Cleanup(widget.ClearStrings)
	xaml := `<Window><Menu x:Name="mainMenu"/><DockManager x:Name="dock"/>` +
		`<TreeView x:Name="reposTree"/><TreeView x:Name="branchesTree"/>` +
		`<FilesGrid x:Name="filesGrid"/>` +
		`<TextBox x:Name="filesFilter"/><TextBlock x:Name="filesFilterCount"/>` +
		filesStatusButtonsXAML +
		`<DataGrid x:Name="journalGrid"/><DiffView x:Name="diffView"/>` +
		`<TextBlock x:Name="statusText"/><TextBlock x:Name="statusBranch"/><ProgressBar x:Name="statusProgress"/>` +
		`<Button x:Name="btnPull"/><Button x:Name="btnSync"/><Button x:Name="btnPush"/><Button x:Name="btnCommit"/></Window>`
	a, err := NewFromXAML(config.Default(), config.Paths{Dir: t.TempDir()}, []byte(xaml), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)
	if headers := a.ColumnHeaders("filesGrid"); len(headers) != 3 {
		t.Fatalf("headers = %v, want 3 default columns", headers)
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
