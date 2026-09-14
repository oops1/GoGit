package ops

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/index"
	"github.com/oops1/gogit/internal/gitcore/merge"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/repo"
)

type StashOptions struct {
	Message string
	When    time.Time
}

type StashEntry struct {
	Index   int
	Commit  hash.ObjectID
	Message string
	When    time.Time
}

func (e StashEntry) Selector() string {
	return "stash@{" + strconv.Itoa(e.Index) + "}"
}

type StashApplyResult struct {
	Conflicts []string
	Dropped   bool
}

func (r StashApplyResult) Clean() bool { return len(r.Conflicts) == 0 }

type SwitchMergeResult struct {
	Stashed   bool
	Conflicts []string
}

func (r SwitchMergeResult) Clean() bool { return len(r.Conflicts) == 0 }

const (
	stashIndexPrefix = "index on "
	stashWorkPrefix  = "WIP on "
	stashNamedPrefix = "On "
	stashNoBranch    = "(no branch)"
	stashBaseLabel   = "Stash base"
	stashOursLabel   = "Updated upstream"
	stashTheirsLabel = "Stashed changes"
)

func StashPush(ctx context.Context, r *repo.Repository, opts StashOptions) (hash.ObjectID, error) {
	m, err := openMerger(ctx, r, MergeOptions{When: opts.When})
	if err != nil {
		return hash.Zero, err
	}
	defer m.close()
	return m.stashPush(opts.Message)
}

func StashList(ctx context.Context, r *repo.Repository) ([]StashEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	rc, err := openRepoContext(r)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.close() }()
	return stashEntries(rc.refs)
}

func StashApply(ctx context.Context, r *repo.Repository, position int) (StashApplyResult, error) {
	m, err := openMerger(ctx, r, MergeOptions{})
	if err != nil {
		return StashApplyResult{}, err
	}
	defer m.close()
	return m.stashApply(position)
}

func StashPop(ctx context.Context, r *repo.Repository, position int) (StashApplyResult, error) {
	m, err := openMerger(ctx, r, MergeOptions{})
	if err != nil {
		return StashApplyResult{}, err
	}
	defer m.close()
	result, err := m.stashApply(position)
	if err != nil || !result.Clean() {
		return result, err
	}
	if err := dropStash(m.rc.refs, position); err != nil {
		return result, err
	}
	result.Dropped = true
	return result, nil
}

func StashDrop(ctx context.Context, r *repo.Repository, position int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	rc, err := openRepoContext(r)
	if err != nil {
		return err
	}
	defer func() { _ = rc.close() }()
	return dropStash(rc.refs, position)
}

func SwitchMerging(ctx context.Context, r *repo.Repository, target string) (SwitchMergeResult, error) {
	_, err := StashPush(ctx, r, StashOptions{})
	if errors.Is(err, ErrNothingToStash) {
		return SwitchMergeResult{}, Switch(ctx, r, target, SwitchOptions{})
	}
	if err != nil {
		return SwitchMergeResult{}, err
	}
	result := SwitchMergeResult{Stashed: true}
	if err := Switch(ctx, r, target, SwitchOptions{}); err != nil {
		_, restoreErr := StashPop(context.WithoutCancel(ctx), r, 0)
		return result, errors.Join(err, restoreErr)
	}
	applied, err := StashPop(context.WithoutCancel(ctx), r, 0)
	result.Conflicts = applied.Conflicts
	return result, err
}

func stashEntries(store *refs.Store) ([]StashEntry, error) {
	var entries []StashEntry
	for entry, err := range store.Reflog(refs.StashName) {
		if err != nil {
			return nil, err
		}
		entries = append(entries, StashEntry{Commit: entry.New, Message: entry.Message, When: entry.Committer.When})
	}
	slices.Reverse(entries)
	for i := range entries {
		entries[i].Index = i
	}
	return entries, nil
}

func dropStash(store *refs.Store, position int) error {
	err := store.DropReflogEntry(refs.StashName, position)
	if errors.Is(err, refs.ErrNotFound) {
		return fmt.Errorf("%w: stash@{%d}", ErrStashNotFound, position)
	}
	return err
}

func (m *merger) stashPush(message string) (hash.ObjectID, error) {
	if err := m.rc.requireIdentity(); err != nil {
		return hash.Zero, err
	}
	head, err := resolveHeadTarget(m.rc.refs)
	if err != nil {
		return hash.Zero, err
	}
	if head.old.IsZero() {
		return hash.Zero, ErrUnbornHead
	}
	base, err := dbCommit(m.rc.db, head.old)
	if err != nil {
		return hash.Zero, err
	}
	idx, err := readIndex(m.r)
	if err != nil {
		return hash.Zero, err
	}
	if idx.HasConflicts() {
		return hash.Zero, ErrUnmergedPaths
	}
	indexTree, err := idx.WriteTree(m.rc.db)
	if err != nil {
		return hash.Zero, err
	}
	headState, err := merge.Read(m.store(), base.Tree)
	if err != nil {
		return hash.Zero, err
	}
	workTree, err := m.stashWorkTree(idx, headState)
	if err != nil {
		return hash.Zero, err
	}
	if indexTree == base.Tree && workTree == base.Tree {
		return hash.Zero, ErrNothingToStash
	}
	previous, err := stashTip(m.rc.refs)
	if err != nil {
		return hash.Zero, err
	}
	label := stashBranch(head) + ": " + abbreviate(head.old) + " " + stashSubject(base.Message)
	sig := m.rc.sig
	sig.When = m.opts.When
	indexCommit, err := dbPutObject(m.rc.db, &object.Commit{
		Tree: indexTree, Parents: []hash.ObjectID{head.old}, Author: sig, Committer: sig,
		Message: stashIndexPrefix + label + "\n",
	})
	if err != nil {
		return hash.Zero, err
	}
	note := stashWorkPrefix + label
	if message != "" {
		note = stashNamedPrefix + stashBranch(head) + ": " + message
	}
	stash, err := dbPutObject(m.rc.db, &object.Commit{
		Tree: workTree, Parents: []hash.ObjectID{head.old, indexCommit}, Author: sig, Committer: sig,
		Message: note,
	})
	if err != nil {
		return hash.Zero, err
	}
	tx := m.rc.refs.Begin()
	tx.SetMessage(note)
	if err := txUpdate(tx, refs.StashName, stash, previous); err != nil {
		tx.Rollback()
		return hash.Zero, err
	}
	if err := txCommit(tx); err != nil {
		return hash.Zero, err
	}
	if err := m.restore(headState); err != nil {
		return stash, err
	}
	if err := m.advance(head, head.old, resetToHeadNote); err != nil {
		return stash, err
	}
	return stash, errors.Join(writeStateFile(m.r, origHeadFile, head.old.String()+"\n"), clearMergeState(m.r), forgetMergeRR(m.r))
}

func stashTip(store *refs.Store) (hash.ObjectID, error) {
	ref, err := refsLookup(store, refs.StashName)
	if errors.Is(err, refs.ErrNotFound) {
		return hash.Zero, nil
	}
	if err != nil {
		return hash.Zero, err
	}
	return ref.Target, nil
}

func stashBranch(head headTarget) string {
	if head.detached {
		return stashNoBranch
	}
	return head.ref.Short()
}

func stashSubject(message string) string {
	var lines []string
	for line := range strings.Lines(message) {
		if strings.TrimSpace(line) == "" {
			if len(lines) > 0 {
				break
			}
			continue
		}
		lines = append(lines, strings.TrimRight(line, " \t\r\n"))
	}
	return strings.Join(lines, " ")
}

func (m *merger) stashWorkTree(idx *index.Index, head merge.Snapshot) (hash.ObjectID, error) {
	sw := &switcher{ctx: m.ctx, wt: m.wt, db: m.rc.db, format: m.rc.db.Format()}
	st := &stager{ctx: m.ctx, wt: m.wt, db: m.rc.db, idx: idx}
	for _, path := range slices.Sorted(maps.Keys(unionKeys(head, indexPaths(idx), map[string]bool{}))) {
		if err := m.ctx.Err(); err != nil {
			return hash.Zero, err
		}
		if entry, staged := idx.Get(path, index.StageMerged); staged {
			dirty, err := sw.isDirty(path, entry, nil)
			if err != nil {
				return hash.Zero, err
			}
			if !dirty {
				continue
			}
		}
		info, err := fsRootLstat(m.wt.root, filepath.FromSlash(path))
		switch {
		case missingPath(err) || err == nil && info.IsDir():
			idx.Remove(path)
		case err != nil:
			return hash.Zero, err
		default:
			if err := st.stageEntry(path, info); err != nil {
				return hash.Zero, err
			}
		}
	}
	return idx.WriteTree(m.rc.db)
}

func (m *merger) stashApply(position int) (StashApplyResult, error) {
	stash, err := m.stashCommit(position)
	if err != nil {
		return StashApplyResult{}, err
	}
	base, err := m.treeOf(stash.Parents[0])
	if err != nil {
		return StashApplyResult{}, err
	}
	idx, err := readIndex(m.r)
	if err != nil {
		return StashApplyResult{}, err
	}
	if idx.HasConflicts() {
		return StashApplyResult{}, ErrUnmergedPaths
	}
	oursTree, err := idx.WriteTree(m.rc.db)
	if err != nil {
		return StashApplyResult{}, err
	}
	ours, err := merge.Read(m.store(), oursTree)
	if err != nil {
		return StashApplyResult{}, err
	}
	labels := merge.Labels{Base: stashBaseLabel, Ours: stashOursLabel, Theirs: stashTheirsLabel}
	merged, err := m.mergeTrees(base, oursTree, stash.Tree, labels, 0)
	if err != nil {
		return StashApplyResult{}, err
	}
	to := outcomeOf(merged)
	if err := m.moveTo(ours, to, false); err != nil {
		return StashApplyResult{}, err
	}
	if conflicts := to.conflicted(); len(conflicts) > 0 {
		return StashApplyResult{Conflicts: conflicts}, nil
	}
	return StashApplyResult{}, m.unstageApplied(ours, to)
}

func (m *merger) stashCommit(position int) (*object.Commit, error) {
	entries, err := stashEntries(m.rc.refs)
	if err != nil {
		return nil, err
	}
	if position < 0 || position >= len(entries) {
		return nil, fmt.Errorf("%w: stash@{%d}", ErrStashNotFound, position)
	}
	stash, err := dbCommit(m.rc.db, entries[position].Commit)
	if err != nil {
		return nil, err
	}
	if len(stash.Parents) < 2 {
		return nil, fmt.Errorf("%w: %s", ErrNotAStash, entries[position].Selector())
	}
	return stash, nil
}

func (m *merger) unstageApplied(ours merge.Snapshot, to outcome) error {
	lock, err := lockIndex(m.r)
	if err != nil {
		return err
	}
	var paths []string
	var entries []index.Entry
	for _, path := range to.changedFrom(ours) {
		entry, had := ours[path]
		if !had {
			continue
		}
		paths = append(paths, path)
		entries = append(entries, index.Entry{Path: path, Mode: entry.Mode, ID: entry.ID, Stage: index.StageMerged})
	}
	lock.idx.Replace(paths, entries)
	return lock.commit()
}
