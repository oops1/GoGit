package ops

import (
	"fmt"
	"maps"
	"slices"

	"github.com/oops1/gogit/internal/gitcore/diff"
	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/merge"
	"github.com/oops1/gogit/internal/gitcore/object"
)

func (m *merger) reinstateIndex(ours merge.Snapshot, parts stashParts) (hash.ObjectID, error) {
	base, err := merge.Read(m.store(), parts.base)
	if err != nil {
		return hash.Zero, err
	}
	stashed, err := merge.Read(m.store(), parts.index)
	if err != nil {
		return hash.Zero, err
	}
	target := maps.Clone(ours)
	for _, path := range slices.Sorted(maps.Keys(unionKeys(base, stashed, map[string]bool{}))) {
		if err := m.ctx.Err(); err != nil {
			return hash.Zero, err
		}
		before, had := base[path]
		after, has := stashed[path]
		if had == has && before == after {
			continue
		}
		if err := m.patchIndexPath(target, path, before, had, after, has); err != nil {
			return hash.Zero, fmt.Errorf("%w: %w", ErrStashIndexConflicts, err)
		}
	}
	tree, err := target.Write(m.store())
	if err != nil {
		return hash.Zero, err
	}
	head, err := resolveHeadTarget(m.rc.refs)
	if err != nil {
		return hash.Zero, err
	}
	if err := writeStateFile(m.r, origHeadFile, head.old.String()+"\n"); err != nil {
		return hash.Zero, err
	}
	return tree, m.advance(head, head.old, resetToHeadNote)
}

func (m *merger) patchIndexPath(target merge.Snapshot, path string, before merge.Entry, had bool, after merge.Entry, has bool) error {
	current, present := target[path]
	switch {
	case !had && present:
		return fmt.Errorf("%s: already exists in index", path)
	case !had:
		target[path] = after
		return nil
	case !present:
		return fmt.Errorf("%s: does not exist in index", path)
	case !has && current.ID != before.ID:
		return fmt.Errorf("%s: %w", path, diff.ErrApply)
	case !has:
		delete(target, path)
		return nil
	case before.ID == after.ID:
		target[path] = merge.Entry{Mode: after.Mode, ID: current.ID}
		return nil
	case current.ID == before.ID:
		target[path] = after
		return nil
	}
	data, err := m.forwardHunks(path, before, after, current)
	if err != nil {
		return err
	}
	id, err := dbPut(m.rc.db, object.TypeBlob, data)
	if err != nil {
		return err
	}
	target[path] = merge.Entry{Mode: after.Mode, ID: id}
	return nil
}

func (m *merger) forwardHunks(path string, before, after, current merge.Entry) ([]byte, error) {
	if !before.Mode.IsRegular() || !after.Mode.IsRegular() || !current.Mode.IsRegular() {
		return nil, fmt.Errorf("%s: %w", path, diff.ErrApply)
	}
	var blobs [3][]byte
	for i, id := range []hash.ObjectID{before.ID, after.ID, current.ID} {
		_, data, err := dbGet(m.rc.db, id)
		if err != nil {
			return nil, err
		}
		blobs[i] = data
	}
	if looksBinary(blobs[0]) || looksBinary(blobs[1]) {
		return nil, fmt.Errorf("%s: %w", path, diff.ErrApply)
	}
	data, err := applyHunks(blobs[2], diff.Blobs(blobs[0], blobs[1], diff.Defaults()))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return data, nil
}

func (m *merger) readTreeIntoIndex(tree hash.ObjectID) error {
	to, err := merge.Read(m.store(), tree)
	if err != nil {
		return err
	}
	return m.stageOnly(to)
}
