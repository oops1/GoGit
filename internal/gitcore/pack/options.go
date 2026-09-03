package pack

import (
	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
)

const (
	DefaultMaxDeltaDepth = 4096
	DefaultCacheBytes    = 16 << 20
)

type BaseResolver interface {
	ResolveBase(id hash.ObjectID, depth int) (object.Type, []byte, error)
}

type settings struct {
	cache    *Cache
	bases    BaseResolver
	index    *Index
	maxDepth int
}

type Option func(*settings)

func WithCache(cache *Cache) Option {
	return func(s *settings) { s.cache = cache }
}

func WithBaseResolver(resolver BaseResolver) Option {
	return func(s *settings) { s.bases = resolver }
}

func WithIndex(index *Index) Option {
	return func(s *settings) { s.index = index }
}

func WithMaxDeltaDepth(depth int) Option {
	return func(s *settings) { s.maxDepth = depth }
}

func newSettings(opts []Option) settings {
	applied := settings{maxDepth: DefaultMaxDeltaDepth}
	for _, opt := range opts {
		opt(&applied)
	}
	if applied.maxDepth <= 0 {
		applied.maxDepth = DefaultMaxDeltaDepth
	}
	if applied.cache == nil {
		applied.cache = NewCache(DefaultCacheBytes)
	}
	return applied
}
