package ops

import (
	"errors"
	"fmt"
	"strings"
)

var (
	ErrNothingToCommit       = errors.New("ops: nothing to commit")
	ErrEmptyMessage          = errors.New("ops: commit message is empty")
	ErrMissingIdentity       = errors.New("ops: user.name and user.email are not configured")
	ErrBranchExists          = errors.New("ops: branch already exists")
	ErrBranchNotFound        = errors.New("ops: branch not found")
	ErrInvalidBranchName     = errors.New("ops: invalid branch name")
	ErrBranchCheckedOut      = errors.New("ops: branch is checked out")
	ErrBranchNotMerged       = errors.New("ops: branch is not fully merged")
	ErrWouldOverwrite        = errors.New("ops: uncommitted changes would be overwritten")
	ErrTargetNotFound        = errors.New("ops: switch target not found")
	ErrIndexLocked           = errors.New("ops: index is locked by another operation")
	ErrBareRepository        = errors.New("ops: repository has no working tree")
	ErrUnbornHead            = errors.New("ops: HEAD does not point to a commit yet")
	ErrDetachedHead          = errors.New("ops: HEAD does not point to a branch")
	ErrInvalidPath           = errors.New("ops: invalid repository path")
	ErrNoUpstream            = errors.New("ops: branch has no upstream")
	ErrNotFastForward        = errors.New("ops: pull would not be a fast-forward")
	ErrDirtyWorkTree         = errors.New("ops: working tree has local changes")
	ErrRemoteExists          = errors.New("ops: remote already exists")
	ErrInvalidRemoteName     = errors.New("ops: invalid remote name")
	ErrNoLocalConfig         = errors.New("ops: repository has no local config")
	ErrMergeInProgress       = errors.New("ops: a merge, cherry-pick or revert is in progress")
	ErrNoMergeInProgress     = errors.New("ops: there is no merge, cherry-pick or revert to abort")
	ErrUnmergedPaths         = errors.New("ops: the index has unmerged paths")
	ErrCannotFastForward     = errors.New("ops: the merge cannot be a fast-forward")
	ErrUnrelatedHistories    = errors.New("ops: refusing to merge unrelated histories")
	ErrPullRebaseUnsupported = errors.New("ops: pull with rebase cannot bring in diverged history yet")
)

type OverwriteError struct {
	Paths []string
}

func (e *OverwriteError) Error() string {
	return fmt.Sprintf("%s: %s", ErrWouldOverwrite, strings.Join(e.Paths, ", "))
}

func (e *OverwriteError) Unwrap() error {
	return ErrWouldOverwrite
}
