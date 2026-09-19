package ops

import (
	"errors"
	"fmt"
	"strings"
)

var (
	ErrNothingToCommit    = errors.New("ops: nothing to commit")
	ErrEmptyMessage       = errors.New("ops: commit message is empty")
	ErrMissingIdentity    = errors.New("ops: user.name and user.email are not configured")
	ErrBranchExists       = errors.New("ops: branch already exists")
	ErrBranchNotFound     = errors.New("ops: branch not found")
	ErrInvalidBranchName  = errors.New("ops: invalid branch name")
	ErrBranchCheckedOut   = errors.New("ops: branch is checked out")
	ErrBranchNotMerged    = errors.New("ops: branch is not fully merged")
	ErrWouldOverwrite     = errors.New("ops: uncommitted changes would be overwritten")
	ErrTargetNotFound     = errors.New("ops: switch target not found")
	ErrIndexLocked        = errors.New("ops: index is locked by another operation")
	ErrBareRepository     = errors.New("ops: repository has no working tree")
	ErrUnbornHead         = errors.New("ops: HEAD does not point to a commit yet")
	ErrDetachedHead       = errors.New("ops: HEAD does not point to a branch")
	ErrInvalidPath        = errors.New("ops: invalid repository path")
	ErrNoUpstream         = errors.New("ops: branch has no upstream")
	ErrNotFastForward     = errors.New("ops: pull would not be a fast-forward")
	ErrDirtyWorkTree      = errors.New("ops: working tree has local changes")
	ErrRemoteExists       = errors.New("ops: remote already exists")
	ErrInvalidRemoteName  = errors.New("ops: invalid remote name")
	ErrNoLocalConfig      = errors.New("ops: repository has no local config")
	ErrMergeInProgress    = errors.New("ops: a merge, cherry-pick, revert or rebase is in progress")
	ErrNoMergeInProgress  = errors.New("ops: there is no merge, cherry-pick, revert or rebase to abort")
	ErrUnmergedPaths      = errors.New("ops: the index has unmerged paths")
	ErrCannotFastForward  = errors.New("ops: the merge cannot be a fast-forward")
	ErrUnrelatedHistories = errors.New("ops: refusing to merge unrelated histories")
	ErrResetPathsWithMode = errors.New("ops: a soft or hard reset cannot take paths")
	ErrInvalidTagName     = errors.New("ops: invalid tag name")
	ErrTagExists          = errors.New("ops: tag already exists")
	ErrTagNotFound        = errors.New("ops: tag not found")
	ErrNothingToStash     = errors.New("ops: there are no local changes to stash")
	ErrStashNotFound      = errors.New("ops: stash entry not found")
	ErrNotAStash          = errors.New("ops: commit is not a stash")
	ErrNoCommitCheckedOut = errors.New("ops: the nested repository has no commit checked out")
	ErrNotBisecting       = errors.New("ops: no bisect is in progress")
	ErrBranchBisected     = errors.New("ops: branch is being bisected")

	ErrBisectGoodWithoutBad  = errors.New("ops: a bisect that names good revisions must name the bad one too")
	ErrBisectMergeBaseBad    = errors.New("ops: the merge base is bad, so the change was undone between it and the good revisions")
	ErrBisectGoodNotAncestor = errors.New("ops: some good revisions are not ancestors of the bad revision")

	ErrStashStagedUntracked = errors.New("ops: a stash of staged changes cannot include untracked files")
	ErrNoStagedChanges      = errors.New("ops: there are no staged changes to stash")
	ErrStashWorktreeKept    = errors.New("ops: the stash was saved but its changes could not be removed from the working tree")
	ErrStashIndexConflicts  = errors.New("ops: the stashed index does not apply to the current index")
	ErrUntrackedNotRestored = errors.New("ops: untracked files could not be restored from the stash")
	ErrPathspecNoMatch      = errors.New("ops: pathspec did not match any file known to git")
)

type PathspecError struct {
	Specs []string
}

func (e *PathspecError) Error() string {
	return fmt.Sprintf("%s: %s", ErrPathspecNoMatch, strings.Join(e.Specs, ", "))
}

func (e *PathspecError) Unwrap() error {
	return ErrPathspecNoMatch
}

type UntrackedRestoreError struct {
	Existing []string
	Blocked  string
}

func (e *UntrackedRestoreError) Error() string {
	if e.Blocked != "" {
		return fmt.Sprintf("%s: cannot create directory at %s", ErrUntrackedNotRestored, e.Blocked)
	}
	return fmt.Sprintf("%s: already exist: %s", ErrUntrackedNotRestored, strings.Join(e.Existing, ", "))
}

func (e *UntrackedRestoreError) Unwrap() error {
	return ErrUntrackedNotRestored
}

type OverwriteError struct {
	Paths []string
}

func (e *OverwriteError) Error() string {
	return fmt.Sprintf("%s: %s", ErrWouldOverwrite, strings.Join(e.Paths, ", "))
}

func (e *OverwriteError) Unwrap() error {
	return ErrWouldOverwrite
}
