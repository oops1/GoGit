package ops

import (
	"errors"
	"io/fs"
	"maps"
	"path/filepath"
	"slices"

	"github.com/oops1/gogit/internal/gitcore/index"
	"github.com/oops1/gogit/internal/gitcore/merge"
	"github.com/oops1/gogit/internal/gitcore/progress"
)

type outcome struct {
	tree   merge.Snapshot
	stages map[string][]index.Entry
}

func outcomeOf(result merge.TreeResult) outcome {
	out := outcome{tree: result.Tree, stages: map[string][]index.Entry{}}
	for _, c := range result.Conflicts {
		for stage, entry := range []*merge.Entry{c.Base, c.Ours, c.Theirs} {
			if entry == nil {
				continue
			}
			staged := index.Entry{Path: c.Path, Mode: entry.Mode, ID: entry.ID, Stage: index.Stage(stage + 1)}
			out.stages[c.Path] = append(out.stages[c.Path], staged)
		}
	}
	return out
}

func (o outcome) conflicted() []string {
	return slices.Sorted(maps.Keys(o.stages))
}

func (o outcome) changedFrom(from merge.Snapshot) []string {
	var changed []string
	for path := range unionKeys(from, o.tree, o.stages) {
		_, conflicted := o.stages[path]
		before, had := from[path]
		after, has := o.tree[path]
		if conflicted || had != has || before != after {
			changed = append(changed, path)
		}
	}
	slices.Sort(changed)
	return changed
}

func unionKeys[A, B, C any](a map[string]A, b map[string]B, c map[string]C) map[string]bool {
	out := map[string]bool{}
	for key := range a {
		out[key] = true
	}
	for key := range b {
		out[key] = true
	}
	for key := range c {
		out[key] = true
	}
	return out
}

func (m *merger) moveTo(from merge.Snapshot, to outcome, cleanIndex bool) error {
	lock, err := lockIndex(m.r)
	if err != nil {
		return err
	}
	sw := &switcher{ctx: m.ctx, wt: m.wt, db: m.rc.db, format: m.rc.db.Format()}
	changed := to.changedFrom(from)
	checked := changed
	if cleanIndex {
		checked = slices.Sorted(maps.Keys(unionKeys(from, indexByPath(lock.idx), to.tree)))
	}
	blocked, err := blockedPaths(sw, lock.idx, from, checked, changed)
	if err != nil {
		lock.abort()
		return err
	}
	if len(blocked) > 0 {
		lock.abort()
		return &OverwriteError{Paths: blocked}
	}
	m.opts.Progress.Phase(progress.PhaseCheckout)
	if err := applyOutcome(sw, lock.idx, to, changed); err != nil {
		lock.abort()
		return err
	}
	return lock.commit()
}

func blockedPaths(sw *switcher, idx *index.Index, from merge.Snapshot, checked, changed []string) ([]string, error) {
	var blocked []string
	for _, path := range checked {
		if err := sw.ctx.Err(); err != nil {
			return nil, err
		}
		want, had := from[path]
		switch {
		case !indexHolds(idx, path, want, had):
			blocked = append(blocked, path)
			continue
		case !slices.Contains(changed, path):
			continue
		}
		entry, _ := idx.Get(path, index.StageMerged)
		dirty, err := sw.isDirty(path, entry)
		if err != nil {
			return nil, err
		}
		if dirty {
			blocked = append(blocked, path)
		}
	}
	return blocked, nil
}

func indexHolds(idx *index.Index, path string, want merge.Entry, has bool) bool {
	if len(idx.Conflicts(path)) > 0 {
		return false
	}
	entry, staged := idx.Get(path, index.StageMerged)
	return staged == has && (!staged || entry.Mode == want.Mode && entry.ID == want.ID)
}

func (m *merger) resetTo(to merge.Snapshot) error {
	lock, err := lockIndex(m.r)
	if err != nil {
		return err
	}
	sw := &switcher{ctx: m.ctx, wt: m.wt, db: m.rc.db, format: m.rc.db.Format()}
	var changed []string
	for path := range unionKeys(to, indexPaths(lock.idx), map[string]bool{}) {
		want, has := to[path]
		if !indexHolds(lock.idx, path, want, has) {
			changed = append(changed, path)
		}
	}
	slices.Sort(changed)
	if err := applyOutcome(sw, lock.idx, outcome{tree: to}, changed); err != nil {
		lock.abort()
		return err
	}
	return lock.commit()
}

func indexPaths(idx *index.Index) map[string]bool {
	out := map[string]bool{}
	for entry := range idx.Entries() {
		out[entry.Path] = true
	}
	return out
}

func applyOutcome(sw *switcher, idx *index.Index, to outcome, changed []string) error {
	var removed []string
	for _, path := range changed {
		if _, keep := to.tree[path]; keep {
			continue
		}
		if err := fsRootRemove(sw.wt.root, filepath.FromSlash(path)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		removed = append(removed, path)
	}
	for _, path := range removed {
		sw.pruneEmptyDirs(parentOf(path))
	}
	for _, path := range changed {
		if err := sw.ctx.Err(); err != nil {
			return err
		}
		idx.Remove(path)
		entry, keep := to.tree[path]
		if keep {
			if err := sw.checkout(path, treeEntry{mode: entry.Mode, id: entry.ID}); err != nil {
				return err
			}
		}
		if stages, conflicted := to.stages[path]; conflicted {
			for _, staged := range stages {
				idx.Add(staged)
			}
			continue
		}
		if keep {
			idx.Add(index.Entry{Path: path, Mode: entry.Mode, ID: entry.ID, Stage: index.StageMerged})
		}
	}
	return nil
}
