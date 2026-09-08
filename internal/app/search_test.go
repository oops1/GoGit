package app

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/config"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/repo"
	"github.com/oops1/gogit/internal/ui/search"
)

func TestOpenSearchLogsWarningWhenViewCreationFails(t *testing.T) {
	widget.ClearStrings()
	t.Cleanup(widget.ClearStrings)
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	a, err := New(config.Default(), config.Paths{Dir: t.TempDir()}, logger)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)

	prev := newSearchView
	wantErr := errors.New("boom")
	newSearchView = func(widget.ModalShower) (*search.View, error) { return nil, wantErr }
	t.Cleanup(func() { newSearchView = prev })

	a.openSearch()

	if !strings.Contains(buf.String(), "open search dialog failed") {
		t.Fatalf("expected dialog failure to be logged: %s", buf.String())
	}
}

func TestOpenSearchPrefillsRootFromActiveRepositoryParentDirectory(t *testing.T) {
	a := newTestApp(t)
	dir := t.TempDir()
	repoDir := filepath.Join(dir, "myrepo")
	node, err := a.registry.AddRepository("myrepo", repoDir, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.registry.SetActive(node.ID); err != nil {
		t.Fatal(err)
	}

	var captured *search.View
	prev := newSearchView
	newSearchView = func(eng widget.ModalShower) (*search.View, error) {
		v, err := prev(eng)
		captured = v
		return v, err
	}
	t.Cleanup(func() { newSearchView = prev })

	a.openSearch()

	if captured == nil {
		t.Fatal("view not created")
	}
	if got, want := captured.Root(), filepath.Dir(node.Path); got != want {
		t.Fatalf("root = %q, want %q", got, want)
	}
}

func TestOpenSearchPrefillsRootWithHomeWhenNoActiveRepository(t *testing.T) {
	a := newTestApp(t)
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory available in this environment")
	}

	var captured *search.View
	prev := newSearchView
	newSearchView = func(eng widget.ModalShower) (*search.View, error) {
		v, err := prev(eng)
		captured = v
		return v, err
	}
	t.Cleanup(func() { newSearchView = prev })

	a.openSearch()

	if captured == nil {
		t.Fatal("view not created")
	}
	if got := captured.Root(); got != home {
		t.Fatalf("root = %q, want %q", got, home)
	}
}

func TestOpenSearchPrefillsRootWithEmptyStringWhenHomeDirIsUnavailable(t *testing.T) {
	a := newTestApp(t)

	prevHome := userHomeDir
	userHomeDir = func() (string, error) { return "", errors.New("no home") }
	t.Cleanup(func() { userHomeDir = prevHome })

	var captured *search.View
	prev := newSearchView
	newSearchView = func(eng widget.ModalShower) (*search.View, error) {
		v, err := prev(eng)
		captured = v
		return v, err
	}
	t.Cleanup(func() { newSearchView = prev })

	a.openSearch()

	if captured == nil {
		t.Fatal("view not created")
	}
	if got := captured.Root(); got != "" {
		t.Fatalf("root = %q, want empty", got)
	}
}

func TestWireSearchViewBrowseSetsRootWhenFolderPicked(t *testing.T) {
	a := newTestApp(t)
	view, err := search.NewView(a.Engine())
	if err != nil {
		t.Fatal(err)
	}
	a.wireSearchView(view)

	prev := showPickFolderDialog
	showPickFolderDialog = func(eng widget.ModalShower, opts widget.FileDialogOptions, cb func(string, bool)) *widget.FileDialog {
		cb(filepath.Join("picked", "dir"), true)
		return nil
	}
	t.Cleanup(func() { showPickFolderDialog = prev })

	view.OnBrowse()

	if got, want := view.Root(), filepath.Join("picked", "dir"); got != want {
		t.Fatalf("root = %q, want %q", got, want)
	}
}

func TestWireSearchViewBrowseIgnoresCancelledPick(t *testing.T) {
	a := newTestApp(t)
	view, err := search.NewView(a.Engine())
	if err != nil {
		t.Fatal(err)
	}
	a.wireSearchView(view)
	view.SetRoot("original")

	prev := showPickFolderDialog
	showPickFolderDialog = func(eng widget.ModalShower, opts widget.FileDialogOptions, cb func(string, bool)) *widget.FileDialog {
		cb("", false)
		return nil
	}
	t.Cleanup(func() { showPickFolderDialog = prev })

	view.OnBrowse()

	if got := view.Root(); got != "original" {
		t.Fatalf("root = %q, want unchanged", got)
	}
}

func TestWireSearchViewCancelClosesModalWithoutPanicking(t *testing.T) {
	a := newTestApp(t)
	view, err := search.NewView(a.Engine())
	if err != nil {
		t.Fatal(err)
	}
	a.wireSearchView(view)
	a.Engine().ShowModal(view.Dialog())

	view.OnCancel()
}

func TestSearchStatusTextCoversErrorEmptyAndFoundStates(t *testing.T) {
	widget.ClearStrings()
	t.Cleanup(widget.ClearStrings)
	if _, err := i18n.Install(""); err != nil {
		t.Fatal(err)
	}
	i18n.Apply("en")

	if got, want := searchStatusText(nil, errors.New("boom")), "boom"; got != want {
		t.Fatalf("error status = %q, want %q", got, want)
	}
	if got, want := searchStatusText(nil, nil), i18n.T("Dialog.Search.Empty"); got != want {
		t.Fatalf("empty status = %q, want %q", got, want)
	}
	found := []search.Found{{Path: "a"}, {Path: "b"}}
	if got, want := searchStatusText(found, nil), i18n.Tf("Dialog.Search.Found", 2); got != want {
		t.Fatalf("found status = %q, want %q", got, want)
	}
}

func TestSearchStatusColorReflectsError(t *testing.T) {
	a := newTestApp(t)
	if got := a.searchStatusColor(errors.New("boom")); got != secretsErrorTextColor {
		t.Fatalf("error color = %+v, want %+v", got, secretsErrorTextColor)
	}
	if got := a.searchStatusColor(nil); got == secretsErrorTextColor {
		t.Fatal("no-error color must not be the error color")
	}
}

func TestStartRepositorySearchAndAddWireTogetherEndToEnd(t *testing.T) {
	a, _ := newTestAppWithPaths(t)
	view, err := search.NewView(a.Engine())
	if err != nil {
		t.Fatal(err)
	}
	a.wireSearchView(view)

	dir := t.TempDir()
	first := filepath.Join(dir, "first")
	second := filepath.Join(dir, "second")

	prevScan := scanRepositories
	scanRepositories = func(ctx context.Context, root string, includeBare bool) ([]search.Found, error) {
		return []search.Found{{Path: first}, {Path: second, Bare: true}}, nil
	}
	t.Cleanup(func() { scanRepositories = prevScan })

	view.OnScan(dir, true)
	searchWG.Wait()
	drainPostQueue(t, a)

	view.Dialog().DefaultAction()

	if _, ok := a.registry.FindByPath(first); !ok {
		t.Fatal("first repository must have been added")
	}
	if _, ok := a.registry.FindByPath(second); !ok {
		t.Fatal("second repository must have been added")
	}
}

func TestAddFoundRepositoriesSkipsDuplicatesAndLogsOtherErrors(t *testing.T) {
	a, _ := newTestAppWithPaths(t)
	dir := t.TempDir()
	dup := filepath.Join(dir, "dup")
	if _, err := a.registry.AddRepository("dup", dup, ""); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	a.log = slog.New(slog.NewTextHandler(&buf, nil))

	prev := addRepositoryToRegistry
	boom := errors.New("boom")
	addRepositoryToRegistry = func(r *repo.Registry, name, path, group string) (*repo.Node, error) {
		if path == "broken" {
			return nil, boom
		}
		return prev(r, name, path, group)
	}
	t.Cleanup(func() { addRepositoryToRegistry = prev })

	fresh := filepath.Join(dir, "fresh")
	a.addFoundRepositories([]string{dup, "broken", fresh})

	if !strings.Contains(buf.String(), "add repository failed") {
		t.Fatalf("expected the non-duplicate error to be logged: %s", buf.String())
	}
	if _, ok := a.registry.FindByPath(fresh); !ok {
		t.Fatal("the fresh repository must still be added")
	}
}

func TestAddFoundRepositoriesLogsWarningWhenSaveFails(t *testing.T) {
	widget.ClearStrings()
	t.Cleanup(widget.ClearStrings)
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "config.toml"), 0o700); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	a, err := New(config.Default(), config.Paths{Dir: dir}, logger)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)

	a.addFoundRepositories([]string{filepath.Join(dir, "repo")})

	if !strings.Contains(buf.String(), "save config failed") {
		t.Fatalf("expected save failure to be logged: %s", buf.String())
	}
}
