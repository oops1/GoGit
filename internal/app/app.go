package app

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/oops1/headless-gui/v3/engine"
	"github.com/oops1/headless-gui/v3/widget"
	"github.com/oops1/headless-gui/v3/window"

	"github.com/oops1/gogit/internal/assets"
	"github.com/oops1/gogit/internal/config"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/systheme"
)

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
}

func New(cfg *config.Config, paths config.Paths) (*App, error) {
	return NewFromXAML(cfg, paths, assets.MainWindow())
}

func NewFromXAML(cfg *config.Config, paths config.Paths, xaml []byte) (*App, error) {
	if _, err := i18n.Install(paths.UserI18NDir()); err != nil {
		return nil, err
	}
	i18n.Apply(cfg.Language)

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

	a := &App{
		cfg:      cfg,
		paths:    paths,
		root:     root,
		named:    named,
		menu:     menu,
		handlers: map[CommandID]func(){},
		detect:   systheme.Detect,
	}
	root.MinWidth = config.MinWindowWidth
	root.MinHeight = config.MinWindowHeight
	root.Title = i18n.T("App.Title")

	a.eng = engine.New(cfg.Window.Width, cfg.Window.Height, targetFPS)
	a.eng.SetRoot(root)
	a.applyDockSizes()
	a.applyTheme()

	a.wireMenu()
	a.wireToolbar()
	a.retranslateGrids()
	a.handlers[CmdClose] = a.exit
	a.handlers[CmdCloseRepository] = a.CloseRepository
	a.langID = widget.AddLanguageListener(func(string) { a.retranslate() })
	a.refreshCommands()
	return a, nil
}

func (a *App) Engine() *engine.Engine { return a.eng }

func (a *App) Root() *widget.Window { return a.root }

func (a *App) Widget(name string) widget.Widget { return a.named[name] }

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
	fn()
	return true
}

func (a *App) SetActiveRepository(id string, worktree bool) {
	a.mu.Lock()
	a.state = State{ActiveRepository: id, ActiveIsWorktree: worktree}
	a.mu.Unlock()
	a.refreshCommands()
}

func (a *App) CloseRepository() {
	a.SetActiveRepository("", false)
}

func (a *App) SetTheme(name string) {
	a.cfg.Theme = name
	a.cfg.Normalize()
	a.applyTheme()
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
	a.eng.SetTheme(themeFor(a.EffectiveTheme()))
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
	if saveErr := a.cfg.Save(a.paths.ConfigFile()); err == nil {
		err = saveErr
	}
	return err
}

func (a *App) Close() {
	widget.RemoveLanguageListener(a.langID)
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
