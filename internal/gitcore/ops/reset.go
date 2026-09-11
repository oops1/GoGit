package ops

import (
	"context"
	"maps"
	"slices"
	"time"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/index"
	"github.com/oops1/gogit/internal/gitcore/merge"
	"github.com/oops1/gogit/internal/gitcore/progress"
	"github.com/oops1/gogit/internal/gitcore/repo"
)

type ResetMode int

const (
	ResetMixed ResetMode = iota
	ResetSoft
	ResetHard
)

type ResetOptions struct {
	Mode     ResetMode
	Paths    []string
	When     time.Time
	Progress progress.Func
}

type ResetResult struct {
	Old hash.ObjectID
	New hash.ObjectID
}

func Reset(ctx context.Context, r *repo.Repository, target string, opts ResetOptions) (ResetResult, error) {
	m, err := openMerger(ctx, r, MergeOptions{When: opts.When, Progress: opts.Progress})
	if err != nil {
		return ResetResult{}, err
	}
	defer m.close()
	return m.reset(target, opts)
}

func (m *merger) reset(target string, opts ResetOptions) (ResetResult, error) {
	if target == "" {
		target = oursLabel
	}
	commit, _, err := m.resolve(target)
	if err != nil {
		return ResetResult{}, err
	}
	if len(opts.Paths) > 0 {
		if opts.Mode != ResetMixed {
			return ResetResult{}, ErrResetPathsWithMode
		}
		return ResetResult{Old: commit, New: commit}, m.resetPaths(commit, opts.Paths)
	}
	if opts.Mode == ResetSoft {
		if err := m.refuseSoftResetWhileMerging(); err != nil {
			return ResetResult{}, err
		}
	}
	head, err := resolveHeadTarget(m.rc.refs)
	if err != nil {
		return ResetResult{}, err
	}
	result := ResetResult{Old: head.old, New: commit}
	if err := m.rewind(commit, opts.Mode); err != nil {
		return result, err
	}
	if !head.old.IsZero() {
		if err := writeStateFile(m.r, origHeadFile, head.old.String()+"\n"); err != nil {
			return result, err
		}
	}
	if err := m.advance(head, commit, resetNotePrefix+target); err != nil {
		return result, err
	}
	return result, clearMergeState(m.r)
}

func (m *merger) refuseSoftResetWhileMerging() error {
	state, err := ReadMergeState(m.r)
	if err != nil {
		return err
	}
	if len(state.Heads) > 0 {
		return ErrMergeInProgress
	}
	return nil
}

func (m *merger) rewind(commit hash.ObjectID, mode ResetMode) error {
	if mode == ResetSoft {
		return nil
	}
	to, _, err := m.snapshot(commit)
	if err != nil {
		return err
	}
	if mode == ResetHard {
		return m.restore(to)
	}
	return m.stageOnly(to)
}

func (m *merger) stageOnly(to merge.Snapshot) error {
	lock, err := lockIndex(m.r)
	if err != nil {
		return err
	}
	for _, path := range slices.Sorted(maps.Keys(unionKeys(to, indexPaths(lock.idx), map[string]bool{}))) {
		if err := m.ctx.Err(); err != nil {
			lock.abort()
			return err
		}
		want, has := to[path]
		if indexHolds(lock.idx, path, want, has) {
			continue
		}
		lock.idx.Remove(path)
		if has {
			lock.idx.Add(index.Entry{Path: path, Mode: want.Mode, ID: want.ID, Stage: index.StageMerged})
		}
	}
	return lock.commit()
}

func (m *merger) restore(to merge.Snapshot) error {
	lock, err := lockIndex(m.r)
	if err != nil {
		return err
	}
	sw := &switcher{ctx: m.ctx, wt: m.wt, db: m.rc.db, format: m.rc.db.Format()}
	changed, err := staleOrDirtyPaths(sw, lock.idx, to)
	if err != nil {
		lock.abort()
		return err
	}
	m.opts.Progress.Phase(progress.PhaseCheckout)
	if err := applyOutcome(sw, lock.idx, outcome{tree: to}, changed); err != nil {
		lock.abort()
		return err
	}
	return lock.commit()
}

func staleOrDirtyPaths(sw *switcher, idx *index.Index, to merge.Snapshot) ([]string, error) {
	var changed []string
	for path := range unionKeys(to, indexPaths(idx), map[string]bool{}) {
		if err := sw.ctx.Err(); err != nil {
			return nil, err
		}
		want, has := to[path]
		if !indexHolds(idx, path, want, has) {
			changed = append(changed, path)
			continue
		}
		entry, _ := idx.Get(path, index.StageMerged)
		dirty, err := sw.isDirty(path, entry, func(inside string) bool { _, ok := idx.Get(inside, index.StageMerged); return ok })
		if err != nil {
			return nil, err
		}
		if dirty {
			changed = append(changed, path)
		}
	}
	slices.Sort(changed)
	return changed, nil
}

func (m *merger) resetPaths(commit hash.ObjectID, paths []string) error {
	tree, err := commitTreeEntries(m.rc.db, commit)
	if err != nil {
		return err
	}
	lock, err := lockIndex(m.r)
	if err != nil {
		return err
	}
	for _, path := range paths {
		if err := m.ctx.Err(); err != nil {
			lock.abort()
			return err
		}
		clean, err := cleanRepoPath(path)
		if err != nil {
			lock.abort()
			return err
		}
		unstagePath(lock.idx, tree, clean)
	}
	return lock.commit()
}
