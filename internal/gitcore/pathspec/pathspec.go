package pathspec

import (
	"errors"
	"fmt"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/wildmatch"
)

var (
	ErrEmpty            = errors.New("pathspec: an empty string is not a valid pathspec")
	ErrMagic            = errors.New("pathspec: invalid pathspec magic")
	ErrUnsupportedMagic = errors.New("pathspec: unsupported pathspec magic")
	ErrOutsideTree      = errors.New("pathspec: the path is outside the repository")
)

type Magic uint8

const (
	Top Magic = 1 << iota
	Literal
	Glob
	ICase
	Exclude
)

const (
	globSpecial  = "*?[\\"
	shortMagic   = "!\"#%&',-/;<=>@_`~"
	longMagicEnd = ')'
)

var magicNames = map[string]Magic{
	"top":     Top,
	"literal": Literal,
	"glob":    Glob,
	"icase":   ICase,
	"exclude": Exclude,
}

type Item struct {
	Match      string
	Magic      Magic
	noWildcard int
	oneStar    bool
}

type Set struct {
	include []Item
	exclude []Item
}

func Parse(specs []string) (Set, error) {
	var set Set
	for _, spec := range specs {
		item, err := ParseItem(spec)
		if err != nil {
			return Set{}, err
		}
		if item.Magic&Exclude != 0 {
			set.exclude = append(set.exclude, item)
			continue
		}
		set.include = append(set.include, item)
	}
	if len(set.exclude) > 0 && len(set.include) == 0 {
		set.include = []Item{{}}
	}
	return set, nil
}

func ParseItem(spec string) (Item, error) {
	if spec == "" {
		return Item{}, ErrEmpty
	}
	magic, rest, err := parseMagic(spec)
	if err != nil {
		return Item{}, err
	}
	if magic&Literal != 0 && magic&Glob != 0 {
		return Item{}, fmt.Errorf("%w: 'literal' and 'glob' are incompatible in %q", ErrMagic, spec)
	}
	match := rest
	if magic&Top == 0 {
		if match, err = normalize(rest); err != nil {
			return Item{}, fmt.Errorf("%w: %q", err, spec)
		}
	}
	item := Item{Match: match, Magic: magic, noWildcard: len(match)}
	if magic&Literal == 0 {
		item.noWildcard = simpleLength(match)
		item.oneStar = magic&Glob == 0 && item.noWildcard < len(match) &&
			match[item.noWildcard] == '*' && !strings.ContainsAny(match[item.noWildcard+1:], globSpecial)
	}
	return item, nil
}

func parseMagic(spec string) (Magic, string, error) {
	if !strings.HasPrefix(spec, ":") {
		return 0, spec, nil
	}
	if strings.HasPrefix(spec, ":(") {
		return parseLongMagic(spec)
	}
	return parseShortMagic(spec)
}

func parseLongMagic(spec string) (Magic, string, error) {
	end := strings.IndexByte(spec, longMagicEnd)
	if end < 0 {
		return 0, "", fmt.Errorf("%w: missing ')' at the end of the magic in %q", ErrMagic, spec)
	}
	var magic Magic
	for word := range strings.SplitSeq(spec[2:end], ",") {
		if word == "" {
			continue
		}
		if bit, known := magicNames[word]; known {
			magic |= bit
			continue
		}
		if strings.HasPrefix(word, "attr:") || strings.HasPrefix(word, "prefix:") {
			return 0, "", fmt.Errorf("%w: %q in %q", ErrUnsupportedMagic, word, spec)
		}
		return 0, "", fmt.Errorf("%w: %q in %q", ErrMagic, word, spec)
	}
	return magic, spec[end+1:], nil
}

func parseShortMagic(spec string) (Magic, string, error) {
	var magic Magic
	at := 1
	for ; at < len(spec) && spec[at] != ':'; at++ {
		switch c := spec[at]; {
		case c == '/':
			magic |= Top
		case c == '!' || c == '^':
			magic |= Exclude
		case strings.IndexByte(shortMagic, c) >= 0:
			return 0, "", fmt.Errorf("%w: %q in %q", ErrUnsupportedMagic, c, spec)
		default:
			return magic, spec[at:], nil
		}
	}
	if at < len(spec) {
		at++
	}
	return magic, spec[at:], nil
}

func normalize(path string) (string, error) {
	if strings.HasPrefix(path, "/") {
		return "", ErrOutsideTree
	}
	var parts []string
	trailing := false
	for part := range strings.SplitSeq(path, "/") {
		trailing = true
		switch part {
		case "", ".":
		case "..":
			if len(parts) == 0 {
				return "", ErrOutsideTree
			}
			parts = parts[:len(parts)-1]
		default:
			parts = append(parts, part)
			trailing = false
		}
	}
	if len(parts) == 0 {
		return "", nil
	}
	out := strings.Join(parts, "/")
	if trailing {
		out += "/"
	}
	return out, nil
}

func simpleLength(match string) int {
	if at := strings.IndexAny(match, globSpecial); at >= 0 {
		return at
	}
	return len(match)
}

func (s Set) Empty() bool {
	return len(s.include) == 0
}

func (s Set) Match(path string) bool {
	if s.Empty() {
		return true
	}
	matched := false
	for _, item := range s.include {
		if item.matches(path) {
			matched = true
			break
		}
	}
	if !matched {
		return false
	}
	for _, item := range s.exclude {
		if item.matches(path) {
			return false
		}
	}
	return true
}

func (s Set) MayMatchUnder(dir string) bool {
	if s.Empty() {
		return true
	}
	prefix := dir + "/"
	for _, item := range s.include {
		if item.mayMatchUnder(prefix) {
			return true
		}
	}
	return false
}

func (it Item) matches(path string) bool {
	match := it.Match
	if match == "" {
		return true
	}
	if len(path) >= len(match) && it.equal(path[:len(match)], match) {
		if len(path) == len(match) || match[len(match)-1] == '/' || path[len(match)] == '/' {
			return true
		}
	}
	if it.noWildcard == len(match) {
		return false
	}
	return it.wildMatches(path)
}

func (it Item) wildMatches(path string) bool {
	prefix := it.noWildcard
	if len(path) < prefix || !it.equal(path[:prefix], it.Match[:prefix]) {
		return false
	}
	pattern, text := it.Match[prefix:], path[prefix:]
	if it.oneStar {
		suffix := pattern[1:]
		return len(text) >= len(suffix) && it.equal(text[len(text)-len(suffix):], suffix)
	}
	var flags wildmatch.Flags
	if it.Magic&ICase != 0 {
		flags |= wildmatch.CaseFold
	}
	if it.Magic&Glob != 0 {
		flags |= wildmatch.Pathname
	}
	return wildmatch.Match(pattern, text, flags)
}

func (it Item) mayMatchUnder(prefix string) bool {
	fixed := it.Match[:it.noWildcard]
	if len(prefix) <= len(fixed) {
		return it.equal(fixed[:len(prefix)], prefix)
	}
	if !it.equal(prefix[:len(fixed)], fixed) {
		return false
	}
	return it.noWildcard < len(it.Match) || fixed == "" || fixed[len(fixed)-1] == '/' || prefix[len(fixed)] == '/'
}

func (it Item) equal(a, b string) bool {
	if it.Magic&ICase == 0 {
		return a == b
	}
	for at := range len(a) {
		if lower(a[at]) != lower(b[at]) {
			return false
		}
	}
	return true
}

func lower(c byte) byte {
	if 'A' <= c && c <= 'Z' {
		return c + 'a' - 'A'
	}
	return c
}
