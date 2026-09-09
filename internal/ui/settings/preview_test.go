package settings

import (
	"image"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/oops1/headless-gui/v3/engine"
	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/config"
	"github.com/oops1/gogit/internal/i18n"
)

type previewVariant struct {
	name      string
	query     string
	lang      string
	theme     *widget.Theme
	width     int
	height    int
	advanced  bool
	collapsed bool
}

func previewVariants() []previewVariant {
	return []previewVariant{
		{"dark", "", "en", widget.Win11DarkTheme(), dialogDefaultWidth, dialogDefaultHeight, false, false},
		{"light", "", "en", widget.Win11LightTheme(), dialogDefaultWidth, dialogDefaultHeight, false, false},
		{"ru", "", "ru", widget.Win11LightTheme(), dialogDefaultWidth, dialogDefaultHeight, false, false},
		{"ru-narrow", "", "ru", widget.Win11LightTheme(), dialogMinWidth, dialogDefaultHeight, false, false},
		{"wide", "", "en", widget.Win11LightTheme(), 1200, dialogDefaultHeight, false, false},
		{"advanced", "", "ru", widget.Win11LightTheme(), dialogMinWidth, dialogDefaultHeight, true, false},
		{"collapsed", "", "ru", widget.Win11LightTheme(), dialogDefaultWidth, dialogDefaultHeight, false, true},
		{"search", "показывать", "ru", widget.Win11LightTheme(), dialogDefaultWidth, dialogDefaultHeight, false, false},
		{"nomatch", "zzz", "ru", widget.Win11LightTheme(), dialogDefaultWidth, dialogDefaultHeight, false, false},
	}
}

func TestPreviewSettingsDialog(t *testing.T) {
	dir := os.Getenv("GOGIT_PREVIEW_DIR")
	if dir == "" {
		t.Skip("GOGIT_PREVIEW_DIR not set")
	}
	sections := []string{"general", "git", "credentials", "ssh"}
	for _, variant := range previewVariants() {
		for _, section := range sections {
			renderPreviewFrames(t, dir, variant, section)
		}
	}
}

func renderPreviewFrames(t *testing.T, dir string, variant previewVariant, section string) {
	t.Helper()
	widget.ClearStrings()
	defer widget.ClearStrings()
	if _, err := i18n.Install(""); err != nil {
		t.Fatal(err)
	}
	i18n.Apply(variant.lang)

	canvasWidth, canvasHeight := variant.width+previewCanvasMargin, variant.height+previewCanvasMargin
	eng := engine.New(canvasWidth, canvasHeight, 30)
	eng.SetTheme(variant.theme)
	root := widget.NewPanel(variant.theme.WindowBG)
	root.SetBounds(image.Rect(0, 0, canvasWidth, canvasHeight))
	eng.SetRoot(root)

	view, err := NewView(eng, []string{"en", "ru"}, Model{
		Language:      variant.lang,
		Theme:         config.ThemeDark,
		ShowToolbar:   true,
		ShowStatusBar: true,
		LogMaxCount:   500,
		AutoFetch:     true,
		FetchInterval: 300,
		DefaultRemote: "origin",
	})
	if err != nil {
		t.Fatal(err)
	}
	view.SetCredentials(goldenSampleCredentials())
	view.SetKeys(goldenSampleKeys())
	view.SetCredentialSourceInfo(
		`C:\Users\valer\AppData\Roaming\GoGit\vault.db`,
		"Argon2id, AES-256-GCM",
		[]CredentialHelperEntry{{Name: "manager-core", Supported: true}},
	)
	view.SetSection(section)
	view.gitAdvanced.SetExpanded(variant.advanced)
	if variant.collapsed {
		view.Dialog().SetNavCollapsed(true)
	}
	view.Dialog().Resize(variant.width, variant.height)
	if variant.query != "" {
		view.search.SetText(variant.query)
		view.applySearch(variant.query)
	}
	eng.ShowModal(view.Dialog())
	view.Restyle(variant.theme)

	eng.SaveFrames(dir + "/settings-" + section + "-" + variant.name + "-" + strconv.Itoa(variant.width))
	eng.Start()
	time.Sleep(700 * time.Millisecond)
	eng.Stop()
}

func TestPreviewSecretEditors(t *testing.T) {
	dir := os.Getenv("GOGIT_PREVIEW_DIR")
	if dir == "" {
		t.Skip("GOGIT_PREVIEW_DIR not set")
	}
	for _, lang := range []string{"ru", "en"} {
		widget.ClearStrings()
		if _, err := i18n.Install(""); err != nil {
			t.Fatal(err)
		}
		i18n.Apply(lang)

		theme := widget.Win11LightTheme()
		eng := engine.New(560, 340, 30)
		eng.SetTheme(theme)
		root := widget.NewPanel(theme.WindowBG)
		root.SetBounds(image.Rect(0, 0, 560, 340))
		eng.SetRoot(root)

		view, err := NewView(eng, []string{"en", "ru"}, Model{})
		if err != nil {
			t.Fatal(err)
		}
		view.openCredentialEditor(SecretEntry{}, false)
		eng.SaveFrames(dir + "/editor-credential-" + lang)
		eng.Start()
		time.Sleep(400 * time.Millisecond)
		eng.Stop()

		eng2 := engine.New(560, 340, 30)
		eng2.SetTheme(theme)
		root2 := widget.NewPanel(theme.WindowBG)
		root2.SetBounds(image.Rect(0, 0, 560, 340))
		eng2.SetRoot(root2)
		view2, err := NewView(eng2, []string{"en", "ru"}, Model{})
		if err != nil {
			t.Fatal(err)
		}
		view2.openKeyEditor(KeyEntry{}, false)
		eng2.SaveFrames(dir + "/editor-key-" + lang)
		eng2.Start()
		time.Sleep(400 * time.Millisecond)
		eng2.Stop()
		widget.ClearStrings()
	}
}
