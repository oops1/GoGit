package worktree

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/index"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/refs"
)

type headEntry struct {
	Mode object.Mode
	ID   hash.ObjectID
}

const maxHeadHops = 5

func (w *Worktree) resolveHead() (branch string, detached bool, headCommit hash.ObjectID, err error) {
	name := refs.HEAD
	for range maxHeadHops {
		ref, err := w.refs.Lookup(name)
		switch {
		case errors.Is(err, refs.ErrNotFound):
			if name == refs.HEAD {
				return "", true, hash.Zero, nil
			}
			return name.Short(), false, hash.Zero, nil
		case err != nil:
			return "", false, hash.Zero, err
		case !ref.IsSymbolic():
			if name == refs.HEAD {
				return "", true, ref.Target, nil
			}
			return name.Short(), false, ref.Target, nil
		default:
			name = ref.SymbolicTarget
		}
	}
	return "", false, hash.Zero, fmt.Errorf("%w: HEAD", refs.ErrTooManySymlinks)
}

func (w *Worktree) collectHeadTree(ctx context.Context, id hash.ObjectID) (map[string]headEntry, error) {
	out := map[string]headEntry{}
	if id.IsZero() {
		return out, nil
	}
	type frame struct {
		id     hash.ObjectID
		prefix string
	}
	stack := []frame{{id: id}}
	for len(stack) > 0 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		top := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		tree, err := w.db.Tree(top.id)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrReadHeadTree, err)
		}
		for _, entry := range tree.Entries {
			entryPath := top.prefix + entry.Name
			if entry.Mode.IsTree() {
				stack = append(stack, frame{id: entry.ID, prefix: entryPath + "/"})
				continue
			}
			out[entryPath] = headEntry{Mode: entry.Mode, ID: entry.ID}
		}
	}
	return out, nil
}

func (w *Worktree) stagedStatus(headTree map[string]headEntry) map[string]Entry {
	indexPaths := map[string]*index.Entry{}
	for entry := range w.index.Entries() {
		if entry.Stage == index.StageMerged {
			indexPaths[entry.Path] = entry
		}
	}

	entries := map[string]Entry{}
	var deletedPaths []string
	for headPath, he := range headTree {
		ie, ok := indexPaths[headPath]
		if !ok {
			deletedPaths = append(deletedPaths, headPath)
			continue
		}
		if ie.Mode == he.Mode && ie.ID == he.ID {
			continue
		}
		code := StatusModified
		if kindOfMode(ie.Mode) != kindOfMode(he.Mode) {
			code = StatusTypeChanged
		}
		entries[headPath] = Entry{Path: headPath, Staged: code, Unstaged: StatusUnmodified}
	}
	var addedPaths []string
	for indexPath := range indexPaths {
		if _, ok := headTree[indexPath]; !ok {
			addedPaths = append(addedPaths, indexPath)
		}
	}
	slices.Sort(addedPaths)
	slices.Sort(deletedPaths)

	byID := map[hash.ObjectID][]string{}
	for _, deleted := range deletedPaths {
		byID[headTree[deleted].ID] = append(byID[headTree[deleted].ID], deleted)
	}
	used := map[string]bool{}
	for _, added := range addedPaths {
		ie := indexPaths[added]
		matched := ""
		for _, candidate := range byID[ie.ID] {
			if used[candidate] {
				continue
			}
			if headTree[candidate].Mode.ObjectType() != ie.Mode.ObjectType() {
				continue
			}
			matched = candidate
			break
		}
		if matched != "" {
			used[matched] = true
			entries[added] = Entry{Path: added, OrigPath: matched, Staged: StatusRenamed, Unstaged: StatusUnmodified}
			continue
		}
		entries[added] = Entry{Path: added, Staged: StatusAdded, Unstaged: StatusUnmodified}
	}
	for _, deleted := range deletedPaths {
		if used[deleted] {
			continue
		}
		entries[deleted] = Entry{Path: deleted, Staged: StatusDeleted, Unstaged: StatusUnmodified}
	}
	return entries
}
