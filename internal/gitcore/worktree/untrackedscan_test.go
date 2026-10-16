package worktree

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func untrackedPaths(t *testing.T, w *Worktree) map[string]StatusCode {
	t.Helper()
	status, err := w.Status(t.Context())
	if err != nil {
		t.Fatalf("Status returned error %v", err)
	}
	got := map[string]StatusCode{}
	for _, entry := range status.Entries {
		got[entry.Path] = entry.Unstaged
	}
	return got
}

func TestUntrackedScanWorksInlineWhenEveryWorkerIsBusy(t *testing.T) {
	tr := newTestRepo(t)
	tr.stage("a/b/c/tracked.txt", "t\n")
	tr.stage("a/b/other.txt", "o\n")
	tr.commit("initial")
	tr.writeFile("a/b/c/new.txt", "n\n")
	tr.writeFile("a/b/new.txt", "n\n")
	w := tr.openWith(Options{Workers: 1})

	got := untrackedPaths(t, w)

	if got["a/b/c/new.txt"] != StatusUntracked || got["a/b/new.txt"] != StatusUntracked || len(got) != 2 {
		t.Fatalf("Status reported %v, want both new files with one worker", got)
	}
}

func TestAnUntrackedDirectoryWithOnlyIgnoredContentIsReportedIgnored(t *testing.T) {
	tr := newTestRepo(t)
	tr.stage(".gitignore", "*.log\nlogs/\n")
	tr.commit("initial")
	tr.writeFile("cache/first.log", "x\n")
	tr.writeFile("cache/logs/second.txt", "x\n")
	tr.writeFile("deep/only/dirs/third.log", "x\n")
	tr.mkdir("empty/nothing/here")
	tr.mkdir("hollow/logs/empty")

	got := untrackedPaths(t, tr.open())

	if got["cache/"] != StatusIgnored || got["deep/"] != StatusIgnored {
		t.Fatalf("Status reported %v, want both directories ignored", got)
	}
	if _, listed := got["hollow/"]; listed {
		t.Fatalf("Status listed a directory whose ignored part holds no files: %v", got)
	}
	if _, listed := got["empty/"]; listed {
		t.Fatalf("Status listed a directory without files: %v", got)
	}
}

func TestAnIgnoredSubdirectoryIsSearchedForFilesOnlyWhenNothingElseWasFound(t *testing.T) {
	tr := newTestRepo(t)
	tr.stage(".gitignore", "logs/\n")
	tr.commit("initial")
	tr.mkdir("box/logs/inner")
	tr.writeFile("box/logs/inner/deep.txt", "x\n")

	got := untrackedPaths(t, tr.open())

	if got["box/"] != StatusIgnored {
		t.Fatalf("Status reported %v, want box/ ignored because of the file under logs/", got)
	}
}

func TestReadingAnUntrackedTreeReportsADirectoryThatCannotBeListed(t *testing.T) {
	for _, broken := range []string{"stray/inner", "lone/logs/inner"} {
		t.Run(broken, func(t *testing.T) {
			tr := newTestRepo(t)
			tr.stage(".gitignore", "logs/\n")
			tr.commit("initial")
			tr.writeFile("stray/inner/y.txt", "x\n")
			tr.writeFile("lone/logs/inner/x.txt", "x\n")
			w := tr.open()
			previous := fsOpenDir
			fsOpenDir = func(root *os.Root, name string) (*os.File, error) {
				if filepath.ToSlash(name) == broken {
					return nil, errors.New("listing refused")
				}
				return previous(root, name)
			}
			t.Cleanup(func() { fsOpenDir = previous })

			if _, err := w.Status(t.Context()); err == nil {
				t.Fatalf("Status ignored that %s could not be listed", broken)
			}
		})
	}
}

func TestAnIgnoredDirectoryIsSkippedOnceAFileWasFound(t *testing.T) {
	tr := newTestRepo(t)
	tr.stage(".gitignore", "logs/\n*.log\n")
	tr.commit("initial")
	tr.writeFile("stray/inner/y.log", "x\n")
	tr.writeFile("stray/logs/inner/x.txt", "x\n")
	w := tr.open()
	previous := fsOpenDir
	fsOpenDir = func(root *os.Root, name string) (*os.File, error) {
		if filepath.ToSlash(name) == "stray/logs" {
			return nil, errors.New("this directory must not be read")
		}
		return previous(root, name)
	}
	t.Cleanup(func() { fsOpenDir = previous })

	got := untrackedPaths(t, w)

	if got["stray/"] != StatusIgnored {
		t.Fatalf("Status reported %v, want stray/ ignored without reading stray/logs", got)
	}
}

func TestAnEntryWithoutDirectoryInformationHasNothingToDescribe(t *testing.T) {
	if info, ok := (dirEntry{name: "x"}).describe(); ok || info != nil {
		t.Fatalf("describe = %v, %v, want nothing", info, ok)
	}
}

func TestUntrackedFilesCarryTheirSizeAndTimeFromTheListing(t *testing.T) {
	tr := newTestRepo(t)
	tr.stage("kept.txt", "k\n")
	tr.commit("initial")
	tr.writeFile("fresh.txt", "twelve bytes")
	w := tr.open()
	stated := countLstatCalls(t, "fresh.txt")

	status, err := w.Status(t.Context())

	if err != nil {
		t.Fatalf("Status returned error %v", err)
	}
	at := slices.IndexFunc(status.Entries, func(e Entry) bool { return e.Path == "fresh.txt" })
	if at < 0 || status.Entries[at].Size != 12 || status.Entries[at].ModTime.IsZero() {
		t.Fatalf("fresh.txt = %+v, want its size and time", status.Entries)
	}
	if calls := stated.Load(); calls != 0 {
		t.Fatalf("an untracked file was stated %d times, want its listing reused", calls)
	}
}
