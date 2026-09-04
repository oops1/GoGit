package worktree

import "errors"

var (
	ErrNoObjectDatabase = errors.New("worktree: object database is required")
	ErrBareRepository   = errors.New("worktree: repository has no working tree")
	ErrTooManyFiles     = errors.New("worktree: working tree exceeds the configured file limit")
	ErrReadWorkingTree  = errors.New("worktree: read working tree")
	ErrReadHead         = errors.New("worktree: read HEAD commit")
	ErrReadHeadTree     = errors.New("worktree: read HEAD tree")
)
