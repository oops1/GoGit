package vault

import "time"

type Options struct {
	Path     string
	IdleTime time.Duration
}
