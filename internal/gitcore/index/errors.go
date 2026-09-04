package index

import "errors"

var (
	ErrBadSignature         = errors.New("index: bad file signature")
	ErrUnsupportedVersion   = errors.New("index: unsupported index version")
	ErrChecksum             = errors.New("index: checksum does not match the trailer")
	ErrUnsorted             = errors.New("index: entries are out of order")
	ErrTruncated            = errors.New("index: file is truncated")
	ErrMalformed            = errors.New("index: malformed index data")
	ErrUnsupportedExtension = errors.New("index: required extension is not understood")
	ErrUnsupported          = errors.New("index: unsupported index feature")
	ErrLocked               = errors.New("index: another process holds the index lock")
	ErrUnmerged             = errors.New("index: index holds unmerged entries")
)
