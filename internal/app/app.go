package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/oops1/headless-gui/v3/engine"
	"github.com/oops1/headless-gui/v3/widget"
	"github.com/oops1/headless-gui/v3/widget/datagrid"
	"github.com/oops1/headless-gui/v3/window"

	"github.com/oops1/gogit/internal/assets"
	"github.com/oops1/gogit/internal/config"
	"github.com/oops1/gogit/internal/gitcore/diff"
	"github.com/oops1/gogit/internal/gitcore/hash"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/gitcore/worktree"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/layout"
	"github.com/oops1/gogit/internal/repo"
	"github.com/oops1/gogit/internal/repo/watch"
	"github.com/oops1/gogit/internal/systheme"
	"github.com/oops1/gogit/internal/ui/addrepo"
	"github.com/oops1/gogit/internal/ui/branches"
	"github.com/oops1/gogit/internal/ui/changes"
	"github.com/oops1/gogit/internal/ui/commit"
	"github.com/oops1/gogit/internal/ui/diffview"
	"github.com/oops1/gogit/internal/ui/filesgrid"
	"github.com/oops1/gogit/internal/ui/journal"
	"github.com/oops1/gogit/internal/ui/panetitle"
	"github.com/oops1/gogit/internal/ui/repos"
	"github.com/oops1/gogit/internal/ui/settings"
)

const shortHashLength = 7

const targetFPS = 30

const systemThemePoll = 3 * time.Second

var ErrWidgetMissing = errors.New("app: named widget missing")

type App struct {
	mu       sync.Mutex
	cfg      *config.Config
	paths    config.Paths
	eng      *engine.Engine
	root     *widget.Window
	named    map[string]widget.Widget
	menu     *widget.MenuBar
	state    State
	handlers map[CommandID]func()
	OnExit   func()
	langID   int
	detect   func() systheme.Scheme
	log      *slog.Logger

	layoutStore   layout.Store
	defaultLayout []byte

	languages []string

	registry          *repo.Registry
	reposView         *repos.View
	branchesView      *branches.View
	journalView       *journal.View
	diffView          *diffview.DiffView
	filesGrid         *filesgrid.Grid
	filesFilterInput  *widget.TextInput
	filesFilterLabel  *widget.Label
	statusLabel       *widget.Label
	statusBranchLabel *widget.Label
	selectedNode      string
	selectedCommit    hash.ObjectID
	askInput          func(title, prompt string, cb func(text string, ok bool))
	askConfirm        func(title, message string, cb func(ok bool))
	showAddRepo       func(initial addrepo.Request, cb func(addrepo.Result, bool))
	showSettings      func(initial settings.Model, cb func(settings.Model, bool))
	showCommit        func(initial commit.Model, cb func(commit.Model, bool))

	open *openedRepository

	newWatcher func(gitrepo.Layout, watch.Options) watcherIface

	writeMu     sync.Mutex
	writeCancel context.CancelFunc
	writeWG     sync.WaitGroup

	watchMu     sync.Mutex
	watchCancel context.CancelFunc
	watchWG     sync.WaitGroup
	watcher     watcherIface

	journalMu       sync.Mutex
	journalCancel   context.CancelFunc
	journalMore     chan struct{}
	journalWG       sync.WaitGroup
	journalPageSize int

	filesItems *datagrid.ObservableCollection

	diffMu     sync.Mutex
	diffCancel context.CancelFunc
	diffWG     sync.WaitGroup

	workingMu     sync.Mutex
	workingCancel context.CancelFunc
	workingWG     sync.WaitGroup

	filesMu            sync.Mutex
	filesMode          filesMode
	currentFiles       []diff.File
	currentEntries     []worktree.Entry
	commitSelected     bool
	filesAllRows       []changes.Row
	filesFilterQuery   string
	filesStatusAllowed map[changes.StatusFilter]bool
	activeModified     bool
	stagedCount        int

	postCh   chan func()
	postStop chan struct{}
	postWG   sync.WaitGroup

	closeOnce sync.Once
}

func New(cfg *config.Config, paths config.Paths, log *slog.Logger) (*App, error) {
	return NewFromXAML(cfg, paths, assets.MainWindow(), log)
}

func NewFromXAML(cfg *config.Config, paths config.Paths, xaml []byte, log *slog.Logger) (*App, error) {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	cat, err := i18n.Install(paths.UserI18NDir())
	if err != nil {
		return nil, err
	}
	i18n.Apply(cfg.Language)
	diffview.Register()
	filesgrid.Register()

	rootWidget, named, err := widget.LoadUIFromXAML(xaml)
	if err != nil {
		return nil, err
	}
	root, ok := rootWidget.(*widget.Window)
	if !ok {
		return nil, fmt.Errorf("app: root is %T, want *widget.Window", rootWidget)
	}
	menu, ok := named["mainMenu"].(*widget.MenuBar)
	if !ok {
		return nil, fmt.Errorf("%w: mainMenu", ErrWidgetMissing)
	}
	for _, name := range []string{"reposTree", "branchesTree", "statusText", "statusBranch", "statusProgress"} {
		if _, ok := named[name]; !ok {
			return nil, fmt.Errorf("%w: %s", ErrWidgetMissing, name)
		}
	}
	reposTreeWidget, ok := named["reposTree"].(*widget.TreeViewWidget)
	if !ok {
		return nil, fmt.Errorf("%w: reposTree", ErrWidgetMissing)
	}
	branchesTreeWidget, ok := named["branchesTree"].(*widget.TreeViewWidget)
	if !ok {
		return nil, fmt.Errorf("%w: branchesTree", ErrWidgetMissing)
	}
	statusTextWidget, ok := named["statusText"].(*widget.Label)
	if !ok {
		return nil, fmt.Errorf("%w: statusText", ErrWidgetMissing)
	}
	statusBranchWidget, ok := named["statusBranch"].(*widget.Label)
	if !ok {
		return nil, fmt.Errorf("%w: statusBranch", ErrWidgetMissing)
	}
	if _, ok := named["dock"].(*widget.DockManager); !ok {
		return nil, fmt.Errorf("%w: dock", ErrWidgetMissing)
	}
	for _, name := range toolbarButtons {
		if _, ok := named[name].(*widget.Button); !ok {
			return nil, fmt.Errorf("%w: %s", ErrWidgetMissing, name)
		}
	}
	for name := range gridColumnKeys {
		if _, ok := named[name].(*widget.DataGridWidget); !ok {
			return nil, fmt.Errorf("%w: %s", ErrWidgetMissing, name)
		}
	}
	filesGridWidget, ok := named["filesGrid"].(*filesgrid.Grid)
	if !ok {
		return nil, fmt.Errorf("%w: filesGrid", ErrWidgetMissing)
	}
	filesFilterWidget, ok := named["filesFilter"].(*widget.TextInput)
	if !ok {
		return nil, fmt.Errorf("%w: filesFilter", ErrWidgetMissing)
	}
	filesFilterCountWidget, ok := named["filesFilterCount"].(*widget.Label)
	if !ok {
		return nil, fmt.Errorf("%w: filesFilterCount", ErrWidgetMissing)
	}
	filesFilterCountWidget.TextAlign = widget.TextAlignRight
	for _, name := range filesStatusButtons {
		if _, ok := named[name].(*widget.Button); !ok {
			return nil, fmt.Errorf("%w: %s", ErrWidgetMissing, name)
		}
	}
	diffWidget, ok := named["diffView"].(*diffview.DiffView)
	if !ok {
		return nil, fmt.Errorf("%w: diffView", ErrWidgetMissing)
	}

	a := &App{
		cfg:               cfg,
		paths:             paths,
		root:              root,
		named:             named,
		menu:              menu,
		handlers:          map[CommandID]func(){},
		detect:            systheme.Detect,
		log:               log,
		languages:         cat.Codes(),
		statusLabel:       statusTextWidget,
		statusBranchLabel: statusBranchWidget,
		registry:          repo.New(cfg),
		diffView:          diffWidget,
		filesGrid:         filesGridWidget,
		filesFilterInput:  filesFilterWidget,
		filesFilterLabel:  filesFilterCountWidget,
		newWatcher:        newRealWatcher,
		journalPageSize:   defaultJournalPageSize,
	}
	a.startPostQueue()
	root.MinWidth = config.MinWindowWidth
	root.MinHeight = config.MinWindowHeight
	root.Title = i18n.T("App.Title")

	a.eng = engine.New(cfg.Window.Width, cfg.Window.Height, targetFPS)
	a.eng.SetRoot(root)
	a.askInput = func(title, prompt string, cb func(text string, ok bool)) {
		widget.NewMessageBox(a.eng).ShowInput(title, prompt, "", nil, cb)
	}
	a.askConfirm = func(title, message string, cb func(ok bool)) {
		widget.NewMessageBox(a.eng).ShowQuestion(title, message, func(r widget.MessageBoxResult) {
			cb(r == widget.MBResultYes)
		})
	}
	a.showAddRepo = a.defaultShowAddRepo
	a.showSettings = a.defaultShowSettings
	a.showCommit = a.defaultShowCommit
	a.applyDockSizes()
	a.defaultLayout = a.Dock().SaveLayout()
	_ = a.RestoreLayout()

	a.reposView = repos.NewView()
	a.reposView.Bind(reposTreeWidget)
	a.reposView.OnActivate = a.ActivateRepository
	a.reposView.OnSelect = func(id string) { a.selectedNode = id }
	a.branchesView = branches.NewView()
	a.branchesView.Bind(branchesTreeWidget)
	a.journalView = journal.NewView()
	a.journalView.Bind(a.named["journalGrid"].(*widget.DataGridWidget))
	a.journalView.OnSelect = a.onJournalRowSelected
	a.journalView.OnNearEnd = a.requestMoreJournal
	a.filesItems = datagrid.NewObservableCollection()
	a.filesGrid.SetItemsSource(a.filesItems)
	a.filesGrid.Data().Grid.SelectionMode = datagrid.SelectionExtended
	a.filesGrid.SetOnSelectionChanged(a.onFilesRowSelected)
	a.restoreFilesColumns()
	a.filesGrid.OnColumnsChanged = a.saveFilesColumns
	a.restoreFilesStatusFilter()
	a.wireFilesStatusButtons()
	a.filesFilterInput.OnChange = a.onFilesFilterChanged
	a.applyFilesFilter()
	a.restoreActiveRepository()
	a.applyTheme()
	a.updateStatusText()

	a.wireMenuBar()
	a.wireToolbar()
	a.retranslateGrids()
	a.wireViewHandlers()
	a.applyMenuTexts(viewMenuIndex)
	a.logLanguageMenuLimit()
	a.wireHotkeys()
	a.handlers[CmdClose] = a.exit
	a.handlers[CmdCloseRepository] = a.CloseRepository
	a.handlers[CmdAddGroup] = a.addGroup
	a.handlers[CmdAddOrCreate] = a.addOrCreateRepository
	a.handlers[CmdResetLayout] = func() { _ = a.ResetLayout() }
	a.handlers[CmdRefresh] = a.RefreshRepository
	a.handlers[CmdSettings] = a.openSettings
	a.handlers[CmdStage] = a.stageSelected
	a.handlers[CmdUnstage] = a.unstageSelected
	a.handlers[CmdDiscard] = a.discardSelected
	a.handlers[CmdCommit] = a.openCommit
	a.langID = widget.AddLanguageListener(func(string) { a.retranslate() })
	a.refreshCommands()
	a.log.Debug("app started", "language", cfg.Language, "theme", cfg.Theme)
	return a, nil
}

func (a *App) Engine() *engine.Engine { return a.eng }

func (a *App) Root() *widget.Window { return a.root }

func (a *App) Widget(name string) widget.Widget { return a.named[name] }

func (a *App) DiffView() *diffview.DiffView { return a.diffView }

func (a *App) Config() *config.Config { return a.cfg }

func (a *App) State() State {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.state
}

func (a *App) SetHandler(id CommandID, fn func()) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.handlers[id] = fn
}

func (a *App) Dispatch(id CommandID) bool {
	a.mu.Lock()
	fn := a.handlers[id]
	enabled := a.state.Enabled(id)
	a.mu.Unlock()
	if fn == nil || !enabled {
		return false
	}
	a.log.Debug("command dispatched", "command", string(id))
	fn()
	return true
}

func (a *App) SetActiveRepository(id string, worktree bool) {
	a.mu.Lock()
	a.state = State{ActiveRepository: id, ActiveIsWorktree: worktree}
	a.mu.Unlock()
	a.refreshCommands()
}

func (a *App) setFilesSelected(v bool) {
	a.mu.Lock()
	changed := a.state.FilesSelected != v
	a.state.FilesSelected = v
	a.mu.Unlock()
	if changed {
		a.refreshCommands()
	}
}

func (a *App) setHasStagedChanges(v bool) {
	a.mu.Lock()
	changed := a.state.HasStagedChanges != v
	a.state.HasStagedChanges = v
	a.mu.Unlock()
	if changed {
		a.refreshCommands()
	}
}

func (a *App) CloseRepository() {
	a.stopWatcher()
	a.stopJournal()
	a.clearChangesPanels()
	a.closeOpenRepository()
	a.registry.ClearActive()
	a.cfg.ActiveRepository = ""
	a.SetActiveRepository("", false)
	a.updateStatusText()
	a.statusBranchLabel.SetText("")
	a.branchesView.Render(branches.Snapshot{})
	a.journalView.Reset()
	a.reposView.Render(a.registry, a.repoTreeState())
}

func (a *App) ActivateRepository(id string) {
	node, ok := a.registry.Find(id)
	if !ok || node.Kind == repo.KindGroup {
		return
	}
	opened, snap, err := openRepositoryAt(id, node.Path)
	if err != nil {
		a.log.Warn("open repository failed", "path", node.Path, "error", err)
		a.stopWatcher()
		a.stopJournal()
		a.clearChangesPanels()
		a.closeOpenRepository()
		a.registry.ClearActive()
		a.cfg.ActiveRepository = ""
		a.SetActiveRepository("", false)
		a.statusLabel.SetText(i18n.Tf("Status.OpenFailed", err))
		a.statusBranchLabel.SetText("")
		a.branchesView.Render(branches.Snapshot{})
		a.journalView.Reset()
		a.reposView.Render(a.registry, a.repoTreeState())
		return
	}
	a.stopWatcher()
	a.stopJournal()
	a.clearChangesPanels()
	a.closeOpenRepository()
	a.open = opened
	_ = a.registry.SetActive(id)
	a.cfg.ActiveRepository = id
	a.SetActiveRepository(id, node.Kind == repo.KindWorktree)
	a.updateStatusText()
	a.branchesView.Render(snap)
	a.statusBranchLabel.SetText(branchStatusText(snap))
	a.reposView.Render(a.registry, a.repoTreeState())
	a.startWatcher(opened.repo.Layout())
	a.startJournal()
	a.startWorking()
}

func (a *App) closeOpenRepository() {
	if a.open == nil {
		return
	}
	if err := a.open.close(); err != nil {
		a.log.Warn("close repository failed", "path", a.open.path, "error", err)
	}
	a.open = nil
}

func (a *App) RefreshRepository() {
	if a.open == nil {
		return
	}
	snap, err := loadBranchSnapshot(a.open.store)
	if err != nil {
		a.log.Warn("refresh repository failed", "path", a.open.path, "error", err)
		a.statusLabel.SetText(i18n.Tf("Status.OpenFailed", err))
		return
	}
	a.branchesView.Render(snap)
	a.statusBranchLabel.SetText(branchStatusText(snap))
	a.startJournal()
	if a.commitSelected {
		return
	}
	a.startWorking()
}

func branchStatusText(snap branches.Snapshot) string {
	if snap.Detached {
		return i18n.T("Pane.Branches.Detached") + " " + shortHash(snap.HeadID)
	}
	return snap.Current
}

func shortHash(id hash.ObjectID) string {
	return id.String()[:shortHashLength]
}

func (a *App) restoreActiveRepository() {
	if a.cfg.ActiveRepository == "" {
		return
	}
	node, ok := a.registry.Find(a.cfg.ActiveRepository)
	if !ok || node.Kind == repo.KindGroup {
		return
	}
	_ = a.registry.SetActive(a.cfg.ActiveRepository)
	a.SetActiveRepository(a.cfg.ActiveRepository, node.Kind == repo.KindWorktree)
}

func (a *App) updateStatusText() {
	if node, ok := a.registry.Active(); ok {
		a.statusLabel.SetText(node.Name)
		return
	}
	a.statusLabel.SetText(i18n.T("Status.NoRepository"))
}

func (a *App) addGroup() {
	title := i18n.T("Dialog.AddGroup.Title")
	prompt := i18n.T("Dialog.AddGroup.Prompt")
	a.askInput(title, prompt, func(text string, ok bool) {
		if !ok {
			return
		}
		name := strings.TrimSpace(text)
		if name == "" {
			return
		}
		if _, err := a.registry.AddGroup(name, a.groupParentForNewGroup()); err != nil {
			a.log.Warn("add group failed", "error", err)
			return
		}
		a.reposView.Render(a.registry, a.repoTreeState())
		if err := a.cfg.Save(a.paths.ConfigFile()); err != nil {
			a.log.Warn("save config failed", "error", err)
		}
	})
}

func (a *App) addOrCreateRepository() {
	a.showAddRepo(addrepo.Request{}, func(result addrepo.Result, ok bool) {
		if !ok {
			return
		}
		node, err := a.registry.AddRepository(result.Name, result.Path, a.groupParentForNewGroup())
		if err != nil {
			a.log.Warn("add repository failed", "error", err)
			return
		}
		if err := a.cfg.Save(a.paths.ConfigFile()); err != nil {
			a.log.Warn("save config failed", "error", err)
		}
		a.reposView.Render(a.registry, a.repoTreeState())
		a.ActivateRepository(node.ID)
	})
}

var newAddRepoView = addrepo.NewView

func (a *App) defaultShowAddRepo(initial addrepo.Request, cb func(addrepo.Result, bool)) {
	view, err := newAddRepoView(a.eng, initial)
	if err != nil {
		a.log.Warn("open add repository dialog failed", "error", err)
		return
	}
	a.wireAddRepoView(view, cb)
	a.eng.ShowModal(view.Dialog())
}

func (a *App) wireAddRepoView(view *addrepo.View, cb func(addrepo.Result, bool)) {
	view.OnOK = func(req addrepo.Request) {
		a.eng.CloseModal(view.Dialog())
		result, err := addrepo.Apply(req)
		if err != nil {
			a.log.Warn("create repository failed", "error", err)
			return
		}
		cb(result, true)
	}
	view.OnCancel = func() {
		a.eng.CloseModal(view.Dialog())
		cb(addrepo.Result{}, false)
	}
}

func (a *App) groupParentForNewGroup() string {
	node, ok := a.registry.Find(a.selectedNode)
	if !ok || node.Kind != repo.KindGroup {
		return ""
	}
	return a.selectedNode
}

func (a *App) SetTheme(name string) {
	a.cfg.Theme = name
	a.cfg.Normalize()
	a.applyTheme()
	a.log.Debug("theme changed", "theme", a.cfg.Theme)
}

func (a *App) SetSystemThemeDetector(fn func() systheme.Scheme) {
	a.mu.Lock()
	a.detect = fn
	a.mu.Unlock()
	a.applyTheme()
}

func (a *App) EffectiveTheme() string {
	a.mu.Lock()
	detect := a.detect
	a.mu.Unlock()
	return effectiveTheme(a.cfg.Theme, detect)
}

func (a *App) applyTheme() {
	theme := themeFor(a.EffectiveTheme())
	a.eng.SetTheme(theme)
	a.applyToolbarIcons(theme)
	a.applyFilesStatusButtonVisuals(theme)
	a.applyRepoTreeTheme(theme)
	a.applyPaneTitleColors()
}

func (a *App) applyPaneTitleColors() {
	panetitle.Apply(a.Dock().Panes())
}

func (a *App) applyRepoTreeTheme(t *widget.Theme) {
	a.reposView.SetAccent(t.Accent)
	a.reposView.Render(a.registry, a.repoTreeState())
}

func (a *App) FollowSystemTheme(ctx context.Context) {
	systheme.Watch(ctx, systemThemePoll, a.OnSystemThemeChanged)
}

func (a *App) OnSystemThemeChanged(systheme.Scheme) bool {
	if a.cfg.Theme != config.ThemeSystem {
		return false
	}
	a.applyTheme()
	return true
}

func effectiveTheme(name string, detect func() systheme.Scheme) string {
	if name != config.ThemeSystem {
		return name
	}
	if detect() == systheme.Light {
		return config.ThemeLight
	}
	return config.ThemeDark
}

func (a *App) SetLanguage(code string) {
	a.cfg.Language = code
	i18n.Apply(code)
	a.log.Debug("language changed", "language", code)
}

func (a *App) Run() error {
	if err := a.paths.Ensure(); err != nil {
		return err
	}
	a.eng.Start()
	defer a.eng.Stop()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go a.FollowSystemTheme(ctx)
	win := window.New(a.eng, a.root.Title)
	if a.OnExit == nil {
		a.OnExit = win.Close
	}
	err := win.Run()
	if saveErr := a.SaveLayout(); err == nil {
		err = saveErr
	}
	return err
}

func (a *App) Close() {
	a.closeOnce.Do(func() {
		a.stopWatcher()
		a.stopJournal()
		a.stopDiff()
		a.stopWorking()
		a.stopWrite()
		close(a.postStop)
		a.postWG.Wait()
		a.closeOpenRepository()
		widget.RemoveLanguageListener(a.langID)
	})
}

func (a *App) exit() {
	if a.OnExit != nil {
		a.OnExit()
	}
}

func (a *App) Dock() *widget.DockManager {
	return a.named["dock"].(*widget.DockManager)
}

func (a *App) applyDockSizes() {
	dock := a.Dock()
	for side, size := range dockSideSizes {
		dock.SetSideSize(side, size)
	}
}

func themeFor(name string) *widget.Theme {
	if name == config.ThemeLight {
		return widget.Win11LightTheme()
	}
	return widget.Win11DarkTheme()
}
