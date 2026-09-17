package worktree

import (
	"context"
	"fmt"
	"path"
	"slices"
	"strings"
	"time"

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
	Size     int64
	ModTime  time.Time

	Submodule         SubmoduleChange
	FilterUnsupported bool
}

type SubmoduleChange struct {
	CommitChanged bool
	Modified      bool
	Untracked     bool
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
	if err := w.refreshIndex(); err != nil {
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
	for entry := range w.currentIndex().Entries() {
		trackedFiles[entry.Path] = true
		for dir := path.Dir(entry.Path); dir != "." && dir != "/" && !trackedDirs[dir]; dir = path.Dir(dir) {
			trackedDirs[dir] = true
		}
		if entry.Stage == index.StageMerged {
			mergedEntries = append(mergedEntries, entry)
		}
	}

	rules, err := w.gitlinkRulesFor(mergedEntries, headTree)
	if err != nil {
		return Status{}, err
	}
	unstaged, err := w.unstagedStatuses(ctx, mergedEntries, rules)
	if err != nil {
		return Status{}, err
	}
	var untracked []Entry
	if !w.hideUntracked {
		untracked, err = w.untrackedEntries(ctx, trackedDirs, trackedFiles)
		if err != nil {
			return Status{}, err
		}
	}

	combined := map[string]*Entry{}
	for entryPath, entry := range staged {
		stored := entry
		combined[entryPath] = &stored
	}
	for entryPath, change := range unstaged {
		if existing, ok := combined[entryPath]; ok {
			existing.Unstaged, existing.Submodule = change.code, change.submodule
			continue
		}
		combined[entryPath] = &Entry{Path: entryPath, Staged: StatusUnmodified, Unstaged: change.code, Submodule: change.submodule}
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

	if w.includeUnmodified {
		for _, entry := range mergedEntries {
			if _, ok := combined[entry.Path]; ok {
				continue
			}
			combined[entry.Path] = &Entry{Path: entry.Path, Staged: StatusUnmodified, Unstaged: StatusUnmodified}
		}
	}

	result := Status{HeadBranch: branch, Detached: detached, Entries: make([]Entry, 0, len(combined))}
	for _, entry := range combined {
		w.fillWorkingInfo(entry)
		entry.FilterUnsupported = !entry.IsDir && w.policy(entry.Path).FilterUnsupported()
		result.Entries = append(result.Entries, *entry)
	}
	slices.SortFunc(result.Entries, func(a, b Entry) int { return strings.Compare(a.Path, b.Path) })
	return result, nil
}
