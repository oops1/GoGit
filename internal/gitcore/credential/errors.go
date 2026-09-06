package credential

import "errors"

var (
	ErrUnsupportedHelper = errors.New("credential: helper is not supported")
	ErrHelperFailed      = errors.New("credential: helper failed")
	ErrMalformedAnswer   = errors.New("credential: malformed helper answer")
)
