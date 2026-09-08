package app

import (
	"image"
	"testing"

	"github.com/oops1/gogit/internal/assets"
)

func everyMenuCommand() []CommandID {
	commands := []CommandID{}
	for _, def := range menuBarDefs {
		for _, entry := range def.Tree {
			if entry.Leaf != nil {
				commands = append(commands, entry.Leaf.Command)
			}
		}
	}
	return commands
}

func TestEveryMenuCommandHasAPicture(t *testing.T) {
	for _, id := range everyMenuCommand() {
		if commandIcons[id] == "" {
			t.Fatalf("command %q has no icon", id)
		}
		if commandIcon(id) == nil {
			t.Fatalf("the icon of %q does not render", id)
		}
	}
}

func TestEveryMenuGroupAndContextItemHasAPicture(t *testing.T) {
	for _, def := range menuBarDefs {
		for _, entry := range def.Tree {
			if entry.Group == nil {
				continue
			}
			if menuKeyIcon(entry.Group.Key) == nil {
				t.Fatalf("group %q has no icon", entry.Group.Key)
			}
		}
	}
	for key := range contextIcons {
		if menuKeyIcon(key) == nil {
			t.Fatalf("context item %q has no icon", key)
		}
	}
}

func TestMenuPicturesAreDrawnAtTheRequestedSize(t *testing.T) {
	img := commandIcon(CmdCommit)
	if img == nil {
		t.Fatal("the commit icon must render")
	}
	if got := img.Bounds(); got != image.Rect(0, 0, menuIconSize, menuIconSize) {
		t.Fatalf("bounds = %v, want a %dx%d icon", got, menuIconSize, menuIconSize)
	}
}

func TestACommandWithoutAPictureGetsNone(t *testing.T) {
	if menuIcon("") != nil {
		t.Fatal("an empty name must give no picture")
	}
	if commandIcon("no.such.command") != nil {
		t.Fatal("an unknown command must give no picture")
	}
	if menuKeyIcon("No.Such.Key") != nil {
		t.Fatal("an unknown key must give no picture")
	}
}

func TestEveryDrawnMenuPictureIsUsed(t *testing.T) {
	used := map[string]bool{}
	for _, name := range commandIcons {
		used[name] = true
	}
	for _, name := range menuGroupIcons {
		used[name] = true
	}
	for _, name := range contextIcons {
		used[name] = true
	}
	for _, name := range assets.MenuIconNames() {
		if !used[name] {
			t.Fatalf("icon %q is drawn but no menu shows it", name)
		}
	}
	for _, name := range []string{"commit", "pull", "push", "sync"} {
		if !used[name] {
			t.Fatalf("the toolbar icon %q must serve its menu item too", name)
		}
	}
}
