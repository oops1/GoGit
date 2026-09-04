package assets

import (
	"io/fs"
	"slices"
	"strings"
	"testing"
)

func TestMainWindowXAML(t *testing.T) {
	data, err := UI(MainWindowXAML)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "<Window") {
		t.Fatal("main window xaml has no Window root")
	}
	if string(MainWindow()) != string(data) {
		t.Fatal("MainWindow() differs from UI()")
	}
}

func TestUIMissing(t *testing.T) {
	if _, err := UI("ui/nope.xaml"); err == nil {
		t.Fatal("expected error")
	}
}

func TestDialog(t *testing.T) {
	data, err := Dialog("add_repo")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "<Grid") {
		t.Fatal("add_repo dialog xaml has no Grid root")
	}
}

func TestDialogMissing(t *testing.T) {
	if _, err := Dialog("nope"); err == nil {
		t.Fatal("expected error")
	}
}

func TestI18NFS(t *testing.T) {
	entries, err := fs.ReadDir(I18N(), ".")
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if !slices.Contains(names, "en.json") || !slices.Contains(names, "ru.json") {
		t.Fatalf("i18n files = %v", names)
	}
}

func TestIcons(t *testing.T) {
	names := IconNames()
	for _, want := range []string{"pull", "sync", "push", "commit", "app"} {
		if !slices.Contains(names, want) {
			t.Fatalf("icon %q missing in %v", want, names)
		}
		data, err := Icon(want)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(strings.TrimSpace(string(data)), "<svg") {
			t.Fatalf("icon %q is not svg", want)
		}
	}
	if _, err := Icon("missing"); err == nil {
		t.Fatal("expected error")
	}
}

func TestIconNamesExcludesSubdirectories(t *testing.T) {
	names := IconNames()
	if slices.Contains(names, "status") || slices.Contains(names, "tree") {
		t.Fatalf("icon names leaked subdirectories: %v", names)
	}
}

func TestStatusIcons(t *testing.T) {
	want := []string{
		"added", "conflict", "copied", "deleted", "ignored", "modified",
		"renamed", "staged", "typechanged", "unchanged", "untracked",
	}
	names := StatusIconNames()
	if len(names) != len(want) {
		t.Fatalf("StatusIconNames() = %v, want %v", names, want)
	}
	for _, name := range want {
		if !slices.Contains(names, name) {
			t.Fatalf("status icon %q missing in %v", name, names)
		}
		data, err := StatusIcon(name)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(strings.TrimSpace(string(data)), "<svg") {
			t.Fatalf("status icon %q is not svg", name)
		}
	}
}

func TestStatusIconMissing(t *testing.T) {
	if _, err := StatusIcon("missing"); err == nil {
		t.Fatal("expected error")
	}
}

func TestTreeIcons(t *testing.T) {
	want := []string{
		"group", "group_open", "repository", "repository_modified",
		"repository_ahead", "repository_missing", "worktree",
		"branch", "branch_current", "branch_remote", "tag", "stash",
		"folder", "file",
	}
	names := TreeIconNames()
	if len(names) != len(want) {
		t.Fatalf("TreeIconNames() = %v, want %v", names, want)
	}
	for _, name := range want {
		if !slices.Contains(names, name) {
			t.Fatalf("tree icon %q missing in %v", name, names)
		}
		data, err := TreeIcon(name)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(strings.TrimSpace(string(data)), "<svg") {
			t.Fatalf("tree icon %q is not svg", name)
		}
	}
}

func TestTreeIconMissing(t *testing.T) {
	if _, err := TreeIcon("missing"); err == nil {
		t.Fatal("expected error")
	}
}
