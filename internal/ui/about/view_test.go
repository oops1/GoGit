package about

import (
	"errors"
	"image"
	"runtime"
	"testing"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/i18n"
)

func newTestView(t *testing.T, info Info) *View {
	t.Helper()
	widget.ClearStrings()
	t.Cleanup(widget.ClearStrings)
	if _, err := i18n.Install(""); err != nil {
		t.Fatal(err)
	}
	i18n.Apply("en")
	view, err := NewView(info)
	if err != nil {
		t.Fatal(err)
	}
	return view
}

func TestViewShowsTheBuildItRunsIn(t *testing.T) {
	v := newTestView(t, Info{Version: "v1.1.0", Architecture: "arm64"})

	if got := v.version.Text(); got != i18n.Tf("Dialog.About.Version", "1.1.0") {
		t.Fatalf("version = %q", got)
	}
	if got := v.platform.Text(); got != i18n.T("Platform.supported") {
		t.Fatalf("platform = %q", got)
	}
	if got := v.architecture.Text(); got != "ARM64" {
		t.Fatalf("architecture = %q", got)
	}
	if got := v.gitEngine.Text(); got != gitEngineName {
		t.Fatalf("git engine = %q", got)
	}
	if got := v.guiEngine.Text(); got != guiEngineName {
		t.Fatalf("gui engine = %q", got)
	}
	if got := v.copyright.Text(); got != i18n.Tf("Dialog.About.Copyright", copyrightYear) {
		t.Fatalf("copyright = %q", got)
	}
	if v.logo.Image() == nil {
		t.Fatal("the dialog must show the application icon")
	}
}

func TestUnknownPlatformAndArchitectureFallBackToTheirGoNames(t *testing.T) {
	v := newTestView(t, Info{Version: "dev", Architecture: "riscv64"})

	if got := v.architecture.Text(); got != "riscv64" {
		t.Fatalf("architecture = %q, want the raw GOARCH", got)
	}
}

func TestKnownArchitecturesGetTheirDesktopNames(t *testing.T) {
	for goarch, want := range map[string]string{"amd64": "x64", "386": "x86", "arm64": "ARM64"} {
		if got := architectureLabel(goarch); got != want {
			t.Fatalf("architectureLabel(%q) = %q, want %q", goarch, got, want)
		}
	}
}

func TestCurrentInfoDescribesTheRunningBuild(t *testing.T) {
	info := CurrentInfo("v9.9.9")

	if info.Version != "v9.9.9" {
		t.Fatalf("version = %q", info.Version)
	}
	if info.Architecture != runtime.GOARCH {
		t.Fatalf("info = %+v, want the running architecture", info)
	}
}

func TestButtonsCallTheirHandlers(t *testing.T) {
	v := newTestView(t, Info{Version: "v1.1.0"})
	var closed, github, license bool
	v.OnClose = func() { closed = true }
	v.OnGitHub = func() { github = true }
	v.OnLicense = func() { license = true }

	v.okBtn.OnClick()
	v.githubBtn.OnClick()
	v.licenseBtn.OnClick()

	if !closed || !github || !license {
		t.Fatalf("handlers fired: close=%v github=%v license=%v", closed, github, license)
	}
}

func TestButtonsAreSafeWithoutHandlers(t *testing.T) {
	v := newTestView(t, Info{Version: "v1.1.0"})

	v.okBtn.OnClick()
	v.githubBtn.OnClick()
	v.licenseBtn.OnClick()
	v.Dialog().CancelAction()
	v.Dialog().DefaultAction()
}

func TestNewViewPropagatesALoadFailure(t *testing.T) {
	prev := loadDialog
	loadDialog = func(string, string) (*widget.Dialog, map[string]widget.Widget, error) {
		return nil, nil, errors.New("boom")
	}
	t.Cleanup(func() { loadDialog = prev })

	if _, err := NewView(Info{}); err == nil {
		t.Fatal("a dialog that cannot be loaded must be reported")
	}
}

func fullNamedWidgets() map[string]widget.Widget {
	return map[string]widget.Widget{
		"platformLabel":     widget.NewWin10Label(""),
		"architectureLabel": widget.NewWin10Label(""),
		"gitEngineLabel":    widget.NewWin10Label(""),
		"guiEngineLabel":    widget.NewWin10Label(""),
		"logo":              widget.NewImageWidget(),
		"githubIcon":        widget.NewImageWidget(),
		"summaryFirst":      widget.NewWin10Label(""),
		"summarySecond":     widget.NewWin10Label(""),
		"tagline":           widget.NewWin10Label(""),
		"version":           widget.NewWin10Label(""),
		"platform":          widget.NewWin10Label(""),
		"architecture":      widget.NewWin10Label(""),
		"gitEngine":         widget.NewWin10Label(""),
		"guiEngine":         widget.NewWin10Label(""),
		"copyright":         widget.NewWin10Label(""),
		"github":            widget.NewButton(""),
		"license":           widget.NewButton(""),
		"ok":                widget.NewButton(""),
	}
}

func TestBindReportsEachMissingWidget(t *testing.T) {
	for key := range fullNamedWidgets() {
		named := fullNamedWidgets()
		delete(named, key)
		v := &View{}
		if err := v.bind(named); err == nil {
			t.Fatalf("missing %q: expected an error", key)
		} else if !errors.Is(err, ErrWidgetMissing) {
			t.Fatalf("missing %q: err = %v, want ErrWidgetMissing", key, err)
		}
	}
	v := &View{}
	if err := v.bind(fullNamedWidgets()); err != nil {
		t.Fatalf("a complete set must bind: %v", err)
	}
}

func TestDialogCarriesTheApplicationNameInItsTitle(t *testing.T) {
	v := newTestView(t, Info{Version: "v1.1.0"})

	if got := v.Dialog().Title; got != i18n.Tf("Dialog.About.Title", i18n.T("App.Title")) {
		t.Fatalf("title = %q", got)
	}
}

func TestNewViewReportsADialogMissingAWidget(t *testing.T) {
	prev := loadDialog
	loadDialog = func(_, title string) (*widget.Dialog, map[string]widget.Widget, error) {
		named := fullNamedWidgets()
		delete(named, "ok")
		return widget.NewDialog(title, 100, 100), named, nil
	}
	t.Cleanup(func() { loadDialog = prev })

	if _, err := NewView(Info{}); !errors.Is(err, ErrWidgetMissing) {
		t.Fatalf("err = %v, want ErrWidgetMissing", err)
	}
}

func TestTheVersionIsShownWithoutTheTagPrefix(t *testing.T) {
	for in, want := range map[string]string{"v1.2.0": "1.2.0", "1.2.0": "1.2.0", "dev": "dev"} {
		if got := displayVersion(in); got != want {
			t.Fatalf("displayVersion(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLinksAreDrawnFlatInTheAccentColour(t *testing.T) {
	v := newTestView(t, Info{Version: "v1.1.0"})
	theme := widget.Win11DarkTheme()

	v.Restyle(theme)

	if v.githubBtn.TextColor != theme.Accent {
		t.Fatalf("github colour = %v, want the accent", v.githubBtn.TextColor)
	}
	if v.githubBtn.Background.A != 0 || v.githubBtn.BorderColor.A != 0 {
		t.Fatal("a link must not be painted as a button")
	}
	if v.licenseBtn.TextColor != theme.SecondaryText {
		t.Fatalf("license colour = %v, want the secondary text", v.licenseBtn.TextColor)
	}
}

func TestTheOkButtonIsFilledWithAFaintAccentTint(t *testing.T) {
	v := newTestView(t, Info{Version: "v1.1.0"})
	theme := widget.Win11LightTheme()

	v.Restyle(theme)

	if v.okBtn.BorderColor != theme.Accent {
		t.Fatalf("border = %v, want the accent", v.okBtn.BorderColor)
	}
	if v.okBtn.Background == theme.BtnBG || v.okBtn.Background == theme.Accent {
		t.Fatalf("background = %v, want a tint between the button and the accent", v.okBtn.Background)
	}
	if v.okBtn.HoverBG == v.okBtn.Background {
		t.Fatal("hovering must deepen the tint")
	}
}

func TestTheGitHubMarkFollowsTheThemeText(t *testing.T) {
	v := newTestView(t, Info{Version: "v1.1.0"})

	v.Restyle(widget.Win11LightTheme())
	light := v.githubIcon.Image()
	v.Restyle(widget.Win11DarkTheme())
	dark := v.githubIcon.Image()

	if light == nil || dark == nil {
		t.Fatal("the mark must be rendered for both themes")
	}
	if !differ(light, dark) {
		t.Fatal("the mark must be recoloured with the theme text")
	}
}

func differ(a, b image.Image) bool {
	bounds := a.Bounds()
	if bounds != b.Bounds() {
		return true
	}
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if a.At(x, y) != b.At(x, y) {
				return true
			}
		}
	}
	return false
}
