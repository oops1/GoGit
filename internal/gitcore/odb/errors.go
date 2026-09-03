package odb

import "errors"

var (
	ErrNotFound        = errors.New("odb: object not found")
	ErrWrongType       = errors.New("odb: object has an unexpected type")
	ErrCorrupt         = errors.New("odb: content does not match the object name")
	ErrAlternatesLoop  = errors.New("odb: alternate object directories form a loop")
	ErrTagChainTooDeep = errors.New("odb: tag chain is too deep")
	ErrWriterClosed    = errors.New("odb: object writer is already closed")
	ErrInvalidPrefix   = errors.New("odb: invalid short object id prefix")
)
