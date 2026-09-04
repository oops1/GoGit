package app

import (
	"os"
	"testing"

	"github.com/oops1/gogit/internal/config"
	"github.com/oops1/gogit/internal/ui/changes"
	"github.com/oops1/gogit/internal/ui/icons"
)

func TestSubdirectoriesButtonStartsPressedAndKeepsItsFullColourIcon(t *testing.T) {
	a := newTestApp(t)
	btn := a.filesSubdirsButton()
	if !a.cfg.UI.FilesSubdirectories {
		t.Fatal("files from subdirectories must be shown by default")
	}
	if btn.Background.A == 0 {
		t.Fatal("a pressed button must carry a background")
	}
	if !sameImage(btn.Icon, icons.ToolbarPlain(filesSubdirsIcon, filesStatusIconSize)) {
		t.Fatal("a pressed button must show the full colour icon")
	}
}

func TestTogglingSubdirectoriesHidesNestedRowsAndSurvivesRestart(t *testing.T) {
	paths := config.Paths{Dir: t.TempDir()}
	cfg := config.Default()
	a, err := New(cfg, paths, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)

	a.setFilesRows([]changes.Row{
		{Name: "main.go", RelDir: "src", Status: changes.RowModified},
		{Name: "deep.go", RelDir: "src/pkg", Status: changes.RowModified},
	})
	a.setFilesDirFilter("src")
	if got := len(a.filesItems.Items()); got != 2 {
		t.Fatalf("visible rows = %d, want both files", got)
	}

	a.toggleFilesSubdirectories()
	if got := len(a.filesItems.Items()); got != 1 {
		t.Fatalf("visible rows = %d, want only the file directly inside src", got)
	}
	btn := a.filesSubdirsButton()
	if btn.Background.A != 0 {
		t.Fatal("a released button must not carry a background")
	}
	if !sameImage(btn.Icon, icons.ToolbarMuted(filesSubdirsIcon, filesStatusIconSize)) {
		t.Fatal("a released button must show the muted icon")
	}

	stored, err := config.Load(paths.ConfigFile())
	if err != nil {
		t.Fatal(err)
	}
	if stored.UI.FilesSubdirectories {
		t.Fatal("the released state must reach the config file")
	}

	second, err := New(stored, paths, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(second.Close)
	if second.filesSubdirsButton().Background.A != 0 {
		t.Fatal("a restarted app must keep the button released")
	}
}

func TestTogglingSubdirectoriesSurvivesAConfigThatCannotBeSaved(t *testing.T) {
	a, paths := newTestAppWithPaths(t)
	if err := writeFile(paths.Dir, "config.toml.tmp", ""); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(paths.ConfigFile() + ".tmp"); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(paths.ConfigFile()+".tmp", 0o700); err != nil {
		t.Fatal(err)
	}

	a.toggleFilesSubdirectories()

	if a.cfg.UI.FilesSubdirectories {
		t.Fatal("the toggle must still flip when the config cannot be written")
	}
}
