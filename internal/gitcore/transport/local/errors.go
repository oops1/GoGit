package local

import "errors"

var (
	ErrInvalidPath = errors.New("local: invalid repository path")
	ErrReadOnly    = errors.New("local: repository is not writable")
)
