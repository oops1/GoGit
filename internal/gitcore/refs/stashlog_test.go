package refs

import (
	"errors"
	"strings"
	"testing"
)

func stashLogDir(t *testing.T) string {
	t.Helper()
	dir := newGitDir(t)
	writeAt(t, dir, "refs/stash", oidFrom(t, "33").String()+"\n")
	writeAt(t, dir, "logs/refs/stash",
		reflogLine(t, "", "11", "one")+reflogLine(t, "11", "22", "two")+reflogLine(t, "22", "33", "three"))
	return dir
}

func TestDropReflogEntryRewritesTheNeighbourAndTheReference(t *testing.T) {
	cases := []struct {
		position int
		log      func(t *testing.T) string
		tip      string
	}{
		{0, func(t *testing.T) string { return reflogLine(t, "", "11", "one") + reflogLine(t, "11", "22", "two") }, "22"},
		{1, func(t *testing.T) string { return reflogLine(t, "", "11", "one") + reflogLine(t, "11", "33", "three") }, "33"},
		{2, func(t *testing.T) string { return reflogLine(t, "", "22", "two") + reflogLine(t, "22", "33", "three") }, "33"},
	}
	for _, tc := range cases {
		dir := stashLogDir(t)
		store := openStore(t, dir)
		if err := store.DropReflogEntry(StashName, tc.position); err != nil {
			t.Fatalf("DropReflogEntry(%d) returned error %v", tc.position, err)
		}
		if got, want := readAt(t, dir, "logs/refs/stash"), tc.log(t); got != want {
			t.Fatalf("DropReflogEntry(%d) left log %q, want %q", tc.position, got, want)
		}
		if got := strings.TrimSpace(readAt(t, dir, "refs/stash")); got != oidFrom(t, tc.tip).String() {
			t.Fatalf("DropReflogEntry(%d) left the reference at %s", tc.position, got)
		}
	}
}

func TestDropReflogEntryRemovesTheReferenceWithTheLastEntry(t *testing.T) {
	dir := newGitDir(t)
	writeAt(t, dir, "refs/stash", oidFrom(t, "11").String()+"\n")
	writeAt(t, dir, "logs/refs/stash", reflogLine(t, "", "11", "only"))
	store := openStore(t, dir)
	if err := store.DropReflogEntry(StashName, 0); err != nil {
		t.Fatalf("DropReflogEntry returned error %v", err)
	}
	if existsAt(dir, "refs/stash") || existsAt(dir, "logs/refs/stash") {
		t.Fatal("the reference or its log survived the last drop")
	}
}

func TestDropReflogEntryRejectsBadRequests(t *testing.T) {
	dir := stashLogDir(t)
	store := openStore(t, dir)
	if err := store.DropReflogEntry("refs/bad..name", 0); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("invalid name returned %v", err)
	}
	for _, position := range []int{-1, 3} {
		if err := store.DropReflogEntry(StashName, position); !errors.Is(err, ErrNotFound) {
			t.Fatalf("position %d returned %v", position, err)
		}
	}
	writeAt(t, dir, "refs/stash.lock", "")
	if err := store.DropReflogEntry(StashName, 0); !errors.Is(err, ErrLocked) {
		t.Fatalf("locked reference returned %v", err)
	}
}

func TestDropReflogEntryReportsLogProblems(t *testing.T) {
	dir := stashLogDir(t)
	writeAt(t, dir, "logs/refs/stash", "garbage\n")
	if err := openStore(t, dir).DropReflogEntry(StashName, 0); !errors.Is(err, ErrMalformedReflog) {
		t.Fatalf("malformed log returned %v", err)
	}

	dir = stashLogDir(t)
	writeAt(t, dir, "logs/refs/stash.lock", "")
	if err := openStore(t, dir).DropReflogEntry(StashName, 0); !errors.Is(err, ErrLocked) {
		t.Fatalf("locked log returned %v", err)
	}
}

func TestDropReflogEntryReportsWriteFailures(t *testing.T) {
	boom := errors.New("boom")
	cases := []struct {
		name  string
		setup func(t *testing.T)
	}{
		{"log write", func(t *testing.T) { swapWrite(t, 0, boom) }},
		{"reference write", func(t *testing.T) { swapWrite(t, 1, boom) }},
		{"log rename", func(t *testing.T) {
			swapRename(t, func(from string) bool { return from == "logs/refs/stash.lock" }, boom)
		}},
		{"reference rename", func(t *testing.T) {
			swapRename(t, func(from string) bool { return from == "refs/stash.lock" }, boom)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := stashLogDir(t)
			store := openStore(t, dir)
			tc.setup(t)
			if err := store.DropReflogEntry(StashName, 0); !errors.Is(err, boom) {
				t.Fatalf("DropReflogEntry returned %v, want boom", err)
			}
			if existsAt(dir, "refs/stash.lock") || existsAt(dir, "logs/refs/stash.lock") {
				t.Fatal("a lock file was left behind")
			}
		})
	}
}

func TestStashReferenceAlwaysKeepsAReflog(t *testing.T) {
	dir := newGitDir(t)
	store := openStoreWith(t, Options{GitDir: dir, Bare: true, Committer: testCommitter()})
	tx := store.Begin()
	tx.SetMessage("WIP on main")
	if err := tx.Set(StashName, oidFrom(t, "11")); err != nil {
		t.Fatalf("Set returned error %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit returned error %v", err)
	}
	if got := readAt(t, dir, "logs/refs/stash"); got != reflogLine(t, "", "11", "WIP on main") {
		t.Fatalf("stash log = %q", got)
	}
}

func TestSettingAnUnchangedBranchLogsOnlyHead(t *testing.T) {
	dir := newGitDir(t)
	writeAt(t, dir, "HEAD", "ref: refs/heads/main\n")
	writeAt(t, dir, "refs/heads/main", oidFrom(t, "11").String()+"\n")
	store := openStore(t, dir)
	tx := store.Begin()
	tx.SetMessage("reset: moving to HEAD")
	if err := tx.Update(HEAD, oidFrom(t, "11"), oidFrom(t, "11")); err != nil {
		t.Fatalf("Update returned error %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit returned error %v", err)
	}
	if existsAt(dir, "logs/refs/heads/main") {
		t.Fatal("the unchanged branch got a reflog entry")
	}
	if got := readAt(t, dir, "logs/HEAD"); got != reflogLine(t, "11", "11", "reset: moving to HEAD") {
		t.Fatalf("HEAD log = %q", got)
	}
}
