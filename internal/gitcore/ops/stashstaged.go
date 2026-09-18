package ops

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"

	"github.com/oops1/gogit/internal/gitcore/diff"
	"github.com/oops1/gogit/internal/gitcore/merge"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/patch"
)

const stagedPatchContext = 1

type worktreeEdit struct {
	path   string
	remove bool
	mode   object.Mode
	data   []byte
	keep   bool
}

func (m *merger) removeStagedChanges(p *stashPush) error {
	edits, err := m.stagedReversal(p.headState, p.indexed)
	if err != nil {
		return err
	}
	if err := m.applyWorktreeEdits(edits); err != nil {
		return err
	}
	switch {
	case p.opts.KeepIndex:
		return nil
	case !p.spec.Empty():
		return m.resetPaths(p.head.old, p.opts.Paths)
	}
	if err := m.stageOnly(p.headState); err != nil {
		return err
	}
	if err := m.advance(p.head, p.head.old, resetToHeadNote); err != nil {
		return err
	}
	return errors.Join(writeStateFile(m.r, origHeadFile, p.head.old.String()+"\n"), clearMergeState(m.r), forgetMergeRR(m.r))
}

func (m *merger) stagedReversal(head, indexed merge.Snapshot) ([]worktreeEdit, error) {
	sw := &switcher{ctx: m.ctx, wt: m.wt, db: m.rc.db, format: m.rc.db.Format()}
	var edits []worktreeEdit
	for _, path := range slices.Sorted(maps.Keys(unionKeys(head, indexed, map[string]bool{}))) {
		if err := m.ctx.Err(); err != nil {
			return nil, err
		}
		before, had := head[path]
		after, has := indexed[path]
		if had == has && before == after || before.Mode.IsSubmodule() || after.Mode.IsSubmodule() {
			continue
		}
		edit, err := m.reverseStaged(sw, path, before, had, after, has)
		if err != nil {
			return nil, err
		}
		edits = append(edits, edit)
	}
	return edits, nil
}

func (m *merger) reverseStaged(sw *switcher, path string, before merge.Entry, had bool, after merge.Entry, has bool) (worktreeEdit, error) {
	info, err := fsRootLstat(m.wt.root, filepath.FromSlash(path))
	exists := err == nil
	if err != nil && !missingPath(err) {
		return worktreeEdit{}, err
	}
	edit := worktreeEdit{path: path, mode: before.Mode}
	switch {
	case !has && exists:
		return edit, fmt.Errorf("%w: %s already exists in the working tree", ErrStashWorktreeKept, path)
	case !has:
		_, edit.data, err = dbGet(m.rc.db, before.ID)
		return edit, err
	case !exists:
		return edit, fmt.Errorf("%w: %s does not exist in the working tree", ErrStashWorktreeKept, path)
	case m.wt.symlinks && info.Mode()&os.ModeSymlink != 0 != after.Mode.IsSymlink():
		return edit, fmt.Errorf("%w: %s on disk does not have the type of the index entry", ErrStashWorktreeKept, path)
	}
	current, err := sw.readWorktreeBytes(path, info)
	if err != nil {
		return edit, err
	}
	id, err := hashSum(m.rc.db.Format(), "blob", current)
	if err != nil {
		return edit, err
	}
	switch {
	case !had && id == after.ID:
		edit.remove = true
		return edit, nil
	case !had:
		return edit, fmt.Errorf("%w: %s: %w", ErrStashWorktreeKept, path, diff.ErrApply)
	case before.ID == after.ID:
		edit.keep = true
		return edit, nil
	case id == after.ID:
		_, edit.data, err = dbGet(m.rc.db, before.ID)
		return edit, err
	}
	edit.data, err = m.reverseHunks(path, before, after, current)
	return edit, err
}

func (m *merger) reverseHunks(path string, before, after merge.Entry, current []byte) ([]byte, error) {
	if !sameEntryType(before.Mode, after.Mode) {
		return nil, fmt.Errorf("%w: %s: %w", ErrStashWorktreeKept, path, diff.ErrApply)
	}
	_, oldData, err := dbGet(m.rc.db, before.ID)
	if err != nil {
		return nil, err
	}
	_, newData, err := dbGet(m.rc.db, after.ID)
	if err != nil {
		return nil, err
	}
	hunks := diff.Blobs(oldData, newData, diff.Options{Context: stagedPatchContext, IndentHeuristic: true})
	data, err := applyHunks(current, patch.Reverse(hunks))
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrStashWorktreeKept, path, err)
	}
	return data, nil
}

func (m *merger) applyWorktreeEdits(edits []worktreeEdit) error {
	sw := &switcher{ctx: m.ctx, wt: m.wt}
	for _, edit := range edits {
		name := filepath.FromSlash(edit.path)
		var err error
		switch {
		case edit.remove:
			err = fsRootRemove(m.wt.root, name)
			sw.pruneEmptyDirs(parentOf(edit.path))
		case edit.keep:
			err = fixExecutable(m.wt, name, edit.mode == object.ModeExecutable)
		default:
			err = m.wt.writeCheckedOut(edit.path, edit.mode, edit.data, nil)
		}
		if err != nil {
			return err
		}
	}
	return nil
}
