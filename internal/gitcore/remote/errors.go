package remote

import "errors"

var (
	ErrNoRemote       = errors.New("remote: no such remote")
	ErrNoURL          = errors.New("remote: remote has no url")
	ErrNonFastForward = errors.New("remote: update is not a fast-forward")
	ErrRejected       = errors.New("remote: server rejected the update")
)
