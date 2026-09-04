package assets

import (
	"embed"
	"io/fs"
	"strings"
)

//go:embed ui/*.xaml ui/dialogs/*.xaml
var uiFS embed.FS

//go:embed i18n/*.json
var i18nFS embed.FS

//go:embed icons/*.svg icons/status/*.svg icons/tree/*.svg
var iconsFS embed.FS

const MainWindowXAML = "ui/main_window.xaml"

const dialogsDir = "ui/dialogs/"

var mainWindow, _ = uiFS.ReadFile(MainWindowXAML)

var i18nSub, _ = fs.Sub(i18nFS, "i18n")

func MainWindow() []byte {
	return mainWindow
}

func UI(name string) ([]byte, error) {
	return uiFS.ReadFile(name)
}

func Dialog(name string) ([]byte, error) {
	return uiFS.ReadFile(dialogsDir + name + ".xaml")
}

func I18N() fs.FS {
	return i18nSub
}

func Icon(name string) ([]byte, error) {
	return iconsFS.ReadFile("icons/" + name + ".svg")
}

func IconNames() []string {
	return svgNamesIn("icons")
}

func StatusIcon(name string) ([]byte, error) {
	return iconsFS.ReadFile("icons/status/" + name + ".svg")
}

func StatusIconNames() []string {
	return svgNamesIn("icons/status")
}

func TreeIcon(name string) ([]byte, error) {
	return iconsFS.ReadFile("icons/tree/" + name + ".svg")
}

func TreeIconNames() []string {
	return svgNamesIn("icons/tree")
}

func svgNamesIn(dir string) []string {
	entries, _ := fs.ReadDir(iconsFS, dir)
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		names = append(names, strings.TrimSuffix(e.Name(), ".svg"))
	}
	return names
}
