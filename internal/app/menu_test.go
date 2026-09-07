package app

import (
	"bytes"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/config"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/journal"
)

func viewTopItems(t *testing.T, a *App) []widget.MenuItem {
	t.Helper()
	items := a.menu.Items()
	if len(items) <= viewMenuIndex {
		t.Fatal("view menu missing")
	}
	return items[viewMenuIndex].Items
}

func viewLeafItem(t *testing.T, a *App, groupPos, leafPos int) widget.MenuItem {
	t.Helper()
	top := viewTopItems(t, a)
	if groupPos < 0 || groupPos >= len(top) {
		t.Fatalf("view entry %d missing", groupPos)
	}
	subs := top[groupPos].SubItems
	if leafPos < 0 || leafPos >= len(subs) {
		t.Fatalf("view leaf %d missing in group %d", leafPos, groupPos)
	}
	return subs[leafPos]
}

func leafIndex(t *testing.T, group *menuGroupEntry, cmd CommandID) int {
	t.Helper()
	for i, leaf := range group.Items {
		if leaf.Command == cmd {
			return i
		}
	}
	t.Fatalf("%q not found in group %q", cmd, group.Key)
	return -1
}

func TestViewMenuTreeStructure(t *testing.T) {
	a := newTestApp(t)
	items := a.menu.Items()
	if len(items) != len(menuBarDefs) {
		t.Fatalf("top menus = %d, want %d", len(items), len(menuBarDefs))
	}
	if items[viewMenuIndex].Text != widget.Tr("Menu.View") {
		t.Fatalf("view menu text = %q", items[viewMenuIndex].Text)
	}
	top := viewTopItems(t, a)
	if len(top) != len(viewMenuTree) {
		t.Fatalf("view sub items = %d, want %d", len(top), len(viewMenuTree))
	}
	for i, entry := range viewMenuTree {
		item := top[i]
		if entry.Separator {
			if !item.Separator {
				t.Fatalf("item %d should be a separator", i)
			}
			continue
		}
		if item.Separator {
			t.Fatalf("item %d should not be a separator", i)
		}
		switch {
		case entry.Leaf != nil:
			if len(item.SubItems) != 0 {
				t.Fatalf("item %d should not have sub items", i)
			}
			if item.Text != widget.Tr(entry.Leaf.Key) {
				t.Fatalf("item %d text = %q, want %q", i, item.Text, widget.Tr(entry.Leaf.Key))
			}
		case entry.Group != nil:
			if item.Text != widget.Tr(entry.Group.Key) {
				t.Fatalf("group %d text = %q, want %q", i, item.Text, widget.Tr(entry.Group.Key))
			}
			if len(item.SubItems) != len(entry.Group.Items) {
				t.Fatalf("group %d sub items = %d, want %d", i, len(item.SubItems), len(entry.Group.Items))
			}
			for j, leaf := range entry.Group.Items {
				sub := item.SubItems[j]
				if sub.Text != a.viewLeafText(leaf) {
					t.Fatalf("group %d item %d text = %q, want %q", i, j, sub.Text, a.viewLeafText(leaf))
				}
			}
		default:
			t.Fatalf("item %d has neither leaf nor group", i)
		}
	}
}

func TestViewMenuTreeShapeMatchesSpec(t *testing.T) {
	if len(viewMenuTree) != 7 {
		t.Fatalf("view tree entries = %d, want 7", len(viewMenuTree))
	}
	if viewMenuTree[0].Group == nil || viewMenuTree[0].Group.Key != "Menu.View.Panes" {
		t.Fatal("panes group out of place")
	}
	if len(viewMenuTree[0].Group.Items) != 4 {
		t.Fatal("panes group must have 4 items")
	}
	if viewMenuTree[1].Leaf == nil || viewMenuTree[1].Leaf.Command != CmdResetLayout {
		t.Fatal("reset layout out of place")
	}
	if !viewMenuTree[2].Separator {
		t.Fatal("expected separator at index 2")
	}
	if viewMenuTree[3].Group == nil || viewMenuTree[3].Group.Key != "Menu.View.Theme" {
		t.Fatal("theme group out of place")
	}
	if len(viewMenuTree[3].Group.Items) != 3 {
		t.Fatal("theme group must have 3 items")
	}
	if viewMenuTree[4].Group == nil || viewMenuTree[4].Group.Key != "Menu.View.Language" {
		t.Fatal("language group out of place")
	}
	if len(viewMenuTree[4].Group.Items) != 2 {
		t.Fatal("language group must have 2 items")
	}
	if !viewMenuTree[5].Separator {
		t.Fatal("expected separator at index 5")
	}
	if viewMenuTree[6].Leaf == nil || viewMenuTree[6].Leaf.Command != CmdRefresh {
		t.Fatal("refresh out of place")
	}
}

func TestViewMenuItemsStartChecked(t *testing.T) {
	a := newTestApp(t)
	panes := viewMenuTree[0].Group
	for i, leaf := range panes.Items {
		text := viewLeafItem(t, a, 0, i).Text
		if text[:len(checkedPrefix)] != checkedPrefix {
			t.Fatalf("pane %s must start checked", leaf.Command)
		}
	}
	theme := viewMenuTree[3].Group
	systemIdx := leafIndex(t, theme, cmdTheme(config.ThemeSystem))
	if viewLeafItem(t, a, 3, systemIdx).Text[:len(checkedPrefix)] != checkedPrefix {
		t.Fatal("system theme must start checked")
	}
	darkIdx := leafIndex(t, theme, cmdTheme(config.ThemeDark))
	if viewLeafItem(t, a, 3, darkIdx).Text[:len(checkedPrefix)] == checkedPrefix {
		t.Fatal("dark theme must not start checked")
	}
	lang := viewMenuTree[4].Group
	enIdx := leafIndex(t, lang, cmdLanguage("en"))
	if viewLeafItem(t, a, 4, enIdx).Text[:len(checkedPrefix)] != checkedPrefix {
		t.Fatal("english must start checked")
	}
}

func TestTogglingPaneFlipsCheckmark(t *testing.T) {
	a := newTestApp(t)
	panes := viewMenuTree[0].Group
	idx := leafIndex(t, panes, cmdPane("journal"))
	before := viewLeafItem(t, a, 0, idx)
	if before.Text[:len(checkedPrefix)] != checkedPrefix {
		t.Fatal("journal should start checked")
	}
	before.OnClick()
	after := viewLeafItem(t, a, 0, idx)
	if after.Text[:len(checkedPrefix)] == checkedPrefix {
		t.Fatal("journal should be unchecked after toggling off")
	}
	if a.PaneVisible("journal") {
		t.Fatal("journal pane should be hidden")
	}
	after.OnClick()
	if !a.PaneVisible("journal") {
		t.Fatal("journal pane should be visible again")
	}
	if viewLeafItem(t, a, 0, idx).Text[:len(checkedPrefix)] != checkedPrefix {
		t.Fatal("journal should be checked again")
	}
}

func TestSelectingThemeUpdatesConfigAndCheckmarks(t *testing.T) {
	a := newTestApp(t)
	theme := viewMenuTree[3].Group
	darkIdx := leafIndex(t, theme, cmdTheme(config.ThemeDark))
	systemIdx := leafIndex(t, theme, cmdTheme(config.ThemeSystem))
	viewLeafItem(t, a, 3, darkIdx).OnClick()
	if a.Config().Theme != config.ThemeDark {
		t.Fatalf("theme = %q", a.Config().Theme)
	}
	if viewLeafItem(t, a, 3, darkIdx).Text[:len(checkedPrefix)] != checkedPrefix {
		t.Fatal("dark theme should be checked")
	}
	if viewLeafItem(t, a, 3, systemIdx).Text[:len(checkedPrefix)] == checkedPrefix {
		t.Fatal("system theme should no longer be checked")
	}
}

func TestSelectingLanguageUpdatesMenusAndConfig(t *testing.T) {
	a := newTestApp(t)
	lang := viewMenuTree[4].Group
	ruIdx := leafIndex(t, lang, cmdLanguage("ru"))
	viewLeafItem(t, a, 4, ruIdx).OnClick()
	if a.Config().Language != "ru" {
		t.Fatalf("language = %q", a.Config().Language)
	}
	if viewLeafItem(t, a, 4, ruIdx).Text[:len(checkedPrefix)] != checkedPrefix {
		t.Fatal("ru should be checked")
	}
	if text, _, _ := a.MenuItemByCommand(CmdAddOrCreate); text != "Добавить или создать..." {
		t.Fatalf("repository menu not retranslated: %q", text)
	}
	if text := viewTopItems(t, a)[1].Text; text != "Сбросить раскладку" {
		t.Fatalf("top-level view menu not retranslated: %q", text)
	}
	panes := viewMenuTree[0].Group
	journalIdx := leafIndex(t, panes, cmdPane("journal"))
	if text := viewLeafItem(t, a, 0, journalIdx).Text; text != checkedPrefix+"Журнал" {
		t.Fatalf("nested pane menu not retranslated: %q", text)
	}
}

func TestRefreshCommandFollowsActiveRepositoryAndF5Dispatches(t *testing.T) {
	a := newTestApp(t)
	if !viewTopItems(t, a)[6].Disabled {
		t.Fatal("refresh must be disabled without an active repository")
	}
	called := 0
	a.SetHandler(CmdRefresh, func() { called++ })
	a.Engine().SendKeyEvent(widget.KeyEvent{Code: widget.KeyF5, Pressed: true})
	if called != 0 {
		t.Fatal("F5 must not dispatch refresh without an active repository")
	}
	a.SetActiveRepository("r1", false)
	if viewTopItems(t, a)[6].Disabled {
		t.Fatal("refresh must be enabled with an active repository")
	}
	a.Engine().SendKeyEvent(widget.KeyEvent{Code: widget.KeyF5, Pressed: true})
	if called != 1 {
		t.Fatalf("F5 must dispatch refresh, called = %d", called)
	}
	viewTopItems(t, a)[6].OnClick()
	if called != 2 {
		t.Fatal("clicking the refresh menu item must dispatch")
	}
}

func TestResetLayoutMenuItemDispatches(t *testing.T) {
	a := newTestApp(t)
	called := 0
	a.SetHandler(CmdResetLayout, func() { called++ })
	viewTopItems(t, a)[1].OnClick()
	if called != 1 {
		t.Fatal("reset layout menu item must dispatch")
	}
}

func TestLogLanguageMenuLimitWarnsAboutExtraLanguages(t *testing.T) {
	widget.ClearStrings()
	t.Cleanup(widget.ClearStrings)
	dir := t.TempDir()
	paths := config.Paths{Dir: dir}
	if err := paths.Ensure(); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(paths.UserI18NDir(), "de.json", "{}"); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	a, err := New(config.Default(), paths, logger)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)
	if !strings.Contains(buf.String(), "view menu shows only built-in languages") {
		t.Fatal("expected language menu limit to be logged")
	}
}

func TestRemoteMenuTreeStructure(t *testing.T) {
	a := newTestApp(t)
	items := a.menu.Items()
	if items[remoteMenuIndex].Text != widget.Tr("Menu.Remote") {
		t.Fatalf("remote menu text = %q", items[remoteMenuIndex].Text)
	}
	sub := items[remoteMenuIndex].Items
	if len(sub) != len(remoteMenuTree) {
		t.Fatalf("remote sub items = %d, want %d", len(sub), len(remoteMenuTree))
	}
	for i, entry := range remoteMenuTree {
		if entry.Separator {
			if !sub[i].Separator {
				t.Fatalf("item %d should be a separator", i)
			}
			continue
		}
		if entry.Leaf == nil {
			t.Fatalf("item %d must be a leaf", i)
		}
		if sub[i].Text != widget.Tr(entry.Leaf.Key) {
			t.Fatalf("item %d text = %q, want %q", i, sub[i].Text, widget.Tr(entry.Leaf.Key))
		}
	}
}

func TestRemoteMenuAndToolbarShowTranslatedTextNotRawKeys(t *testing.T) {
	a := newTestApp(t)
	toolbarKeys := map[string]string{
		"btnPull": "Toolbar.Pull",
		"btnSync": "Toolbar.Sync",
		"btnPush": "Toolbar.Push",
	}
	for _, lang := range []string{"en", "ru"} {
		a.SetLanguage(lang)
		sub := a.menu.Items()[remoteMenuIndex].Items
		for i, entry := range remoteMenuTree {
			if entry.Leaf == nil {
				continue
			}
			switch entry.Leaf.Command {
			case CmdPull, CmdPush, CmdSync:
			default:
				continue
			}
			text := sub[i].Text
			if text == "" || text == entry.Leaf.Key {
				t.Fatalf("lang %q: remote menu item for command %v shows %q instead of translated text",
					lang, entry.Leaf.Command, text)
			}
		}
		for name, key := range toolbarKeys {
			btn, ok := a.Widget(name).(*widget.Button)
			if !ok {
				t.Fatalf("widget %s is not a Button", name)
			}
			if btn.Text == "" || btn.Text == key {
				t.Fatalf("lang %q: %s caption shows %q instead of translated text", lang, name, btn.Text)
			}
		}
	}
}

func TestDockPaneTitlesRetranslateOnLanguageChange(t *testing.T) {
	a := newTestApp(t)
	if len(viewPaneKeys) == 0 {
		t.Fatal("viewPaneKeys must not be empty")
	}
	for _, lang := range []string{"ru", "en", "ru"} {
		a.SetLanguage(lang)
		for _, pane := range a.Dock().Panes() {
			key, ok := viewPaneKeys[pane.ID]
			if !ok {
				continue
			}
			want := widget.Tr(key)
			if pane.Title != want {
				t.Fatalf("lang %q: pane %q title = %q, want %q", lang, pane.ID, pane.Title, want)
			}
		}
	}
}

func TestDockPaneTitlesIgnoreAPaneWithNoKnownKey(t *testing.T) {
	a := newTestApp(t)
	bogus := widget.NewDockPane("bogus-pane", "Bogus", nil)
	a.Dock().AddPane(bogus, widget.DockLeft)
	a.SetLanguage("ru")
	if bogus.Title != "Bogus" {
		t.Fatalf("pane with no known key must keep its title, got %q", bogus.Title)
	}
}

func TestBranchesTreeGroupLabelsRetranslateOnLanguageChange(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	initTestRepo(t, target)
	cfg := config.Default()
	cfg.Repositories = []config.Repository{{ID: "r1", Name: "Main", Path: target}}
	a := newTestAppWithConfig(t, cfg)
	a.ActivateRepository("r1")

	tree := a.Widget("branchesTree").(*widget.TreeViewWidget)
	for _, lang := range []string{"ru", "en", "ru"} {
		a.SetLanguage(lang)
		roots := tree.Tree.Roots()
		if len(roots) < 3 {
			t.Fatalf("lang %q: got %d root groups, want at least 3", lang, len(roots))
		}
		wantLocal := i18n.T("Pane.Branches.Local")
		wantRemotes := i18n.T("Pane.Branches.Remotes")
		wantTags := i18n.T("Pane.Branches.Tags")
		if roots[0].DisplayText() != wantLocal {
			t.Fatalf("lang %q: local group label = %q, want %q", lang, roots[0].DisplayText(), wantLocal)
		}
		if roots[1].DisplayText() != wantRemotes {
			t.Fatalf("lang %q: remotes group label = %q, want %q", lang, roots[1].DisplayText(), wantRemotes)
		}
		if roots[2].DisplayText() != wantTags {
			t.Fatalf("lang %q: tags group label = %q, want %q", lang, roots[2].DisplayText(), wantTags)
		}
	}
}

func TestBranchesTreeAndStatusRetranslateOnLanguageChangeWhenDetached(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	initTestRepo(t, target)
	head := oid(t, "aa")
	detachTestHead(t, target, head)
	cfg := config.Default()
	cfg.Repositories = []config.Repository{{ID: "r1", Name: "Main", Path: target}}
	a := newTestAppWithConfig(t, cfg)
	a.ActivateRepository("r1")

	shortHead := head.String()[:shortHashLength]
	for _, lang := range []string{"ru", "en", "ru"} {
		a.SetLanguage(lang)
		want := i18n.T("Pane.Branches.Detached") + " " + shortHead
		item, ok := a.branchesView.Item(refs.HEAD)
		if !ok {
			t.Fatalf("lang %q: detached branch item missing", lang)
		}
		if item.DisplayText() != want {
			t.Fatalf("lang %q: branches tree detached item = %q, want %q", lang, item.DisplayText(), want)
		}
		if got := a.statusBranchLabel.Text(); got != want {
			t.Fatalf("lang %q: status branch label = %q, want %q", lang, got, want)
		}
	}
}

func TestRetranslateRepoTreesLogsAWarningWhenTheBranchSnapshotFailsToLoad(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	initTestRepo(t, target)
	a, buf := newChangesTestApp(t, target)
	a.ActivateRepository("r1")
	withFailingBranchSnapshotLoad(t)

	a.SetLanguage("ru")

	if !strings.Contains(buf.String(), "retranslate branches failed") {
		t.Fatalf("expected the branch snapshot error to be logged: %s", buf.String())
	}
}

func TestFilesGridStateColumnRetranslatesOnLanguageChange(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	buildWorkingRepoFixture(t, target)
	a := activatedWorkingApp(t, target)
	waitForWorkingRows(t, a, 5)

	for _, lang := range []string{"ru", "en", "ru"} {
		a.SetLanguage(lang)
		want := i18n.T("Files.State.Modified")
		deadline := time.Now().Add(testTimeout)
		for {
			row := filesRowOnDispatcher(t, a, 2)
			if row.State == want {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("lang %q: modified file state = %q, want %q", lang, row.State, want)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
}

func TestRetranslateSkipsWorkingRefreshWhileACommitIsSelected(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	buildWorkingRepoFixture(t, target)
	a := activatedWorkingApp(t, target)
	waitForWorkingRows(t, a, 1)
	waitForJournalRows(t, a, 1)

	first := readOnDispatcher(t, a, func() journal.Row {
		row, _ := a.named["journalGrid"].(*widget.DataGridWidget).Grid.ItemsSource().Get(0).(journal.Row)
		return row
	})
	readOnDispatcher(t, a, func() bool { a.onJournalRowSelected(first); return true })
	waitForFilesMode(t, a, filesModeCommit, 1)
	before := filesRowOnDispatcher(t, a, 0)

	a.SetLanguage("ru")
	a.SetLanguage("en")
	a.diffWG.Wait()

	if got := filesModeOnDispatcher(a); got != filesModeCommit {
		t.Fatalf("files grid mode = %v after a language change with a commit selected, want %v", got, filesModeCommit)
	}
	if after := filesRowOnDispatcher(t, a, 0); after != before {
		t.Fatalf("commit diff row changed while a commit was selected: %+v -> %+v", before, after)
	}
}

func TestRemoteMenuItemsFollowHasRemotes(t *testing.T) {
	a := newTestApp(t)
	fetchIdx := -1
	manageIdx := -1
	for i, entry := range remoteMenuTree {
		if entry.Leaf == nil {
			continue
		}
		switch entry.Leaf.Command {
		case CmdFetch:
			fetchIdx = i
		case CmdManageRemotes:
			manageIdx = i
		}
	}
	if fetchIdx < 0 || manageIdx < 0 {
		t.Fatal("fetch and manage remotes must be part of the remote menu")
	}
	sub := func() []widget.MenuItem { return a.menu.Items()[remoteMenuIndex].Items }
	if !sub()[fetchIdx].Disabled || !sub()[manageIdx].Disabled {
		t.Fatal("remote commands must start disabled without an active repository")
	}
	a.SetActiveRepository("r1", false)
	if !sub()[fetchIdx].Disabled {
		t.Fatal("fetch must stay disabled without a remote")
	}
	if sub()[manageIdx].Disabled {
		t.Fatal("manage remotes must be enabled once a repository is open")
	}
	a.setHasRemotes(true)
	if sub()[fetchIdx].Disabled {
		t.Fatal("fetch must be enabled once a remote exists")
	}
}
