package refs

import (
	"errors"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

func reflogLine(t *testing.T, old, current, message string) string {
	t.Helper()
	line := oidFrom(t, old).String() + " " + oidFrom(t, current).String() + " " + testSignature().String()
	if message != "" {
		line += "\t" + message
	}
	return line + "\n"
}

func TestReflogReadsEntriesFromOldestToNewest(t *testing.T) {
	dir := newGitDir(t)
	writeAt(t, dir, "logs/refs/heads/main",
		reflogLine(t, "00", "11", "commit: one")+reflogLine(t, "11", "22", "commit: two"))
	store := openStore(t, dir)

	var messages []string
	for entry, err := range store.Reflog(BranchName("main")) {
		if err != nil {
			t.Fatalf("Reflog returned error %v", err)
		}
		messages = append(messages, entry.Message)
	}
	if strings.Join(messages, "|") != "commit: one|commit: two" {
		t.Fatalf("Reflog returned %v", messages)
	}
	last, err := store.ReflogLast(BranchName("main"))
	if err != nil {
		t.Fatalf("ReflogLast returned error %v", err)
	}
	if last.Old != oidFrom(t, "11") || last.New != oidFrom(t, "22") {
		t.Fatalf("last entry is %+v", last)
	}
	if last.Committer.Name != testSignature().Name || last.Committer.When.Unix() != testSignature().When.Unix() {
		t.Fatalf("committer is %+v", last.Committer)
	}
}

func TestReflogIsEmptyWhenLogIsMissingOrEmpty(t *testing.T) {
	dir := newGitDir(t)
	writeAt(t, dir, "logs/refs/heads/empty", "")
	store := openStore(t, dir)
	for _, name := range []Name{BranchName("absent"), BranchName("empty")} {
		for range store.Reflog(name) {
			t.Fatalf("Reflog(%s) produced an entry", name)
		}
		if _, err := store.ReflogLast(name); !errors.Is(err, ErrNotFound) {
			t.Fatalf("ReflogLast(%s) returned %v, want ErrNotFound", name, err)
		}
	}
}

func TestReflogStopsWhenCallerBreaks(t *testing.T) {
	dir := newGitDir(t)
	writeAt(t, dir, "logs/refs/heads/main",
		reflogLine(t, "00", "11", "one")+reflogLine(t, "11", "22", "two"))
	store := openStore(t, dir)
	seen := 0
	for range store.Reflog(BranchName("main")) {
		seen++
		break
	}
	if seen != 1 {
		t.Fatalf("iteration produced %d entries", seen)
	}
}

func TestReflogReportsBrokenInput(t *testing.T) {
	dir := newGitDir(t)
	writeAt(t, dir, "logs/refs/heads/broken", "not a reflog line\n")
	store := openStore(t, dir)
	if _, err := store.ReflogLast(BranchName("broken")); !errors.Is(err, ErrMalformedReflog) {
		t.Fatalf("ReflogLast returned %v, want ErrMalformedReflog", err)
	}
	for _, err := range store.Reflog(Name("refs/heads/bad name")) {
		if !errors.Is(err, ErrInvalidName) {
			t.Fatalf("Reflog returned %v, want ErrInvalidName", err)
		}
	}
	if _, err := store.ReflogLast(Name("refs/heads/bad name")); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("ReflogLast returned %v, want ErrInvalidName", err)
	}
	writeAt(t, dir, "logs/refs/heads/main", reflogLine(t, "00", "11", "one"))
	swapReadFile(t, func(name string) bool { return name == "logs/refs/heads/main" }, errors.New("broken"))
	for _, err := range store.Reflog(BranchName("main")) {
		if !errors.Is(err, ErrReadFailed) {
			t.Fatalf("Reflog returned %v, want ErrReadFailed", err)
		}
	}
}

func TestParseReflogLineRejectsBrokenLines(t *testing.T) {
	zero := hash.Zero.String()
	broken := map[string]string{
		"short":            "abc",
		"bad old id":       strings.Repeat("z", hash.HexSize) + " " + zero + " " + testSignature().String(),
		"no first space":   zero + "x" + zero + " " + testSignature().String(),
		"bad new id":       zero + " " + strings.Repeat("z", hash.HexSize) + " " + testSignature().String(),
		"no second space":  zero + " " + zero + "x" + testSignature().String(),
		"broken signature": zero + " " + zero + " nobody",
	}
	for description, line := range broken {
		if _, err := ParseReflogLine([]byte(line)); !errors.Is(err, ErrMalformedReflog) {
			t.Errorf("ParseReflogLine of %s returned %v, want ErrMalformedReflog", description, err)
		}
	}
	entry, err := ParseReflogLine([]byte(zero + " " + zero + " " + testSignature().String()))
	if err != nil {
		t.Fatalf("ParseReflogLine returned error %v", err)
	}
	if entry.Message != "" {
		t.Fatalf("message is %q", entry.Message)
	}
}

func TestFormatReflogMessageCollapsesWhitespaceLikeGit(t *testing.T) {
	cases := map[string]string{
		"":                    "",
		"commit: one":         "commit: one",
		"  leading":           "leading",
		"trailing  ":          "trailing",
		"one\ntwo":            "one two",
		"one\n\n  two":        "one two",
		"keep  inner  spaces": "keep  inner  spaces",
		" \n\t ":              "",
	}
	for input, want := range cases {
		if got := FormatReflogMessage(input); got != want {
			t.Errorf("FormatReflogMessage(%q) is %q, want %q", input, got, want)
		}
	}
}

func TestReflogIsWrittenAccordingToPolicy(t *testing.T) {
	cases := []struct {
		policy ReflogPolicy
		bare   bool
		name   Name
		want   bool
	}{
		{policy: ReflogDefault, name: BranchName("main"), want: true},
		{policy: ReflogDefault, name: TagName("v1"), want: false},
		{policy: ReflogDefault, name: RemoteBranchName("origin", "main"), want: true},
		{policy: ReflogDefault, name: Name("refs/notes/commits"), want: true},
		{policy: ReflogDefault, bare: true, name: BranchName("main"), want: false},
		{policy: ReflogEnabled, bare: true, name: BranchName("main"), want: true},
		{policy: ReflogDisabled, name: BranchName("main"), want: false},
		{policy: ReflogAlways, name: TagName("v1"), want: true},
		{policy: ReflogDisabled, name: HEAD, want: true},
	}
	for _, item := range cases {
		dir := newGitDir(t)
		store := openStoreWith(t, Options{
			GitDir:    dir,
			Bare:      item.bare,
			Reflog:    item.policy,
			Committer: testCommitter(),
		})
		mustCommit(t, store, func(tx *Transaction) error {
			return tx.Detach(item.name, oidFrom(t, "11"))
		})
		if got := existsAt(dir, reflogPath(item.name)); got != item.want {
			t.Errorf("policy %v for %s wrote reflog %v", item.policy, item.name, got)
		}
	}
}

func TestReflogIsAppendedWhenLogFileAlreadyExists(t *testing.T) {
	dir := newGitDir(t)
	writeAt(t, dir, "logs/refs/tags/v1", reflogLine(t, "00", "11", "old"))
	store := openStoreWith(t, Options{GitDir: dir, Reflog: ReflogDisabled, Committer: testCommitter()})
	mustCommit(t, store, func(tx *Transaction) error {
		return tx.Detach(TagName("v1"), oidFrom(t, "22"))
	})
	if lines := strings.Count(readAt(t, dir, "logs/refs/tags/v1"), "\n"); lines != 2 {
		t.Fatalf("reflog holds %d lines", lines)
	}
}

func TestCommitFailsWithoutCommitter(t *testing.T) {
	dir := newGitDir(t)
	store := openStoreWith(t, Options{GitDir: dir})
	err := commitOne(t, store, func(tx *Transaction) error {
		return tx.Set(BranchName("main"), oidFrom(t, "11"))
	})
	if !errors.Is(err, ErrMissingCommitter) {
		t.Fatalf("Commit returned %v, want ErrMissingCommitter", err)
	}
}

func TestCommitFailsWhenReflogCannotBeCreated(t *testing.T) {
	dir := newGitDir(t)
	writeAt(t, dir, "logs/refs", "not a directory")
	store := openStore(t, dir)
	err := commitOne(t, store, func(tx *Transaction) error {
		return tx.Set(BranchName("main"), oidFrom(t, "11"))
	})
	if !errors.Is(err, ErrWriteFailed) {
		t.Fatalf("Commit returned %v, want ErrWriteFailed", err)
	}
}

func TestCommitFailsWhenReflogCannotBeWritten(t *testing.T) {
	dir := newGitDir(t)
	store := openStore(t, dir)
	swapWrite(t, 1, errors.New("disk is full"))
	err := commitOne(t, store, func(tx *Transaction) error {
		return tx.Set(BranchName("main"), oidFrom(t, "11"))
	})
	if !errors.Is(err, ErrWriteFailed) {
		t.Fatalf("Commit returned %v, want ErrWriteFailed", err)
	}
}
