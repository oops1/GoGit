package transport

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

var ErrUnsupportedFilter = errors.New("transport: unsupported object filter")

const (
	FilterBlobNone  = "blob:none"
	filterBlobLimit = "blob:limit="
	filterTreeDepth = "tree:"
)

type Filter struct {
	spec      string
	blobLimit int64
	treeDepth int
	omitBlobs bool
	limited   bool
	trees     bool
}

func ParseFilter(spec string) (Filter, error) {
	switch {
	case spec == "":
		return Filter{}, nil
	case spec == FilterBlobNone:
		return Filter{spec: spec, omitBlobs: true}, nil
	case strings.HasPrefix(spec, filterBlobLimit):
		size, err := parseFilterSize(strings.TrimPrefix(spec, filterBlobLimit))
		if err != nil {
			return Filter{}, err
		}
		return Filter{spec: spec, blobLimit: size, limited: true}, nil
	case strings.HasPrefix(spec, filterTreeDepth):
		depth, err := strconv.Atoi(strings.TrimPrefix(spec, filterTreeDepth))
		if err != nil || depth < 0 {
			return Filter{}, fmt.Errorf("%w: %s", ErrUnsupportedFilter, spec)
		}
		return Filter{spec: spec, treeDepth: depth, trees: true}, nil
	}
	return Filter{}, fmt.Errorf("%w: %s", ErrUnsupportedFilter, spec)
}

func parseFilterSize(text string) (int64, error) {
	if text == "" {
		return 0, fmt.Errorf("%w: %s%s", ErrUnsupportedFilter, filterBlobLimit, text)
	}
	unit := int64(1)
	switch text[len(text)-1] {
	case 'k', 'K':
		unit = 1 << 10
	case 'm', 'M':
		unit = 1 << 20
	case 'g', 'G':
		unit = 1 << 30
	}
	digits := text
	if unit > 1 {
		digits = text[:len(text)-1]
	}
	size, err := strconv.ParseInt(digits, 10, 64)
	if err != nil || size < 0 {
		return 0, fmt.Errorf("%w: %s%s", ErrUnsupportedFilter, filterBlobLimit, text)
	}
	return size * unit, nil
}

func (f Filter) Spec() string { return f.spec }

func (f Filter) IsZero() bool { return f.spec == "" }

func (f Filter) OmitsBlob(size int64, depth int) bool {
	switch {
	case f.omitBlobs:
		return true
	case f.limited:
		return size >= f.blobLimit
	case f.trees:
		return depth >= f.treeDepth
	}
	return false
}

func (f Filter) OmitsTree(depth int) bool {
	return f.trees && depth >= f.treeDepth
}
