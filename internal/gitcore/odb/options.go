package odb

import (
	"fmt"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/pack"
)

const (
	DefaultCacheBytes      = 32 << 20
	DefaultMaxDeltaDepth   = pack.DefaultMaxDeltaDepth
	MaxAlternatesDepth     = 5
	MaxTagChain            = 16
	MinShortPrefix         = 4
	DefaultMaxShortMatches = 64
	DefaultAbbrevLength    = 7
)

type Options struct {
	Format          hash.Format
	Alternates      []string
	CacheBytes      int64
	PackBytes       int64
	MaxDepth        int
	MaxShortMatches int
}

func (o Options) normalized() (Options, error) {
	if o.Format == 0 {
		o.Format = hash.SHA1
	}
	if !o.Format.Supported() {
		return o, fmt.Errorf("%w: %s", hash.ErrUnsupportedFormat, o.Format)
	}
	if o.CacheBytes <= 0 {
		o.CacheBytes = DefaultCacheBytes
	}
	if o.PackBytes <= 0 {
		o.PackBytes = pack.DefaultCacheBytes
	}
	if o.MaxDepth <= 0 {
		o.MaxDepth = DefaultMaxDeltaDepth
	}
	if o.MaxShortMatches <= 0 {
		o.MaxShortMatches = DefaultMaxShortMatches
	}
	return o, nil
}
