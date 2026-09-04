package diff

import "errors"

var (
	ErrApply       = errors.New("diff: hunks do not apply")
	ErrNotATree    = errors.New("diff: object is not a tree")
	ErrMissingBlob = errors.New("diff: blob is missing")
)
