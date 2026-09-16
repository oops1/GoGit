package credential

import "errors"

var (
	ErrUnsupportedHelper  = errors.New("credential: helper is not supported")
	ErrKeyringLocked      = errors.New("credential: the system keyring is locked")
	ErrNoDefaultKeyring   = errors.New("credential: the system keyring has no default collection")
	ErrInvalidAccount     = errors.New("credential: the account name cannot be used as a file name")
	ErrMissingConfigValue = errors.New("credential: a credential setting has no value")
)
