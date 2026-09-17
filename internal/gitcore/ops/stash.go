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
	"github.com/oops1/gogit/internal/gitcore/pathspec"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/repo"
)

type StashOptions struct {
	Message          string
	When             time.Time
	IncludeUntracked bool
	IncludeIgnored   bool
	KeepIndex        bool
	Staged           bool
	Paths            []string
}

func (o StashOptions) untracked() bool { return o.IncludeUntracked || o.IncludeIgnored }

type StashApplyOptions struct {
	Index bool
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
	Conflicts     []string
	Warnings      []merge.Warning
	IndexRestored bool
	Dropped       bool
}

func (r StashApplyResult) Clean() bool {
	return len(r.Conflicts) == 0 && !slices.ContainsFunc(r.Warnings, merge.Warning.Unclean)
}

type SwitchMergeResult struct {
	Stashed   bool
	Conflicts []string
}

func (r SwitchMergeResult) Clean() bool { return len(r.Conflicts) == 0 }

const (
	stashIndexPrefix     = "index on "
	stashUntrackedPrefix = "untracked files on "
	stashWorkPrefix      = "WIP on "
	stashNamedPrefix     = "On "
	stashNoBranch        = "(no branch)"
	stashBaseLabel       = "Stash base"
	stashOursLabel       = "Updated upstream"
	stashTheirsLabel     = "Stashed changes"
	stashUntrackedParent = 2
)

func StashPush(ctx context.Context, r *repo.Repository, opts StashOptions) (hash.ObjectID, error) {
	m, err := openMerger(ctx, r, MergeOptions{When: opts.When})
	if err != nil {
		return hash.Zero, err
	}
	defer m.close()
	return m.stashPush(opts)
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

func StashApply(ctx context.Context, r *repo.Repository, position int, opts StashApplyOptions) (StashApplyResult, error) {
	m, err := openMerger(ctx, r, MergeOptions{})
	if err != nil {
		return StashApplyResult{}, err
	}
	defer m.close()
	return m.stashApply(position, opts)
}

func StashPop(ctx context.Context, r *repo.Repository, position int, opts StashApplyOptions) (StashApplyResult, error) {
	m, err := openMerger(ctx, r, MergeOptions{})
	if err != nil {
		return StashApplyResult{}, err
	}
	defer m.close()
	result, err := m.stashApply(position, opts)
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
		_, restoreErr := StashPop(context.WithoutCancel(ctx), r, 0, StashApplyOptions{})
		return result, errors.Join(err, restoreErr)
	}
	applied, err := StashPop(context.WithoutCancel(ctx), r, 0, StashApplyOptions{})
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

type stashPush struct {
	opts      StashOptions
	spec      pathspec.Set
	head      headTarget
	base      *object.Commit
	headState merge.Snapshot
	indexTree hash.ObjectID
	indexed   merge.Snapshot
	workTree  hash.ObjectID
	untracked []string
	walk      *untrackedWalk
}

func (m *merger) stashPush(opts StashOptions) (hash.ObjectID, error) {
	if opts.Staged && opts.untracked() {
		return hash.Zero, ErrStashStagedUntracked
	}
	spec, err := pathspec.Parse(opts.Paths)
	if err != nil {
		return hash.Zero, fmt.Errorf("%w: %w", ErrInvalidPath, err)
	}
	if err := m.rc.requireIdentity(); err != nil {
		return hash.Zero, err
	}
	p := &stashPush{opts: opts, spec: spec}
	if err := m.readStashSources(p); err != nil {
		return hash.Zero, err
	}
	previous, err := stashTip(m.rc.refs)
	if err != nil {
		return hash.Zero, err
	}
	stash, err := m.writeStashCommits(p)
	if err != nil {
		return hash.Zero, err
	}
	note := stashNote(p)
	tx := m.rc.refs.Begin()
	tx.SetMessage(note)
	if err := txUpdate(tx, refs.StashName, stash, previous); err != nil {
		tx.Rollback()
		return hash.Zero, err
	}
	if err := txCommit(tx); err != nil {
		return hash.Zero, err
	}
	if opts.Staged {
		return stash, m.removeStagedChanges(p)
	}
	return stash, m.removeStashedChanges(p)
}

func (m *merger) readStashSources(p *stashPush) error {
	head, err := resolveHeadTarget(m.rc.refs)
	if err != nil {
		return err
	}
	if head.old.IsZero() {
		return ErrUnbornHead
	}
	p.head = head
	if p.base, err = dbCommit(m.rc.db, head.old); err != nil {
		return err
	}
	idx, err := readIndex(m.r)
	if err != nil {
		return err
	}
	if idx.HasConflicts() {
		return ErrUnmergedPaths
	}
	if !p.opts.untracked() {
		if err := requireKnownPaths(idx, p.opts.Paths); err != nil {
			return err
		}
	}
	if p.indexTree, err = idx.WriteTree(m.rc.db); err != nil {
		return err
	}
	if p.headState, err = merge.Read(m.store(), p.base.Tree); err != nil {
		return err
	}
	if p.indexed, err = merge.Read(m.store(), p.indexTree); err != nil {
		return err
	}
	if p.opts.untracked() {
		p.walk = newUntrackedWalk(m, idx, p.spec, p.opts.IncludeIgnored)
		if p.untracked, err = p.walk.list(); err != nil {
			return err
		}
	}
	if p.workTree, err = m.stashWorkTree(idx, p.headState, p.spec); err != nil {
		return err
	}
	if !stagedWithin(p.headState, p.indexed, p.spec) && p.workTree == p.indexTree && len(p.untracked) == 0 {
		return ErrNothingToStash
	}
	if p.opts.Staged {
		if p.indexTree == p.base.Tree {
			return ErrNoStagedChanges
		}
		p.workTree = p.indexTree
	}
	return nil
}

func requireKnownPaths(idx *index.Index, specs []string) error {
	var unknown []string
	paths := slices.Collect(idx.Paths(""))
	for _, spec := range specs {
		single, _ := pathspec.Parse([]string{spec})
		if !slices.ContainsFunc(paths, single.Match) {
			unknown = append(unknown, spec)
		}
	}
	if len(unknown) > 0 {
		return &PathspecError{Specs: unknown}
	}
	return nil
}

func stagedWithin(head, indexed merge.Snapshot, spec pathspec.Set) bool {
	for path := range unionKeys(head, indexed, map[string]bool{}) {
		before, had := head[path]
		after, has := indexed[path]
		if (had != has || before != after) && spec.Match(path) {
			return true
		}
	}
	return false
}

func (m *merger) writeStashCommits(p *stashPush) (hash.ObjectID, error) {
	label := stashLabel(p)
	sig := m.rc.sig
	sig.When = m.opts.When
	parents := []hash.ObjectID{p.head.old}
	indexCommit, err := dbPutObject(m.rc.db, &object.Commit{
		Tree: p.indexTree, Parents: parents, Author: sig, Committer: sig,
		Message: stashIndexPrefix + label + "\n",
	})
	if err != nil {
		return hash.Zero, err
	}
	parents = append(parents, indexCommit)
	if len(p.untracked) > 0 {
		tree, err := m.untrackedTree(p.untracked)
		if err != nil {
			return hash.Zero, err
		}
		untrackedCommit, err := dbPutObject(m.rc.db, &object.Commit{
			Tree: tree, Author: sig, Committer: sig,
			Message: stashUntrackedPrefix + label + "\n",
		})
		if err != nil {
			return hash.Zero, err
		}
		parents = append(parents, untrackedCommit)
	}
	return dbPutObject(m.rc.db, &object.Commit{
		Tree: p.workTree, Parents: parents, Author: sig, Committer: sig,
		Message: stashNote(p),
	})
}

func stashLabel(p *stashPush) string {
	return stashBranch(p.head) + ": " + abbreviate(p.head.old) + " " + stashSubject(p.base.Message)
}

func stashNote(p *stashPush) string {
	if p.opts.Message != "" {
		return stashNamedPrefix + stashBranch(p.head) + ": " + p.opts.Message
	}
	return stashWorkPrefix + stashLabel(p)
}

func (m *merger) removeStashedChanges(p *stashPush) error {
	if !p.spec.Empty() {
		if err := m.restoreMatching(p.headState, p.spec); err != nil {
			return err
		}
		if err := m.removeFiles(p.untracked); err != nil {
			return err
		}
		return m.keepStashedIndex(p)
	}
	if p.opts.untracked() {
		if err := p.walk.cleanAll(); err != nil {
			return err
		}
	}
	if err := m.resetHard(p.head, p.headState); err != nil {
		return err
	}
	return m.keepStashedIndex(p)
}

func (m *merger) resetHard(head headTarget, to merge.Snapshot) error {
	if err := m.restore(to); err != nil {
		return err
	}
	if err := m.advance(head, head.old, resetToHeadNote); err != nil {
		return err
	}
	return errors.Join(writeStateFile(m.r, origHeadFile, head.old.String()+"\n"), clearMergeState(m.r), forgetMergeRR(m.r))
}

func (m *merger) keepStashedIndex(p *stashPush) error {
	if !p.opts.KeepIndex {
		return nil
	}
	return m.restoreMatching(p.indexed, p.spec)
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

func (m *merger) stashWorkTree(idx *index.Index, head merge.Snapshot, spec pathspec.Set) (hash.ObjectID, error) {
	sw := &switcher{ctx: m.ctx, wt: m.wt, db: m.rc.db, format: m.rc.db.Format()}
	st := &stager{ctx: m.ctx, wt: m.wt, db: m.rc.db, idx: idx}
	for _, path := range slices.Sorted(maps.Keys(unionKeys(head, indexPaths(idx), map[string]bool{}))) {
		if err := m.ctx.Err(); err != nil {
			return hash.Zero, err
		}
		if !spec.Match(path) {
			continue
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

type stashParts struct {
	entry     StashEntry
	stash     *object.Commit
	base      hash.ObjectID
	index     hash.ObjectID
	untracked hash.ObjectID
}

func (m *merger) stashApply(position int, opts StashApplyOptions) (StashApplyResult, error) {
	parts, err := readStashParts(m.rc, position)
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
	var restored hash.ObjectID
	if opts.Index && parts.index != parts.base && parts.index != oursTree {
		if restored, err = m.reinstateIndex(ours, parts); err != nil {
			return StashApplyResult{}, err
		}
	}
	labels := merge.Labels{Base: stashBaseLabel, Ours: stashOursLabel, Theirs: stashTheirsLabel}
	merged, err := m.mergeTrees(parts.base, oursTree, parts.stash.Tree, labels, 0)
	if err != nil {
		return StashApplyResult{}, err
	}
	to := outcomeOf(merged)
	if err := m.moveTo(ours, to, false); err != nil {
		return StashApplyResult{}, errors.Join(err, m.restoreUntracked(parts.untracked))
	}
	result := StashApplyResult{Conflicts: to.conflicted(), Warnings: m.warnings}
	if !merged.Clean() {
		tree, err := merged.Tree.Write(m.store())
		if err != nil {
			return result, err
		}
		return result, errors.Join(writeStateFile(m.r, autoMergeFile, tree.String()+"\n"), m.restoreUntracked(parts.untracked))
	}
	if restored.IsZero() {
		err = m.unstageApplied(ours, to)
	} else {
		err = m.readTreeIntoIndex(restored)
		result.IndexRestored = err == nil
	}
	if err != nil {
		return result, err
	}
	return result, m.restoreUntracked(parts.untracked)
}

func readStashParts(rc *repoContext, position int) (stashParts, error) {
	entries, err := stashEntries(rc.refs)
	if err != nil {
		return stashParts{}, err
	}
	if position < 0 || position >= len(entries) {
		return stashParts{}, fmt.Errorf("%w: stash@{%d}", ErrStashNotFound, position)
	}
	stash, err := dbCommit(rc.db, entries[position].Commit)
	if err != nil {
		return stashParts{}, err
	}
	if len(stash.Parents) < 2 {
		return stashParts{}, fmt.Errorf("%w: %s", ErrNotAStash, entries[position].Selector())
	}
	parts := stashParts{entry: entries[position], stash: stash}
	if parts.base, err = treeOfCommit(rc, stash.Parents[0]); err != nil {
		return stashParts{}, err
	}
	if parts.index, err = treeOfCommit(rc, stash.Parents[1]); err != nil {
		return stashParts{}, err
	}
	if len(stash.Parents) > stashUntrackedParent {
		if parts.untracked, err = treeOfCommit(rc, stash.Parents[stashUntrackedParent]); err != nil {
			return stashParts{}, err
		}
	}
	return parts, nil
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
