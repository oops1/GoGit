package console

import (
	"context"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/worktree"
)

func busyRepo(t *testing.T) *testRepo {
	t.Helper()
	r := newTestRepo(t)
	base := r.commit("base", map[string]string{"a.txt": "a\n"})
	r.run("branch feature " + base.String())
	r.run("tag v1")
	r.write("a.txt", "changed\n")
	r.run("stash")
	r.write("b.txt", "b\n")
	return r
}

func TestEveryReadingCommandStopsOnACancelledContext(t *testing.T) {
	r := busyRepo(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	tests := map[string][]string{
		"status":    nil,
		"log":       {"--oneline"},
		"branch":    {"new-one"},
		"add":       {"b.txt"},
		"add -u":    {"-u"},
		"commit":    {"-m", "nope"},
		"commit -a": {"-a", "-m", "nope"},
		"reset":     {"--hard", "HEAD"},
		"diff":      {"HEAD", "HEAD"},
		"merge":     {"feature"},
		"rebase":    {"feature"},
		"stash":     {"list"},
		"tag":       nil,
		"submodule": {"status"},
		"checkout":  {"feature"},
		"switch":    {"feature"},
	}
	for name, args := range tests {
		t.Run(name, func(t *testing.T) {
			command, _, _ := strings.Cut(name, " ")
			if _, err := commands[command](ctx, r.env, args); err == nil {
				t.Fatalf("%s must stop on a cancelled context", name)
			}
		})
	}
}

func TestACommitSubjectOfAMissingObjectIsEmpty(t *testing.T) {
	r := newTestRepo(t)
	db, err := r.env.openObjects()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	if got := commitSubject(db, hash.Zero); got != "" {
		t.Fatalf("subject = %q", got)
	}
}

func TestStageableSkipsWhatGitWouldSkip(t *testing.T) {
	tests := []struct {
		name          string
		entry         worktree.Entry
		withUntracked bool
		want          bool
	}{
		{"ignored", worktree.Entry{Unstaged: worktree.StatusIgnored}, true, false},
		{"untracked kept", worktree.Entry{Unstaged: worktree.StatusUntracked}, true, true},
		{"untracked skipped", worktree.Entry{Unstaged: worktree.StatusUntracked}, false, false},
		{"tracked directory", worktree.Entry{IsDir: true, Staged: worktree.StatusModified}, true, false},
		{
			name:  "unchanged file",
			entry: worktree.Entry{Staged: worktree.StatusUnmodified, Unstaged: worktree.StatusUnmodified},
			want:  false,
		},
		{"changed in the index", worktree.Entry{Staged: worktree.StatusModified, Unstaged: worktree.StatusUnmodified}, false, true},
		{"changed in the tree", worktree.Entry{Staged: worktree.StatusUnmodified, Unstaged: worktree.StatusModified}, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := stageable(tt.entry, tt.withUntracked); got != tt.want {
				t.Fatalf("stageable = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAddWithUntrackedDirectoriesDropsTheTrailingSlash(t *testing.T) {
	r := newTestRepo(t)
	r.commit("initial", map[string]string{"a.txt": "a\n"})
	r.write("fresh/inner.txt", "fresh\n")

	paths, err := r.env.changedPaths(t.Context(), true)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 || paths[0] != "fresh" {
		t.Fatalf("paths = %#v", paths)
	}
}
