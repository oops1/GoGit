package vault

import "errors"

var (
	ErrLocked             = errors.New("vault: locked")
	ErrClosed             = errors.New("vault: closed")
	ErrWrongKey           = errors.New("vault: wrong key")
	ErrCorrupted          = errors.New("vault: corrupted")
	ErrInvalidFormat      = errors.New("vault: invalid format")
	ErrUnsupportedVersion = errors.New("vault: unsupported version")
	ErrSlotUnavailable    = errors.New("vault: slot unavailable")
	ErrSlotKindMismatch   = errors.New("vault: slot kind mismatch")
	ErrSlotNotFound       = errors.New("vault: slot not found")
	ErrSlotIndex          = errors.New("vault: slot index out of range")
	ErrSlotCount          = errors.New("vault: invalid slot count")
	ErrTooManySlots       = errors.New("vault: too many slots")
	ErrLastSlot           = errors.New("vault: cannot remove last slot")
	ErrNotFound           = errors.New("vault: not found")
	ErrAlreadyExists      = errors.New("vault: already exists")
	ErrInvalidPath        = errors.New("vault: invalid path")
	ErrInvalidKeyFile     = errors.New("vault: invalid key file")
	ErrNilUnlocker        = errors.New("vault: nil unlocker")
)
