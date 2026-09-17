package submodule

import "errors"

var (
	ErrInvalidGitmodules  = errors.New("submodule: invalid .gitmodules")
	ErrInvalidUpdate      = errors.New("submodule: invalid update mode")
	ErrInvalidIgnore      = errors.New("submodule: invalid ignore mode")
	ErrInvalidActive      = errors.New("submodule: invalid active setting")
	ErrEmptyRemoteURL     = errors.New("submodule: the remote url is empty")
	ErrCannotStripURL     = errors.New("submodule: cannot strip one component off url")
	ErrReadGitmodules     = errors.New("submodule: read .gitmodules")
	ErrSymlinkInPath      = errors.New("submodule: the submodule path goes through a symbolic link")
	ErrGitDirInsideGitDir = errors.New("submodule: the submodule git directory is inside another submodule git directory")
)
