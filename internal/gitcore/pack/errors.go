package pack

import "errors"

var (
	ErrBadMagic                = errors.New("pack: bad file signature")
	ErrUnsupportedVersion      = errors.New("pack: unsupported packfile version")
	ErrUnsupportedIndexVersion = errors.New("pack: unsupported pack index version")
	ErrTruncated               = errors.New("pack: file is truncated")
	ErrChecksumMismatch        = errors.New("pack: checksum does not match the trailer")
	ErrPackMismatch            = errors.New("pack: index does not belong to the packfile")
	ErrCorruptIndex            = errors.New("pack: corrupt pack index")
	ErrOutOfRange              = errors.New("pack: entry number is out of range")
	ErrBadOffset               = errors.New("pack: bad object offset")
	ErrBadObjectHeader         = errors.New("pack: bad object header")
	ErrUnknownObjectKind       = errors.New("pack: unknown object kind")
	ErrObjectTooLarge          = errors.New("pack: declared object size exceeds the packfile capacity")
	ErrSizeMismatch            = errors.New("pack: inflated size differs from the declared size")
	ErrDecompress              = errors.New("pack: cannot decompress object data")
	ErrInvalidDelta            = errors.New("pack: invalid delta stream")
	ErrDeltaSizeMismatch       = errors.New("pack: delta produced an unexpected number of bytes")
	ErrDeltaChainTooDeep       = errors.New("pack: delta chain is too deep")
	ErrBaseNotFound            = errors.New("pack: delta base is not available")
)
