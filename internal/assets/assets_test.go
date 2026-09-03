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
