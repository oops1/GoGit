package systheme

import (
	"context"
	"time"
)

type Scheme int

const (
	Unknown Scheme = iota
	Dark
	Light
)

func (s Scheme) String() string {
	switch s {
	case Dark:
		return "dark"
	case Light:
		return "light"
	}
	return "unknown"
}

func Detect() Scheme {
	return detect()
}

func Watch(ctx context.Context, interval time.Duration, fn func(Scheme) bool) {
	watch(ctx, interval, detect, fn)
}

func watch(ctx context.Context, interval time.Duration, probe func() Scheme, fn func(Scheme) bool) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	last := probe()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if cur := probe(); cur != last {
				last = cur
				fn(cur)
			}
		}
	}
}
