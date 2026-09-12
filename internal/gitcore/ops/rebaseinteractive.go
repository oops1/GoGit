package ops

import (
	"fmt"
	"slices"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
)

var rebaseActions = []string{actionPick, actionReword, actionEdit, actionSquash, actionFixup, actionDrop}

func validateTodo(todo []RebaseStep) error {
	folded := false
	for _, step := range todo {
		switch {
		case !slices.Contains(rebaseActions, step.Action):
			return fmt.Errorf("%w: %s", ErrRebaseStepUnsupported, step.Action)
		case step.Commit.IsZero():
			return fmt.Errorf("%w: %s without a commit", ErrRebaseStepUnsupported, step.Action)
		case folds(step.Action) && !folded:
			return fmt.Errorf("%w: %s without a commit to fold into", ErrRebaseStepUnsupported, step.Action)
		}
		if step.Action != actionDrop {
			folded = true
		}
	}
	return nil
}

func folds(action string) bool { return action == actionSquash || action == actionFixup }

func (m *merger) runRebase(state RebaseState, result RebaseResult) (RebaseResult, error) {
	for len(state.Todo) > 0 {
		step := state.Todo[0]
		stopped, err := m.runRebaseStep(&state, &result, step)
		if stopped || err != nil {
			return result, err
		}
	}
	return m.finishRebase(state, result)
}

func (m *merger) runRebaseStep(state *RebaseState, result *RebaseResult, step RebaseStep) (bool, error) {
	switch step.Action {
	case actionDrop:
		state.Todo, state.Done = state.Todo[1:], append(state.Done, step)
		return false, writeRebaseState(m.r, *state)
	case actionPick, actionReword, actionEdit:
		return m.applyRebaseStep(state, result, step)
	case actionSquash, actionFixup:
		return m.foldRebaseStep(state, result, step)
	}
	return false, fmt.Errorf("%w: %s", ErrRebaseStepUnsupported, step.Action)
}

func (m *merger) applyRebaseStep(state *RebaseState, result *RebaseResult, step RebaseStep) (bool, error) {
	head, err := resolveHeadTarget(m.rc.refs)
	if err != nil {
		return false, err
	}
	plan, err := m.planPick(step.Commit, false)
	if err != nil {
		return false, err
	}
	plan.reflog = m.rebaseNote(actionPick) + firstLine(plan.message)
	picked, err := m.mergePick(head, plan)
	if err != nil {
		return false, err
	}
	state.Todo, state.Done = state.Todo[1:], append(state.Done, step)
	if len(picked.conflicts) > 0 {
		return true, m.stopForConflicts(state, result, step.Commit, plan, picked.conflicts)
	}
	if picked.empty {
		return false, writeRebaseState(m.r, *state)
	}
	commit, err := m.commitPick(head, plan, picked.tree)
	if err != nil {
		return false, err
	}
	state.Rewritten += step.Commit.String() + " " + commit.String() + "\n"
	result.Applied++
	if step.Action == actionPick {
		return false, writeRebaseState(m.r, *state)
	}
	return true, m.stopForAmending(state, result, step.Commit, commit, plan)
}

func (m *merger) foldRebaseStep(state *RebaseState, result *RebaseResult, step RebaseStep) (bool, error) {
	head, err := resolveHeadTarget(m.rc.refs)
	if err != nil {
		return false, err
	}
	previous, err := dbCommit(m.rc.db, head.old)
	if err != nil {
		return false, err
	}
	plan, err := m.planPick(step.Commit, false)
	if err != nil {
		return false, err
	}
	plan.message = foldedMessage(step.Action, previous.Message, plan.message)
	plan.author = &previous.Author
	plan.reflog = m.rebaseNote(step.Action) + firstLine(plan.message)
	picked, err := m.mergePick(head, plan)
	if err != nil {
		return false, err
	}
	state.Todo, state.Done = state.Todo[1:], append(state.Done, step)
	if len(picked.conflicts) > 0 {
		return true, m.stopForConflicts(state, result, step.Commit, plan, picked.conflicts)
	}
	commit, err := m.amendHead(head, previous, picked.tree, plan)
	if err != nil {
		return false, err
	}
	state.Rewritten = rewrittenAfterFold(state.Rewritten, head.old, step.Commit, commit)
	result.Applied++
	return false, writeRebaseState(m.r, *state)
}

func foldedMessage(action, previous, next string) string {
	if action == actionFixup {
		return previous
	}
	return normalizeMessage(strings.TrimRight(previous, "\n") + "\n\n" + next)
}

func rewrittenAfterFold(rewritten string, folded, original, commit hash.ObjectID) string {
	var out strings.Builder
	if rewritten != "" {
		for line := range strings.SplitSeq(strings.TrimSuffix(rewritten, "\n"), "\n") {
			out.WriteString(strings.TrimSuffix(line, " "+folded.String()) + " " + commit.String() + "\n")
		}
	}
	out.WriteString(original.String() + " " + commit.String() + "\n")
	return out.String()
}

func (m *merger) amendHead(head headTarget, previous *object.Commit, tree hash.ObjectID, plan pickPlan) (hash.ObjectID, error) {
	committer := m.rc.sig
	committer.When = m.opts.When
	author := committer
	if plan.author != nil {
		author = *plan.author
	}
	commit, err := dbPutObject(m.rc.db, &object.Commit{
		Tree:      tree,
		Parents:   previous.Parents,
		Author:    author,
		Committer: committer,
		Message:   normalizeMessage(plan.message),
	})
	if err != nil {
		return hash.Zero, err
	}
	return commit, m.advance(head, commit, plan.reflog)
}

func (m *merger) stopForConflicts(state *RebaseState, result *RebaseResult, stopped hash.ObjectID, plan pickPlan, conflicts []string) error {
	state.Stopped, state.Message, state.Author = stopped, plan.message, plan.author
	result.Stopped, result.Conflicts = stopped, conflicts
	return joinErrors(
		writeRebaseState(m.r, *state),
		writeStateFile(m.r, mergeMsgFile, withConflictList(plan.message, conflicts)),
		m.rerere().conflicts(conflicts),
	)
}

func (m *merger) stopForAmending(state *RebaseState, result *RebaseResult, stopped, commit hash.ObjectID, plan pickPlan) error {
	state.Stopped, state.Amend, state.Message, state.Author = stopped, commit, plan.message, plan.author
	result.Stopped, result.Amend, result.Message = stopped, commit, plan.message
	return writeRebaseState(m.r, *state)
}

func (m *merger) continueAmending(state *RebaseState, result *RebaseResult, message string) error {
	head, err := resolveHeadTarget(m.rc.refs)
	if err != nil {
		return err
	}
	amended, err := dbCommit(m.rc.db, head.old)
	if err != nil {
		return err
	}
	tree, err := m.stagedTree()
	if err != nil {
		return err
	}
	if message == "" {
		message = amended.Message
	}
	plan := pickPlan{message: message, author: &amended.Author}
	plan.reflog = m.rebaseNote("continue") + firstLine(plan.message)
	commit, err := m.amendHead(head, amended, tree, plan)
	if err != nil {
		return err
	}
	state.Rewritten = rewrittenAfterFold(state.Rewritten, head.old, state.Stopped, commit)
	state.Stopped, state.Amend, state.Message, state.Author = hash.Zero, hash.Zero, "", nil
	result.Applied++
	return joinErrors(writeRebaseState(m.r, *state), removeStateFiles(m.r, mergeMsgFile))
}

func (m *merger) stagedTree() (hash.ObjectID, error) {
	idx, err := readIndex(m.r)
	if err != nil {
		return hash.Zero, err
	}
	if idx.HasConflicts() {
		return hash.Zero, ErrUnmergedPaths
	}
	return idx.WriteTree(m.store())
}
