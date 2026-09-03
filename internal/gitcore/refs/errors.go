package refs

import "errors"

var (
	ErrInvalidName         = errors.New("refs: invalid reference name")
	ErrInvalidTarget       = errors.New("refs: reference target is the zero object id")
	ErrNotFound            = errors.New("refs: reference not found")
	ErrMalformedRef        = errors.New("refs: malformed reference file")
	ErrMalformedPacked     = errors.New("refs: malformed packed-refs file")
	ErrMalformedReflog     = errors.New("refs: malformed reflog entry")
	ErrTooManySymlinks     = errors.New("refs: too many levels of symbolic reference")
	ErrLocked              = errors.New("refs: reference is already locked")
	ErrNameConflict        = errors.New("refs: reference name conflicts with an existing reference")
	ErrOldValueMismatch    = errors.New("refs: reference does not have the expected old value")
	ErrDuplicateUpdate     = errors.New("refs: reference is updated twice in one transaction")
	ErrCommitted           = errors.New("refs: transaction is already finished")
	ErrSymbolicOutsideRefs = errors.New("refs: symbolic reference must point inside refs/")
	ErrNoPeeler            = errors.New("refs: object peeler is not configured")
	ErrNotTag              = errors.New("refs: reference does not point to a tag object")
	ErrMissingCommitter    = errors.New("refs: reflog committer is not configured")
	ErrReadFailed          = errors.New("refs: cannot read file")
	ErrWriteFailed         = errors.New("refs: cannot write file")
	ErrDirFailed           = errors.New("refs: cannot read directory")
)
