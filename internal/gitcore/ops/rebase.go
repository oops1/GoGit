package ops

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"time"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/progress"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/gitcore/revision"
	"github.com/oops1/gogit/internal/gitcore/worktree"
)

var (
	ErrNoRebaseInProgress       = errors.New("ops: there is no rebase in progress")
	ErrRebaseStepUnsupported    = errors.New("ops: the rebase step is not supported")
	ErrRebaseUncommittedChanges = errors.New("ops: commit the staged changes before continuing the rebase")
)

func joinErrors(errs ...error) error { return errors.Join(errs...) }

const (
	rebaseAction = "rebase"
	returningTo  = "returning to "
)

func (m *merger) rebaseNote(kind string) string {
	return m.rebaseName() + " (" + kind + "): "
}

func (m *merger) rebaseName() string { return cmp.Or(m.action, rebaseAction) }

type RebaseOptions struct {
	Onto     string
	Todo     []RebaseStep
	Message  string
	When     time.Time
	Progress progress.Func
}

type RebaseResult struct {
	Old       hash.ObjectID
	New       hash.ObjectID
	UpToDate  bool
	Applied   int
	Stopped   hash.ObjectID
	Amend     hash.ObjectID
	Message   string
	Conflicts []string
}

func (r RebaseResult) Amending() bool { return !r.Amend.IsZero() }

func (r RebaseResult) Finished() bool { return r.Stopped.IsZero() }

func Rebase(ctx context.Context, r *repo.Repository, upstream string, opts RebaseOptions) (RebaseResult, error) {
	m, err := openRebaser(ctx, r, opts)
	if err != nil {
		return RebaseResult{}, err
	}
	defer m.close()
	if err := m.refuseWhileMerging(); err != nil {
		return RebaseResult{}, err
	}
	base, _, err := m.resolve(upstream)
	if err != nil {
		return RebaseResult{}, err
	}
	onto, ontoName := base, upstream
	if opts.Onto != "" {
		if onto, _, err = m.resolve(opts.Onto); err != nil {
			return RebaseResult{}, err
		}
		ontoName = opts.Onto
	}
	return m.rebaseOnto(base, onto, ontoName, opts.Todo)
}

func (m *merger) rebaseOnto(base, onto hash.ObjectID, ontoName string, todo []RebaseStep) (RebaseResult, error) {
	head, err := resolveHeadTarget(m.rc.refs)
	if err != nil {
		return RebaseResult{}, err
	}
	switch {
	case head.detached:
		return RebaseResult{}, ErrDetachedHead
	case head.old.IsZero():
		return RebaseResult{}, ErrUnbornHead
	}
	return m.startRebase(head, base, onto, ontoName, todo)
}

func openRebaser(ctx context.Context, r *repo.Repository, opts RebaseOptions) (*merger, error) {
	m, err := openMerger(ctx, r, MergeOptions{When: opts.When, Progress: opts.Progress})
	if err != nil {
		return nil, err
	}
	if err := m.rc.requireIdentity(); err != nil {
		m.close()
		return nil, err
	}
	return m, nil
}

func (m *merger) startRebase(head headTarget, base, onto hash.ObjectID, ontoName string, chosen []RebaseStep) (RebaseResult, error) {
	result := RebaseResult{Old: head.old, New: head.old}
	todo, upToDate, err := m.rebaseSteps(head.old, base, onto, chosen)
	if err != nil || upToDate {
		result.UpToDate = upToDate
		return result, err
	}
	if err := m.requireCleanWorkTree(); err != nil {
		return result, err
	}
	if err := writeStateFile(m.r, origHeadFile, head.old.String()+"\n"); err != nil {
		return result, err
	}
	from, _, err := m.snapshot(head.old)
	if err != nil {
		return result, err
	}
	to, _, err := m.snapshot(onto)
	if err != nil {
		return result, err
	}
	if err := m.moveTo(from, outcome{tree: to}, false); err != nil {
		return result, err
	}
	if err := m.detachAt(onto, m.rebaseNote("start")+"checkout "+ontoName); err != nil {
		return result, err
	}
	state := RebaseState{HeadName: head.ref.String(), Onto: onto, OrigHead: head.old, Todo: todo}
	if err := writeRebaseState(m.r, state); err != nil {
		return result, err
	}
	return m.runRebase(state, result)
}

func (m *merger) rebaseSteps(head, base, onto hash.ObjectID, chosen []RebaseStep) ([]RebaseStep, bool, error) {
	planned := len(chosen) == 0
	if !planned {
		todo, err := m.rebaseTodo(base, head)
		if err != nil {
			return nil, false, err
		}
		planned = slices.EqualFunc(todo, chosen, sameStep)
	}
	if planned {
		upToDate, err := m.rebaseIsUpToDate(head, base, onto)
		if err != nil || upToDate {
			return nil, upToDate, err
		}
	}
	if len(chosen) > 0 {
		return chosen, false, validateTodo(chosen)
	}
	todo, err := m.rebaseTodo(base, head)
	return todo, false, err
}

func sameStep(a, b RebaseStep) bool { return a.Action == b.Action && a.Commit == b.Commit }

func (m *merger) rebaseIsUpToDate(head, base, onto hash.ObjectID) (bool, error) {
	ctx := revision.Context{Objects: m.store()}
	ahead, err := revision.IsAncestor(ctx, onto, head)
	if err != nil || !ahead {
		return false, err
	}
	bases, err := revision.MergeBase(ctx, base, head)
	return err == nil && len(bases) == 1 && bases[0] == onto, err
}

func (m *merger) requireCleanWorkTree() error {
	tree, err := worktreeOpen(m.r, worktree.Options{DB: m.rc.db, Refs: m.rc.refs})
	if err != nil {
		return err
	}
	defer func() { _ = tree.Close() }()
	status, err := tree.Status(m.ctx)
	if err != nil {
		return err
	}
	var dirty []string
	for _, e := range status.Entries {
		if e.Unstaged == worktree.StatusUntracked || e.Unstaged == worktree.StatusIgnored {
			continue
		}
		if e.Staged != worktree.StatusUnmodified || e.Unstaged != worktree.StatusUnmodified {
			dirty = append(dirty, e.Path)
		}
	}
	if len(dirty) > 0 {
		return &OverwriteError{Paths: dirty}
	}
	return nil
}

func (m *merger) rebaseTodo(base, head hash.ObjectID) ([]RebaseStep, error) {
	upstream, err := m.upstreamPatchIDs(base, head)
	if err != nil {
		return nil, err
	}
	var todo []RebaseStep
	walk := revision.Walk(m.ctx, revision.Options{
		Context: revision.Context{Objects: m.store()},
		Include: []hash.ObjectID{head},
		Exclude: []hash.ObjectID{base},
		Order:   revision.Topo,
		Reverse: true,
	})
	for commit, err := range walk {
		if err != nil {
			return nil, err
		}
		if len(commit.Parents) > 1 {
			continue
		}
		applied, err := m.appliedUpstream(commit, upstream)
		if err != nil {
			return nil, err
		}
		if !applied {
			todo = append(todo, RebaseStep{Action: actionPick, Commit: commit.ID, Subject: firstLine(commit.Message)})
		}
	}
	return todo, nil
}

func (m *merger) detachAt(commit hash.ObjectID, note string) error {
	tx := m.rc.refs.Begin()
	tx.SetMessage(note)
	if err := txDetach(tx, refs.HEAD, commit); err != nil {
		tx.Rollback()
		return err
	}
	return txCommit(tx)
}

func (m *merger) finishRebase(state RebaseState, result RebaseResult) (RebaseResult, error) {
	head, err := resolveHeadTarget(m.rc.refs)
	if err != nil {
		return result, err
	}
	branch := refs.Name(state.HeadName)
	tx := m.rc.refs.Begin()
	tx.SetMessage(m.rebaseNote("finish") + state.HeadName + " onto " + state.Onto.String())
	if err := txUpdate(tx, branch, head.old, state.OrigHead); err != nil {
		tx.Rollback()
		return result, err
	}
	if err := txCommit(tx); err != nil {
		return result, err
	}
	if err := m.attachTo(branch, m.rebaseNote("finish")+returningTo+state.HeadName); err != nil {
		return result, err
	}
	result.New = head.old
	return result, errors.Join(clearRebaseState(m.r), removeStateFiles(m.r, mergeMsgFile))
}

func (m *merger) attachTo(branch refs.Name, note string) error {
	tx := m.rc.refs.Begin()
	tx.SetMessage(note)
	if err := txSetSymbolic(tx, refs.HEAD, branch); err != nil {
		tx.Rollback()
		return err
	}
	return txCommit(tx)
}

func ContinueRebase(ctx context.Context, r *repo.Repository, opts RebaseOptions) (RebaseResult, error) {
	m, state, err := openRebaseInProgress(ctx, r, opts)
	if err != nil {
		return RebaseResult{}, err
	}
	defer m.close()
	result := RebaseResult{Old: state.OrigHead}
	if state.Amending() {
		if err := m.continueAmending(&state, &result, opts.Message); err != nil {
			return result, err
		}
		return m.runRebase(state, result)
	}
	if !state.Stopped.IsZero() {
		commit, err := m.commitResolution(state)
		if err != nil {
			return result, err
		}
		if !commit.IsZero() {
			state.Rewritten += state.Stopped.String() + " " + commit.String() + "\n"
			result.Applied++
		}
		state.Stopped, state.Message, state.Author = hash.Zero, "", nil
		if err := errors.Join(writeRebaseState(m.r, state), removeStateFiles(m.r, mergeMsgFile)); err != nil {
			return result, err
		}
	}
	return m.runRebase(state, result)
}

func (m *merger) commitResolution(state RebaseState) (hash.ObjectID, error) {
	tree, err := m.stagedTree()
	if err != nil {
		return hash.Zero, err
	}
	head, err := resolveHeadTarget(m.rc.refs)
	if err != nil {
		return hash.Zero, err
	}
	headTree, err := m.treeOf(head.old)
	if err != nil || headTree == tree {
		return hash.Zero, err
	}
	commit, err := m.commitPick(head, pickPlan{
		message: state.Message,
		author:  state.Author,
		reflog:  m.rebaseNote("continue") + firstLine(state.Message),
	}, tree)
	if err != nil {
		return hash.Zero, err
	}
	return commit, m.rerere().record()
}

func SkipRebase(ctx context.Context, r *repo.Repository, opts RebaseOptions) (RebaseResult, error) {
	m, state, err := openRebaseInProgress(ctx, r, opts)
	if err != nil {
		return RebaseResult{}, err
	}
	defer m.close()
	result := RebaseResult{Old: state.OrigHead}
	if err := errors.Join(m.rerere().clear(), m.resetToHead()); err != nil {
		return result, err
	}
	state.Stopped, state.Amend, state.Message, state.Author = hash.Zero, hash.Zero, "", nil
	if err := errors.Join(writeRebaseState(m.r, state), clearMergeState(m.r)); err != nil {
		return result, err
	}
	return m.runRebase(state, result)
}

func (m *merger) resetToHead() error {
	head, err := resolveHeadCommit(m.rc.refs)
	if err != nil {
		return err
	}
	tree, _, err := m.snapshot(head)
	if err != nil {
		return err
	}
	return m.resetTo(tree)
}

func openRebaseInProgress(ctx context.Context, r *repo.Repository, opts RebaseOptions) (*merger, RebaseState, error) {
	state, err := ReadRebaseState(r)
	if err != nil {
		return nil, RebaseState{}, err
	}
	if !state.InProgress() {
		return nil, RebaseState{}, ErrNoRebaseInProgress
	}
	m, err := openRebaser(ctx, r, opts)
	return m, state, err
}

func (m *merger) abortRebase(state RebaseState) error {
	tree, _, err := m.snapshot(state.OrigHead)
	if err != nil {
		return err
	}
	if err := m.resetTo(tree); err != nil {
		return err
	}
	if err := m.attachTo(refs.Name(state.HeadName), m.rebaseNote("abort")+returningTo+state.HeadName); err != nil {
		return err
	}
	return errors.Join(m.rerere().clear(), clearRebaseState(m.r), clearMergeState(m.r))
}
