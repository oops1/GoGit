package linelog

import "errors"

var (
	ErrNoRanges       = errors.New("linelog: no line ranges to follow")
	ErrMalformedArg   = errors.New("linelog: -L argument is not 'start,end:file' or ':funcname:file'")
	ErrMalformedRange = errors.New("linelog: malformed line range")
	ErrInvalidLine    = errors.New("linelog: invalid line number")
	ErrEmptyRange     = errors.New("linelog: invalid empty range")
	ErrPattern        = errors.New("linelog: invalid pattern")
	ErrNoMatch        = errors.New("linelog: the pattern does not match")
	ErrTooFewLines    = errors.New("linelog: the file has fewer lines than the range")
	ErrPathNotFound   = errors.New("linelog: the path is not a file in the commit")
	ErrNotCommit      = errors.New("linelog: the object is not a commit")
)
