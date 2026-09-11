package ops

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/merge"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/progress"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/gitcore/revision"
)

type MergeMode int

const (
	MergeFastForward MergeMode = iota
	MergeFastForwardOnly
	MergeNoFastForward
	MergeSquash
)

type MergeOptions struct {
	Mode     MergeMode
	NoCommit bool
	Message  string
	When     time.Time
	Progress progress.Func
}

type MergeResult struct {
	Target      hash.ObjectID
	Old         hash.ObjectID
	New         hash.ObjectID
	UpToDate    bool
	FastForward bool
	Committed   bool
	Conflicts   []string
}

func (r MergeResult) Clean() bool { return len(r.Conflicts) == 0 }

const (
	oursLabel         = "HEAD"
	virtualLabelOurs  = "Temporary merge branch 1"
	virtualLabelTheir = "Temporary merge branch 2"
	virtualMarkerSize = merge.DefaultMarkerSize + 2
	mergeStrategyNote = ": Merge made by the 'ort' strategy."
	fastForwardNote   = ": Fast-forward"
	resetToHeadNote   = "reset: moving to HEAD"
	resetNotePrefix   = "reset: moving to "
	initialPullNote   = "initial pull"
)

type merger struct {
	ctx  context.Context
	r    *repo.Repository
	wt   *workingTree
	rc   *repoContext
	opts MergeOptions
}

func openMerger(ctx context.Context, r *repo.Repository, opts MergeOptions) (*merger, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	wt, err := openWorkingTree(r)
	if err != nil {
		return nil, err
	}
	rc, err := openRepoContext(r)
	if err != nil {
		_ = wt.close()
		return nil, err
	}
	if opts.When.IsZero() {
		opts.When = time.Now()
	}
	return &merger{ctx: ctx, r: r, wt: wt, rc: rc, opts: opts}, nil
}

func (m *merger) close() {
	_ = m.rc.close()
	_ = m.wt.close()
}

func Merge(ctx context.Context, r *repo.Repository, target string, opts MergeOptions) (MergeResult, error) {
	m, err := openMerger(ctx, r, opts)
	if err != nil {
		return MergeResult{}, err
	}
	defer m.close()
	return m.merge(target)
}

type incoming struct {
	commit  hash.ObjectID
	label   string
	reflog  string
	message func(head headTarget) string
}

func (m *merger) merge(target string) (MergeResult, error) {
	if err := m.refuseWhileMerging(); err != nil {
		return MergeResult{}, err
	}
	theirs, ref, err := m.resolve(target)
	if err != nil {
		return MergeResult{}, err
	}
	return m.integrate(incoming{
		commit: theirs,
		label:  target,
		reflog: "merge " + target,
		message: func(head headTarget) string {
			return defaultMergeMessage(m.r, m.rc.refs, target, ref, head)
		},
	})
}

func (m *merger) refuseWhileMerging() error {
	state, err := ReadMergeState(m.r)
	if err != nil {
		return err
	}
	if state.InProgress() {
		return ErrMergeInProgress
	}
	return nil
}

func (m *merger) integrate(in incoming) (MergeResult, error) {
	head, err := resolveHeadTarget(m.rc.refs)
	if err != nil {
		return MergeResult{}, err
	}
	result := MergeResult{Target: in.commit, Old: head.old, New: head.old}
	if head.old.IsZero() {
		return m.fastForward(head, in.commit, initialPullNote, result)
	}
	if err := writeStateFile(m.r, origHeadFile, head.old.String()+"\n"); err != nil {
		return result, err
	}
	bases, err := revision.MergeBase(revision.Context{Objects: m.store()}, head.old, in.commit)
	if err != nil {
		return result, err
	}
	switch {
	case slices.Contains(bases, in.commit):
		result.UpToDate = true
		return result, nil
	case slices.Contains(bases, head.old) && m.opts.Mode != MergeNoFastForward && m.opts.Mode != MergeSquash:
		return m.fastForward(head, in.commit, in.reflog+fastForwardNote, result)
	case m.opts.Mode == MergeFastForwardOnly:
		return result, ErrCannotFastForward
	case len(bases) == 0:
		return result, ErrUnrelatedHistories
	}
	return m.threeWay(head, bases, in, result)
}

func (m *merger) resolve(target string) (hash.ObjectID, refs.Name, error) {
	rev, err := revision.Parse(target, revision.Context{Objects: m.rc.db, Refs: m.rc.refs, Head: refs.HEAD})
	if err != nil {
		return hash.Zero, "", fmt.Errorf("%w: %s: %w", ErrTargetNotFound, target, err)
	}
	kind, commit, err := dbPeel(m.rc.db, rev.ID)
	if err != nil {
		return hash.Zero, "", err
	}
	if kind != object.TypeCommit {
		return hash.Zero, "", fmt.Errorf("%w: %s is a %s, not a commit", ErrTargetNotFound, target, kind)
	}
	return commit, rev.Ref, nil
}

func (m *merger) snapshot(commit hash.ObjectID) (merge.Snapshot, hash.ObjectID, error) {
	if commit.IsZero() {
		return merge.Snapshot{}, hash.Zero, nil
	}
	tree, err := m.treeOf(commit)
	if err != nil {
		return nil, hash.Zero, err
	}
	s, err := merge.Read(m.store(), tree)
	return s, tree, err
}

func (m *merger) fastForward(head headTarget, theirs hash.ObjectID, note string, result MergeResult) (MergeResult, error) {
	from, _, err := m.snapshot(head.old)
	if err != nil {
		return result, err
	}
	to, _, err := m.snapshot(theirs)
	if err != nil {
		return result, err
	}
	if err := m.moveTo(from, outcome{tree: to}, false); err != nil {
		return result, err
	}
	if err := m.advance(head, theirs, note); err != nil {
		return result, err
	}
	result.New, result.FastForward = theirs, true
	return result, nil
}

func (m *merger) advance(head headTarget, commit hash.ObjectID, message string) error {
	tx := m.rc.refs.Begin()
	tx.SetMessage(message)
	if err := txUpdate(tx, head.ref, commit, head.old); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (m *merger) commits() bool {
	return m.opts.Mode != MergeSquash && !m.opts.NoCommit
}

func (m *merger) threeWay(head headTarget, bases []hash.ObjectID, in incoming, result MergeResult) (MergeResult, error) {
	theirs := in.commit
	if m.commits() {
		if err := m.rc.requireIdentity(); err != nil {
			return result, err
		}
	}
	ours, oursTree, err := m.snapshot(head.old)
	if err != nil {
		return result, err
	}
	theirsTree, err := m.treeOf(theirs)
	if err != nil {
		return result, err
	}
	base, err := m.baseTree(bases, 0)
	if err != nil {
		return result, err
	}
	merged, err := m.mergeTrees(base, oursTree, theirsTree, merge.Labels{Ours: oursLabel, Theirs: in.label}, 0)
	if err != nil {
		return result, err
	}
	to := outcomeOf(merged)
	if err := m.moveTo(ours, to, true); err != nil {
		return result, err
	}
	tree, err := merged.Tree.Write(m.store())
	if err != nil {
		return result, err
	}
	result.Conflicts = to.conflicted()
	message := m.opts.Message
	if message == "" {
		message = in.message(head)
	}
	switch {
	case m.opts.Mode == MergeSquash:
		return result, m.stopAfterSquash(head.old, theirs, tree, result.Conflicts)
	case !result.Clean() || m.opts.NoCommit:
		return result, m.stopBeforeCommit(theirs, tree, message, result.Conflicts)
	}
	commit, err := m.writeMergeCommit(tree, head.old, theirs, message)
	if err != nil {
		return result, err
	}
	if err := m.advance(head, commit, in.reflog+mergeStrategyNote); err != nil {
		return result, err
	}
	result.New, result.Committed = commit, true
	return result, nil
}

type stateFile struct{ name, content string }

func (m *merger) stopAfterSquash(ours, theirs, tree hash.ObjectID, conflicts []string) error {
	squash, err := m.squashMessage(ours, theirs)
	if err != nil {
		return err
	}
	files := []stateFile{{squashMsgFile, squash}, {autoMergeFile, tree.String() + "\n"}}
	if len(conflicts) > 0 {
		files = append(files, stateFile{mergeMsgFile, withConflictList("", conflicts)})
	}
	return writeStateFiles(m.r, files)
}

func (m *merger) stopBeforeCommit(theirs, tree hash.ObjectID, message string, conflicts []string) error {
	mode := ""
	if m.opts.Mode == MergeNoFastForward {
		mode = noFastForward
	}
	return writeStateFiles(m.r, []stateFile{
		{mergeHeadFile, theirs.String() + "\n"},
		{mergeModeFile, mode},
		{mergeMsgFile, withConflictList(message, conflicts)},
		{autoMergeFile, tree.String() + "\n"},
	})
}

func writeStateFiles(r *repo.Repository, files []stateFile) error {
	for _, f := range files {
		if err := writeStateFile(r, f.name, f.content); err != nil {
			return err
		}
	}
	return nil
}

func (m *merger) writeMergeCommit(tree, ours, theirs hash.ObjectID, message string) (hash.ObjectID, error) {
	sig := m.rc.sig
	sig.When = m.opts.When
	return dbPutObject(m.rc.db, &object.Commit{
		Tree:      tree,
		Parents:   []hash.ObjectID{ours, theirs},
		Author:    sig,
		Committer: sig,
		Message:   normalizeMessage(message),
	})
}

func (m *merger) treeOf(commit hash.ObjectID) (hash.ObjectID, error) {
	c, err := dbCommit(m.rc.db, commit)
	if err != nil {
		return hash.Zero, err
	}
	return c.Tree, nil
}

func (m *merger) mergeTrees(base, ours, theirs hash.ObjectID, labels merge.Labels, depth int) (merge.TreeResult, error) {
	snapshots := make([]merge.Snapshot, 3)
	for i, tree := range []hash.ObjectID{base, ours, theirs} {
		s, err := merge.Read(m.store(), tree)
		if err != nil {
			return merge.TreeResult{}, err
		}
		snapshots[i] = s
	}
	opts := merge.TreeOptions{File: merge.Options{Labels: labels}}
	if depth > 0 {
		opts.File.MarkerSize = virtualMarkerSize
	}
	var err error
	if opts.OurRenames, err = merge.DetectRenames(m.ctx, m.store(), base, ours); err != nil {
		return merge.TreeResult{}, err
	}
	if opts.TheirRenames, err = merge.DetectRenames(m.ctx, m.store(), base, theirs); err != nil {
		return merge.TreeResult{}, err
	}
	return merge.Trees(snapshots[0], snapshots[1], snapshots[2], m.store(), opts)
}

func (m *merger) baseTree(bases []hash.ObjectID, depth int) (hash.ObjectID, error) {
	if len(bases) == 0 {
		return hash.Zero, nil
	}
	tree, err := m.treeOf(bases[0])
	if err != nil {
		return hash.Zero, err
	}
	for i, next := range bases[1:] {
		inner, err := m.commonBases(bases[:i+1], next)
		if err != nil {
			return hash.Zero, err
		}
		innerTree, err := m.baseTree(inner, depth+1)
		if err != nil {
			return hash.Zero, err
		}
		nextTree, err := m.treeOf(next)
		if err != nil {
			return hash.Zero, err
		}
		result, err := m.mergeTrees(innerTree, tree, nextTree, merge.Labels{Ours: virtualLabelOurs, Theirs: virtualLabelTheir}, depth+1)
		if err != nil {
			return hash.Zero, err
		}
		if tree, err = result.Tree.Write(m.store()); err != nil {
			return hash.Zero, err
		}
	}
	return tree, nil
}

func (m *merger) commonBases(merged []hash.ObjectID, next hash.ObjectID) ([]hash.ObjectID, error) {
	var out []hash.ObjectID
	for _, commit := range merged {
		found, err := revision.MergeBase(revision.Context{Objects: m.store()}, commit, next)
		if err != nil {
			return nil, err
		}
		for _, id := range found {
			if !slices.Contains(out, id) {
				out = append(out, id)
			}
		}
	}
	return out, nil
}

func AbortOperation(ctx context.Context, r *repo.Repository) error {
	state, err := ReadMergeState(r)
	if err != nil {
		return err
	}
	if !state.InProgress() {
		return ErrNoMergeInProgress
	}
	m, err := openMerger(ctx, r, MergeOptions{})
	if err != nil {
		return err
	}
	defer m.close()
	head, err := resolveHeadTarget(m.rc.refs)
	if err != nil {
		return err
	}
	tree, _, err := m.snapshot(head.old)
	if err != nil {
		return err
	}
	if err := errors.Join(m.resetTo(tree), clearMergeState(r)); err != nil {
		return err
	}
	if err := writeStateFile(r, origHeadFile, head.old.String()+"\n"); err != nil {
		return err
	}
	note := resetToHeadNote
	if state.Operation() != OperationMerge {
		note = resetNotePrefix + head.old.String()
	}
	return m.advance(head, head.old, note)
}

type mergeStore struct{ db *odb.DB }

func (m *merger) store() mergeStore { return mergeStore{db: m.rc.db} }

func (s mergeStore) Get(id hash.ObjectID) (object.Type, []byte, error) { return dbGet(s.db, id) }

func (s mergeStore) Put(kind object.Type, data []byte) (hash.ObjectID, error) {
	return dbPut(s.db, kind, data)
}

func (s mergeStore) Tree(id hash.ObjectID) (*object.Tree, error) { return dbTree(s.db, id) }
