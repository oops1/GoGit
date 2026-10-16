package worktree

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/index"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/submodule"
)

var loadTree = (*odb.DB).Tree

type stagedAddition struct {
	path  string
	entry *index.Entry
}

type stagedRemoval struct {
	path string
	mode object.Mode
	id   hash.ObjectID
}

type headDifference struct {
	changed map[string]Entry
	added   []stagedAddition
	removed []stagedRemoval
}

type indexRun struct {
	name  string
	isDir bool
	first int
	end   int
}

func (r indexRun) key() string {
	if r.isDir {
		return r.name + "/"
	}
	return r.name
}

func treeEntryKey(entry object.TreeEntry) string {
	if entry.Mode.IsTree() {
		return entry.Name + "/"
	}
	return entry.Name
}

func (w *Worktree) stagedStatus(ctx context.Context, headTree hash.ObjectID) (map[string]Entry, error) {
	idx := w.currentIndex()
	difference := &headDifference{changed: map[string]Entry{}}
	if err := w.compareDirectory(ctx, difference, headTree, "", idx.CacheTree, 0, idx.Len()); err != nil {
		return nil, err
	}
	return difference.entries(), nil
}

func (w *Worktree) compareDirectory(ctx context.Context, difference *headDifference, treeID hash.ObjectID, prefix string, cache *index.CacheTree, first, end int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if cache != nil && cache.Valid() && !treeID.IsZero() && cache.ID == treeID {
		return nil
	}
	var tree *object.Tree
	if !treeID.IsZero() {
		loaded, err := loadTree(w.db, treeID)
		if err != nil {
			return fmt.Errorf("%w: %w", ErrReadHeadTree, err)
		}
		tree = loaded
	}
	runs := w.indexRuns(prefix, first, end)
	var entries []object.TreeEntry
	if tree != nil {
		entries = tree.Entries
	}
	for len(entries) > 0 || len(runs) > 0 {
		switch {
		case len(runs) == 0 || (len(entries) > 0 && treeEntryKey(entries[0]) < runs[0].key()):
			if err := w.removeTreeEntry(ctx, difference, entries[0], prefix); err != nil {
				return err
			}
			entries = entries[1:]
		case len(entries) == 0 || runs[0].key() < treeEntryKey(entries[0]):
			w.addRun(difference, prefix, runs[0])
			runs = runs[1:]
		default:
			if err := w.compareEntry(ctx, difference, entries[0], runs[0], prefix, cache); err != nil {
				return err
			}
			entries, runs = entries[1:], runs[1:]
		}
	}
	return nil
}

func (w *Worktree) indexRuns(prefix string, first, end int) []indexRun {
	idx := w.currentIndex()
	var runs []indexRun
	for position := first; position < end; position++ {
		entry := idx.At(position)
		if entry.Stage != index.StageMerged {
			continue
		}
		name, _, nested := strings.Cut(entry.Path[len(prefix):], "/")
		if last := len(runs) - 1; last >= 0 && runs[last].name == name && runs[last].isDir == nested {
			runs[last].end = position + 1
			continue
		}
		runs = append(runs, indexRun{name: name, isDir: nested, first: position, end: position + 1})
	}
	return runs
}

func (w *Worktree) compareEntry(ctx context.Context, difference *headDifference, entry object.TreeEntry, run indexRun, prefix string, cache *index.CacheTree) error {
	if entry.Mode.IsTree() {
		var sub *index.CacheTree
		if cache != nil {
			sub = cache.Find(entry.Name)
		}
		return w.compareDirectory(ctx, difference, entry.ID, prefix+entry.Name+"/", sub, run.first, run.end)
	}
	staged := w.currentIndex().At(run.first)
	if staged.Mode == entry.Mode && staged.ID == entry.ID {
		return nil
	}
	code := StatusModified
	if kindOfMode(staged.Mode) != kindOfMode(entry.Mode) {
		code = StatusTypeChanged
	}
	difference.changed[staged.Path] = Entry{Path: staged.Path, Staged: code, Unstaged: StatusUnmodified}
	return nil
}

func (w *Worktree) addRun(difference *headDifference, prefix string, run indexRun) {
	idx := w.currentIndex()
	for position := run.first; position < run.end; position++ {
		if entry := idx.At(position); entry.Stage == index.StageMerged {
			difference.added = append(difference.added, stagedAddition{path: entry.Path, entry: entry})
		}
	}
}

func (w *Worktree) removeTreeEntry(ctx context.Context, difference *headDifference, entry object.TreeEntry, prefix string) error {
	if !entry.Mode.IsTree() {
		difference.removed = append(difference.removed, stagedRemoval{path: prefix + entry.Name, mode: entry.Mode, id: entry.ID})
		return nil
	}
	return w.removeTree(ctx, difference, entry.ID, prefix+entry.Name+"/")
}

func (w *Worktree) removeTree(ctx context.Context, difference *headDifference, treeID hash.ObjectID, prefix string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	tree, err := loadTree(w.db, treeID)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrReadHeadTree, err)
	}
	for _, entry := range tree.Entries {
		if err := w.removeTreeEntry(ctx, difference, entry, prefix); err != nil {
			return err
		}
	}
	return nil
}

func (d *headDifference) entries() map[string]Entry {
	slices.SortFunc(d.added, func(a, b stagedAddition) int { return strings.Compare(a.path, b.path) })
	slices.SortFunc(d.removed, func(a, b stagedRemoval) int { return strings.Compare(a.path, b.path) })
	byID := map[hash.ObjectID][]stagedRemoval{}
	for _, removed := range d.removed {
		byID[removed.id] = append(byID[removed.id], removed)
	}
	used := map[string]bool{}
	for _, added := range d.added {
		matched := ""
		for _, candidate := range byID[added.entry.ID] {
			if used[candidate.path] || candidate.mode.ObjectType() != added.entry.Mode.ObjectType() {
				continue
			}
			matched = candidate.path
			break
		}
		if matched != "" {
			used[matched] = true
			d.changed[added.path] = Entry{Path: added.path, OrigPath: matched, Staged: StatusRenamed, Unstaged: StatusUnmodified}
			continue
		}
		d.changed[added.path] = Entry{Path: added.path, Staged: StatusAdded, Unstaged: StatusUnmodified}
	}
	for _, removed := range d.removed {
		if !used[removed.path] {
			d.changed[removed.path] = Entry{Path: removed.path, Staged: StatusDeleted, Unstaged: StatusUnmodified}
		}
	}
	return d.changed
}

func (w *Worktree) headGitmodules(headTree hash.ObjectID) (hash.ObjectID, error) {
	if headTree.IsZero() {
		return hash.Zero, nil
	}
	tree, err := loadTree(w.db, headTree)
	if err != nil {
		return hash.Zero, fmt.Errorf("%w: %w", ErrReadHeadTree, err)
	}
	entry, found := tree.Find(submodule.GitmodulesFile)
	if !found || entry.Mode.IsTree() {
		return hash.Zero, nil
	}
	return entry.ID, nil
}
