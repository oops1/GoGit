package repo

import "errors"

var (
	ErrNotFound                 = errors.New("repo: not a git repository")
	ErrCeilingReached           = errors.New("repo: discovery stopped at a ceiling directory")
	ErrInvalidGitDirFile        = errors.New("repo: invalid gitdir file")
	ErrUnsupportedFormatVersion = errors.New("repo: unsupported repository format version")
	ErrNotBareNoWorkTree        = errors.New("repo: repository is not bare and has no working tree")
	ErrInvalidPath              = errors.New("repo: invalid path")
	ErrInvalidShallowFile       = errors.New("repo: invalid shallow file")
)
