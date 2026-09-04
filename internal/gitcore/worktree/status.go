package worktree

import (
	"context"
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/index"
)

type StatusCode byte

const (
	StatusUnmodified  StatusCode = ' '
	StatusModified    StatusCode = 'M'
	StatusTypeChanged StatusCode = 'T'
	StatusAdded       StatusCode = 'A'
	StatusDeleted     StatusCode = 'D'
	StatusRenamed     StatusCode = 'R'
	StatusCopied      StatusCode = 'C'
	StatusUnmerged    StatusCode = 'U'
	StatusUntracked   StatusCode = '?'
	StatusIgnored     StatusCode = '!'
)

type ConflictKind uint8

const (
	ConflictNone ConflictKind = iota
	ConflictBothAdded
	ConflictBothModified
	ConflictBothDeleted
	ConflictAddedByUs
	ConflictAddedByThem
	ConflictDeletedByUs
	ConflictDeletedByThem
)

type Entry struct {
	Path     string
	OrigPath string
	Staged   StatusCode
	Unstaged StatusCode
	Conflict ConflictKind
	IsDir    bool
}

type Status struct {
	Entries    []Entry
	HeadBranch string
	Detached   bool
	Ahead      int
	Behind     int
}

func (w *Worktree) Status(ctx context.Context) (Status, error) {
	if err := ctx.Err(); err != nil {
		return Status{}, err
	}
	branch, detached, headCommit, err := w.resolveHead()
	if err != nil {
		return Status{}, err
	}
	headTree := map[string]headEntry{}
	if !headCommit.IsZero() {
		commit, err := w.db.Commit(headCommit)
		if err != nil {
			return Status{}, fmt.Errorf("%w: %w", ErrReadHead, err)
		}
		headTree, err = w.collectHeadTree(ctx, commit.Tree)
		if err != nil {
			return Status{}, err
		}
	}
	staged := w.stagedStatus(headTree)

	var mergedEntries []*index.Entry
	trackedFiles := map[string]bool{}
	trackedDirs := map[string]bool{}
	for entry := range w.index.Entries() {
		trackedFiles[entry.Path] = true
		for dir := path.Dir(entry.Path); dir != "." && dir != "/" && !trackedDirs[dir]; dir = path.Dir(dir) {
			trackedDirs[dir] = true
		}
		if entry.Stage == index.StageMerged {
			mergedEntries = append(mergedEntries, entry)
		}
	}

	unstaged, err := w.unstagedStatuses(ctx, mergedEntries)
	if err != nil {
		return Status{}, err
	}
	untracked, err := w.untrackedEntries(ctx, trackedDirs, trackedFiles)
	if err != nil {
		return Status{}, err
	}

	combined := map[string]*Entry{}
	for entryPath, entry := range staged {
		stored := entry
		combined[entryPath] = &stored
	}
	for entryPath, code := range unstaged {
		if existing, ok := combined[entryPath]; ok {
			existing.Unstaged = code
			continue
		}
		combined[entryPath] = &Entry{Path: entryPath, Staged: StatusUnmodified, Unstaged: code}
	}
	for _, entry := range w.conflictEntries() {
		stored := entry
		combined[stored.Path] = &stored
	}
	for _, entry := range untracked {
		if existing, ok := combined[entry.Path]; ok {
			existing.Unstaged = entry.Unstaged
			existing.IsDir = entry.IsDir
			continue
		}
		stored := entry
		combined[stored.Path] = &stored
	}

	result := Status{HeadBranch: branch, Detached: detached, Entries: make([]Entry, 0, len(combined))}
	for _, entry := range combined {
		result.Entries = append(result.Entries, *entry)
	}
	slices.SortFunc(result.Entries, func(a, b Entry) int { return strings.Compare(a.Path, b.Path) })
	return result, nil
}
