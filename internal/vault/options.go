package vault

import "time"

type Options struct {
	Path            string
	IdleTime        time.Duration
	KnownGeneration uint64
	OnGeneration    func(uint64)
}
