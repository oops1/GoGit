package scan

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func mkdirs(t *testing.T, root string, dirs ...string) {
	t.Helper()
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(root, d), 0o700); err != nil {
			t.Fatal(err)
		}
	}
}

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func makeRepo(t *testing.T, root, rel string) {
	t.Helper()
	writeFile(t, root, filepath.Join(rel, ".git", "HEAD"), "ref: refs/heads/main\n")
}

func makeBare(t *testing.T, root, rel string) {
	t.Helper()
	writeFile(t, root, filepath.Join(rel, "HEAD"), "ref: refs/heads/main\n")
	mkdirs(t, root, filepath.Join(rel, "objects"), filepath.Join(rel, "refs"))
}

func collect(t *testing.T, ctx context.Context, root string, opts Options) ([]Found, []error) {
	t.Helper()
	var found []Found
	var errs []error
	for f, err := range Scan(ctx, root, opts) {
		if err != nil {
			errs = append(errs, err)
			continue
		}
		found = append(found, f)
	}
	return found, errs
}

func paths(root string, found []Found) []string {
	out := make([]string, 0, len(found))
	for _, f := range found {
		rel, _ := filepath.Rel(root, f.Path)
		out = append(out, filepath.ToSlash(rel))
	}
	return out
}

func TestScanFindsRepositoriesInLexicographicOrderWithoutDescendingIntoThem(t *testing.T) {
	root := t.TempDir()
	makeRepo(t, root, "b/inner")
	makeRepo(t, root, "a")
	makeRepo(t, root, "a/nested")
	mkdirs(t, root, "c/empty")
	found, errs := collect(t, t.Context(), root, Options{})
	if len(errs) != 0 {
		t.Fatalf("errors: %v", errs)
	}
	if got := paths(root, found); !slices.Equal(got, []string{"a", "b/inner"}) {
		t.Fatalf("found = %v", got)
	}
}

func TestScanDescendsIntoRepositoriesWhenNested(t *testing.T) {
	root := t.TempDir()
	makeRepo(t, root, "a")
	makeRepo(t, root, "a/nested")
	found, _ := collect(t, t.Context(), root, Options{Nested: true})
	if got := paths(root, found); !slices.Equal(got, []string{"a", "a/nested"}) {
		t.Fatalf("found = %v", got)
	}
}

func TestScanReportsRootItselfWhenItIsARepository(t *testing.T) {
	root := t.TempDir()
	makeRepo(t, root, ".")
	found, _ := collect(t, t.Context(), root, Options{})
	if len(found) != 1 || found[0].Path != filepath.Clean(root) {
		t.Fatalf("found = %+v", found)
	}
}

func TestScanRecognisesGitdirFilesAndWorktrees(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "wt/.git", "gitdir: ../main/.git/worktrees/wt\r\n")
	writeFile(t, root, "sep/.git", "gitdir: /somewhere/else.git\n")
	writeFile(t, root, "junk/.git", "not a git file")
	writeFile(t, root, "nohead/.git/config", "")
	found, errs := collect(t, t.Context(), root, Options{})
	if len(errs) != 0 {
		t.Fatalf("errors: %v", errs)
	}
	if got := paths(root, found); !slices.Equal(got, []string{"sep", "wt"}) {
		t.Fatalf("found = %v", got)
	}
	if found[0].Worktree || !found[1].Worktree {
		t.Fatalf("worktree flags: %+v", found)
	}
}

func TestScanIncludesBareRepositoriesOnlyWhenAsked(t *testing.T) {
	root := t.TempDir()
	makeBare(t, root, "bare.git")
	writeFile(t, root, "half/HEAD", "x")
	mkdirs(t, root, "half/objects")
	writeFile(t, root, "wrong/HEAD", "x")
	writeFile(t, root, "wrong/objects", "file")
	mkdirs(t, root, "wrong/refs")
	found, _ := collect(t, t.Context(), root, Options{})
	if len(found) != 0 {
		t.Fatalf("bare must be ignored by default: %+v", found)
	}
	found, _ = collect(t, t.Context(), root, Options{IncludeBare: true})
	if got := paths(root, found); !slices.Equal(got, []string{"bare.git"}) || !found[0].Bare {
		t.Fatalf("found = %+v", found)
	}
}

func TestScanSkipsExcludedAndHiddenDirectories(t *testing.T) {
	root := t.TempDir()
	makeRepo(t, root, "node_modules/pkg")
	makeRepo(t, root, ".hidden/repo")
	makeRepo(t, root, "custom/repo")
	makeRepo(t, root, "ok")
	found, _ := collect(t, t.Context(), root, Options{})
	if got := paths(root, found); !slices.Equal(got, []string{".hidden/repo", "custom/repo", "ok"}) {
		t.Fatalf("default exclude: %v", got)
	}
	found, _ = collect(t, t.Context(), root, Options{SkipHidden: true, Exclude: []string{"custom"}})
	if got := paths(root, found); !slices.Equal(got, []string{"node_modules/pkg", "ok"}) {
		t.Fatalf("custom exclude: %v", got)
	}
}

func TestScanHonoursMaxDepth(t *testing.T) {
	root := t.TempDir()
	makeRepo(t, root, "l1")
	makeRepo(t, root, "d1/l2")
	makeRepo(t, root, "d1/d2/l3")
	found, _ := collect(t, t.Context(), root, Options{MaxDepth: 2})
	if got := paths(root, found); !slices.Equal(got, []string{"d1/l2", "l1"}) {
		t.Fatalf("found = %v", got)
	}
}

func TestScanDoesNotFollowSymlinkedDirectories(t *testing.T) {
	root := t.TempDir()
	makeRepo(t, root, "real")
	if err := os.Symlink(filepath.Join(root, "real"), filepath.Join(root, "link")); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	found, _ := collect(t, t.Context(), root, Options{})
	if got := paths(root, found); !slices.Equal(got, []string{"real"}) {
		t.Fatalf("found = %v", got)
	}
}

func TestScanReportsUnreadableDirectoryAndContinues(t *testing.T) {
	root := t.TempDir()
	makeRepo(t, root, "z")
	mkdirs(t, root, "a/gone")
	opts := Options{Progress: func(dir string) {
		if filepath.Base(dir) == "gone" {
			_ = os.Remove(dir)
		}
	}}
	found, errs := collect(t, t.Context(), root, opts)
	if len(errs) != 1 {
		t.Fatalf("errors = %v", errs)
	}
	if got := paths(root, found); !slices.Equal(got, []string{"z"}) {
		t.Fatalf("found = %v", got)
	}
}

func TestScanStopsWhenConsumerBreaks(t *testing.T) {
	root := t.TempDir()
	makeRepo(t, root, "a")
	makeRepo(t, root, "b")
	var visited []string
	opts := Options{Progress: func(dir string) { visited = append(visited, filepath.Base(dir)) }}
	for range Scan(t.Context(), root, opts) {
		break
	}
	if slices.Contains(visited, "b") {
		t.Fatalf("scan continued after break: %v", visited)
	}
}

func TestScanStopsAfterReadDirErrorWhenConsumerBreaks(t *testing.T) {
	root := t.TempDir()
	mkdirs(t, root, "a/gone")
	makeRepo(t, root, "b")
	var visited []string
	opts := Options{Progress: func(dir string) {
		visited = append(visited, filepath.Base(dir))
		if filepath.Base(dir) == "gone" {
			_ = os.Remove(dir)
		}
	}}
	for _, err := range Scan(t.Context(), root, opts) {
		if err != nil {
			break
		}
	}
	if slices.Contains(visited, "b") {
		t.Fatalf("scan continued after break: %v", visited)
	}
}

func TestScanStopsOnCancelledContext(t *testing.T) {
	root := t.TempDir()
	makeRepo(t, root, "a")
	makeRepo(t, root, "b")
	ctx, cancel := context.WithCancel(t.Context())
	opts := Options{Progress: func(dir string) {
		if filepath.Base(dir) == "a" {
			cancel()
		}
	}}
	found, errs := collect(t, ctx, root, opts)
	if len(errs) != 1 || !errors.Is(errs[0], context.Canceled) {
		t.Fatalf("errors = %v", errs)
	}
	if got := paths(root, found); !slices.Equal(got, []string{"a"}) {
		t.Fatalf("found = %v", got)
	}
}
