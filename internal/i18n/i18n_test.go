package i18n

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/oops1/headless-gui/v3/widget"
)

func TestBuiltinHasEnglishAndRussian(t *testing.T) {
	cat, err := Builtin()
	if err != nil {
		t.Fatal(err)
	}
	codes := cat.Codes()
	if len(codes) != 2 || codes[0] != "en" || codes[1] != "ru" {
		t.Fatalf("codes = %v", codes)
	}
}

func TestBuiltinTablesAreComplete(t *testing.T) {
	cat, err := Builtin()
	if err != nil {
		t.Fatal(err)
	}
	for code, missing := range cat.MissingKeys("en") {
		if len(missing) > 0 {
			t.Errorf("%s is missing keys: %v", code, missing)
		}
	}
	for code, missing := range cat.MissingKeys("ru") {
		if len(missing) > 0 {
			t.Errorf("%s has extra keys compared to ru: %v", code, missing)
		}
	}
}

func TestMissingKeysReportsGaps(t *testing.T) {
	cat := Catalog{
		"en": {"A": "a", "B": "b"},
		"de": {"A": "x"},
		"fr": {"A": "y", "B": "z"},
	}
	got := cat.MissingKeys("en")
	if len(got["de"]) != 1 || got["de"][0] != "B" {
		t.Fatalf("de missing = %v", got["de"])
	}
	if len(got["fr"]) != 0 {
		t.Fatalf("fr missing = %v", got["fr"])
	}
}

func TestMissingKeysUnknownReference(t *testing.T) {
	if got := (Catalog{}).MissingKeys("xx"); got != nil {
		t.Fatalf("got %v", got)
	}
}

func TestLoadFSSkipsNonJSON(t *testing.T) {
	fsys := fstest.MapFS{
		"de.json":   {Data: []byte(`{"A":"a"}`)},
		"notes.txt": {Data: []byte("ignored")},
		"dir":       {Mode: os.ModeDir},
	}
	cat, err := LoadFS(fsys)
	if err != nil {
		t.Fatal(err)
	}
	if len(cat) != 1 || cat["de"]["A"] != "a" {
		t.Fatalf("catalog = %v", cat)
	}
}

func TestLoadFSInvalidJSON(t *testing.T) {
	fsys := fstest.MapFS{"de.json": {Data: []byte(`{`)}}
	if _, err := LoadFS(fsys); err == nil {
		t.Fatal("expected error")
	}
}

func TestLoadFSReadDirError(t *testing.T) {
	if _, err := LoadFS(os.DirFS(filepath.Join(t.TempDir(), "missing"))); err == nil {
		t.Fatal("expected error")
	}
}

func TestLoadFSReadFileError(t *testing.T) {
	fsys := openFailFS{inner: fstest.MapFS{"de.json": {Data: []byte(`{}`)}}}
	if _, err := LoadFS(fsys); err == nil {
		t.Fatal("expected error")
	}
}

func TestCode(t *testing.T) {
	if Code("RU.json") != "ru" || Code("nested/en.json") != "en" {
		t.Fatal("code normalization")
	}
}

func TestMerge(t *testing.T) {
	base := Catalog{"en": {"A": "a", "B": "b"}}
	base.Merge(Catalog{"en": {"B": "override"}, "de": {"A": "x"}})
	if base["en"]["B"] != "override" || base["en"]["A"] != "a" || base["de"]["A"] != "x" {
		t.Fatalf("merge = %v", base)
	}
}

func TestInstallWithUserOverride(t *testing.T) {
	widget.ClearStrings()
	t.Cleanup(widget.ClearStrings)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "de.json"), []byte(`{"Status.Ready":"Bereit"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cat, err := Install(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(cat.Codes()) != 3 {
		t.Fatalf("codes = %v", cat.Codes())
	}
	Apply("de")
	if Current() != "de" {
		t.Fatalf("current = %q", Current())
	}
	if T("Status.Ready") != "Bereit" {
		t.Fatalf("T = %q", T("Status.Ready"))
	}
	if T("Pane.Files") != "Files" {
		t.Fatalf("fallback = %q", T("Pane.Files"))
	}
	Apply("ru")
	if T("Pane.Files") != "Файлы" {
		t.Fatalf("ru = %q", T("Pane.Files"))
	}
}

func TestInstallMissingUserDir(t *testing.T) {
	widget.ClearStrings()
	t.Cleanup(widget.ClearStrings)
	if _, err := Install(filepath.Join(t.TempDir(), "nope")); err != nil {
		t.Fatal(err)
	}
}

func TestInstallBrokenUserDir(t *testing.T) {
	widget.ClearStrings()
	t.Cleanup(widget.ClearStrings)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "xx.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(dir); err == nil {
		t.Fatal("expected error")
	}
}

func TestInstallFromBrokenBuiltin(t *testing.T) {
	widget.ClearStrings()
	t.Cleanup(widget.ClearStrings)
	if _, err := InstallFrom(openFailFS{inner: fstest.MapFS{"en.json": {Data: []byte(`{}`)}}}, ""); err == nil {
		t.Fatal("expected error")
	}
}

func TestInstallWithoutUserDir(t *testing.T) {
	widget.ClearStrings()
	t.Cleanup(widget.ClearStrings)
	cat, err := Install("")
	if err != nil {
		t.Fatal(err)
	}
	if len(cat.Codes()) != 2 {
		t.Fatalf("codes = %v", cat.Codes())
	}
}
