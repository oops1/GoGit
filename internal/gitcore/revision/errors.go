package revision

import "errors"

var (
	ErrSyntax      = errors.New("revision: malformed revision specification")
	ErrNotFound    = errors.New("revision: revision not found")
	ErrAmbiguous   = errors.New("revision: abbreviated object id is ambiguous")
	ErrNotCommit   = errors.New("revision: object is not a commit")
	ErrUnsupported = errors.New("revision: unsupported revision specification")
)
