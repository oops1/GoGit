package ops

import (
	"context"
	"errors"
	"time"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/merge"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/progress"
	"github.com/oops1/gogit/internal/gitcore/repo"
)

var ErrPickMergeCommit = errors.New("ops: a merge commit cannot be picked or reverted without choosing a parent")

type PickOptions struct {
	When     time.Time
	Progress progress.Func
}

type PickResult struct {
	Picked    hash.ObjectID
	Commit    hash.ObjectID
	Conflicts []string
}

func (r PickResult) Clean() bool { return len(r.Conflicts) == 0 }

const (
	pickNote          = "cherry-pick: "
	revertNote        = "revert: "
	parentLabelPrefix = "parent of "
)

type pickPlan struct {
	base, theirs hash.ObjectID
	labels       merge.Labels
	message      string
	author       *object.Signature
	reflog       string
	stateFile    string
}

func CherryPick(ctx context.Context, r *repo.Repository, target string, opts PickOptions) (PickResult, error) {
	return pick(ctx, r, target, opts, false)
}

func Revert(ctx context.Context, r *repo.Repository, target string, opts PickOptions) (PickResult, error) {
	return pick(ctx, r, target, opts, true)
}

func pick(ctx context.Context, r *repo.Repository, target string, opts PickOptions, revert bool) (PickResult, error) {
	m, err := openMerger(ctx, r, MergeOptions{When: opts.When, Progress: opts.Progress})
	if err != nil {
		return PickResult{}, err
	}
	defer m.close()
	if err := m.refuseWhileMerging(); err != nil {
		return PickResult{}, err
	}
	if err := m.rc.requireIdentity(); err != nil {
		return PickResult{}, err
	}
	id, _, err := m.resolve(target)
	if err != nil {
		return PickResult{}, err
	}
	plan, err := m.planPick(id, revert)
	if err != nil {
		return PickResult{}, err
	}
	head, err := resolveHeadTarget(m.rc.refs)
	if err != nil {
		return PickResult{}, err
	}
	if head.old.IsZero() {
		return PickResult{}, ErrUnbornHead
	}
	return m.applyPick(head, id, plan)
}

func (m *merger) planPick(id hash.ObjectID, revert bool) (pickPlan, error) {
	c, err := dbCommit(m.rc.db, id)
	if err != nil {
		return pickPlan{}, err
	}
	if len(c.Parents) > 1 {
		return pickPlan{}, ErrPickMergeCommit
	}
	parent := hash.Zero
	if len(c.Parents) == 1 {
		if parent, err = m.treeOf(c.Parents[0]); err != nil {
			return pickPlan{}, err
		}
	}
	subject := firstLine(c.Message)
	label := abbreviate(id) + " (" + subject + ")"
	if revert {
		message := "Revert \"" + subject + "\"\n\nThis reverts commit " + id.String() + ".\n"
		return pickPlan{
			base:      c.Tree,
			theirs:    parent,
			labels:    merge.Labels{Ours: oursLabel, Theirs: parentLabelPrefix + label, Base: label},
			message:   message,
			reflog:    revertNote + firstLine(message),
			stateFile: revertFile,
		}, nil
	}
	author := c.Author
	return pickPlan{
		base:      parent,
		theirs:    c.Tree,
		labels:    merge.Labels{Ours: oursLabel, Theirs: label, Base: parentLabelPrefix + label},
		message:   c.Message,
		author:    &author,
		reflog:    pickNote + subject,
		stateFile: pickHeadFile,
	}, nil
}

type pickedTree struct {
	tree      hash.ObjectID
	empty     bool
	conflicts []string
}

func (m *merger) applyPick(head headTarget, id hash.ObjectID, plan pickPlan) (PickResult, error) {
	result := PickResult{Picked: id}
	picked, err := m.mergePick(head, plan)
	if err != nil {
		return result, err
	}
	result.Conflicts = picked.conflicts
	switch {
	case !result.Clean():
		return result, writeStateFiles(m.r, []stateFile{
			{plan.stateFile, id.String() + "\n"},
			{mergeMsgFile, withConflictList(plan.message, result.Conflicts)},
		})
	case picked.empty:
		return result, ErrNothingToCommit
	}
	result.Commit, err = m.commitPick(head, plan, picked.tree)
	return result, err
}

func (m *merger) mergePick(head headTarget, plan pickPlan) (pickedTree, error) {
	ours, oursTree, err := m.snapshot(head.old)
	if err != nil {
		return pickedTree{}, err
	}
	merged, err := m.mergeTrees(plan.base, oursTree, plan.theirs, plan.labels, 0)
	if err != nil {
		return pickedTree{}, err
	}
	to := outcomeOf(merged)
	if err := m.moveTo(ours, to, true); err != nil {
		return pickedTree{}, err
	}
	tree, err := merged.Tree.Write(m.store())
	if err != nil {
		return pickedTree{}, err
	}
	if err := writeStateFile(m.r, autoMergeFile, tree.String()+"\n"); err != nil {
		return pickedTree{}, err
	}
	return pickedTree{tree: tree, empty: tree == oursTree, conflicts: to.conflicted()}, nil
}

func (m *merger) commitPick(head headTarget, plan pickPlan, tree hash.ObjectID) (hash.ObjectID, error) {
	committer := m.rc.sig
	committer.When = m.opts.When
	author := committer
	if plan.author != nil {
		author = *plan.author
	}
	commit, err := dbPutObject(m.rc.db, &object.Commit{
		Tree:      tree,
		Parents:   []hash.ObjectID{head.old},
		Author:    author,
		Committer: committer,
		Message:   plan.message,
	})
	if err != nil {
		return hash.Zero, err
	}
	return commit, m.advance(head, commit, plan.reflog)
}
