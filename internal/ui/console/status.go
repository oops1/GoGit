package console

import (
	"context"
	"strconv"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/worktree"
)

const detachedHeadLabel = "HEAD (no branch)"

var statusOptions = []option{
	plain("porcelain", ""),
	plain("short", "s"),
	plain("branch", "b"),
	plain("ignored", ""),
}

func (e Env) status(ctx context.Context) (worktree.Status, error) {
	db, err := e.openObjects()
	if err != nil {
		return worktree.Status{}, err
	}
	defer func() { _ = db.Close() }()
	store, err := e.openRefs()
	if err != nil {
		return worktree.Status{}, err
	}
	defer func() { _ = store.Close() }()
	wt, err := worktree.Open(e.Repo, worktree.Options{DB: db, Refs: store})
	if err != nil {
		return worktree.Status{}, err
	}
	defer func() { _ = wt.Close() }()
	return wt.Status(ctx)
}

func runStatus(ctx context.Context, env Env, args []string) (string, error) {
	opts, err := parseOptions(args, statusOptions)
	if err != nil {
		return "", err
	}
	if rest := opts.args(); len(rest) > 0 {
		return "", detail(ErrUsage, rest[0])
	}
	status, err := env.status(ctx)
	if err != nil {
		return "", err
	}
	return formatStatus(status, opts.has("branch"), opts.has("ignored")), nil
}

func formatStatus(status worktree.Status, withBranch, withIgnored bool) string {
	var lines []string
	if withBranch {
		lines = append(lines, "## "+statusBranchLabel(status))
	}
	for _, entry := range status.Entries {
		line, ok := statusEntryLine(entry, withIgnored)
		if ok {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n")
}

func statusBranchLabel(status worktree.Status) string {
	if status.Detached {
		return detachedHeadLabel
	}
	label := status.HeadBranch
	if suffix := divergenceSuffix(status.Ahead, status.Behind); suffix != "" {
		label += " " + suffix
	}
	return label
}

func divergenceSuffix(ahead, behind int) string {
	var parts []string
	if ahead > 0 {
		parts = append(parts, "ahead "+strconv.Itoa(ahead))
	}
	if behind > 0 {
		parts = append(parts, "behind "+strconv.Itoa(behind))
	}
	if len(parts) == 0 {
		return ""
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func statusEntryLine(entry worktree.Entry, withIgnored bool) (string, bool) {
	staged, unstaged := entry.Staged, entry.Unstaged
	switch {
	case unstaged == worktree.StatusIgnored:
		if !withIgnored {
			return "", false
		}
		staged = worktree.StatusIgnored
	case unstaged == worktree.StatusUntracked:
		staged = worktree.StatusUntracked
	case staged == worktree.StatusUnmodified && unstaged == worktree.StatusUnmodified:
		return "", false
	}
	path := entry.Path
	if entry.OrigPath != "" && entry.OrigPath != entry.Path {
		path = entry.OrigPath + " -> " + entry.Path
	}
	return string([]byte{byte(staged), byte(unstaged)}) + " " + path, true
}
