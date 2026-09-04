package odb

import (
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

func (d *DB) ResolveShort(prefix string) ([]hash.ObjectID, error) {
	if err := validateShortPrefix(prefix); err != nil {
		return nil, err
	}
	if len(prefix) == hash.HexSize {
		return d.resolveFullHash(prefix)
	}
	seen := make(map[hash.ObjectID]struct{})
	var matches []hash.ObjectID
	if err := d.collectShort(prefix, seen, &matches); err != nil {
		return nil, err
	}
	slices.SortFunc(matches, func(a, b hash.ObjectID) int { return a.Compare(b) })
	if limit := d.opts.MaxShortMatches; len(matches) > limit {
		matches = matches[:limit]
	}
	return matches, nil
}

func (d *DB) AbbreviateID(id hash.ObjectID, minLen int) (string, error) {
	if minLen <= 0 {
		minLen = DefaultAbbrevLength
	}
	if minLen < MinShortPrefix {
		minLen = MinShortPrefix
	}
	full := id.String()
	for length := minLen; length < hash.HexSize; length++ {
		matches, err := d.ResolveShort(full[:length])
		if err != nil {
			return "", err
		}
		if len(matches) == 1 && matches[0] == id {
			return full[:length], nil
		}
	}
	ok, err := d.Has(id)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	return full, nil
}

func (d *DB) resolveFullHash(prefix string) ([]hash.ObjectID, error) {
	var id hash.ObjectID
	_, _ = hex.Decode(id[:], []byte(prefix))
	ok, err := d.Has(id)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	return []hash.ObjectID{id}, nil
}

func (d *DB) collectShort(prefix string, seen map[hash.ObjectID]struct{}, out *[]hash.ObjectID) error {
	if err := d.looseShort(prefix, seen, out); err != nil {
		return err
	}
	if store := d.store(); store != nil {
		id, bits := decodePrefix(prefix)
		for _, file := range store.Files() {
			for match := range file.Index.Prefix(id[:], bits) {
				addShortMatch(seen, match, out)
			}
		}
	}
	for _, alternate := range d.alternates {
		if err := alternate.collectShort(prefix, seen, out); err != nil {
			return err
		}
	}
	return nil
}

func (d *DB) looseShort(prefix string, seen map[hash.ObjectID]struct{}, out *[]hash.ObjectID) error {
	fanout := prefix[:fanoutLength]
	rest := prefix[fanoutLength:]
	entries, err := d.readDir(fanout)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), rest) {
			continue
		}
		id, ok := looseID(fanout, entry.Name())
		if !ok {
			continue
		}
		addShortMatch(seen, id, out)
	}
	return nil
}

func addShortMatch(seen map[hash.ObjectID]struct{}, id hash.ObjectID, out *[]hash.ObjectID) {
	if _, ok := seen[id]; ok {
		return
	}
	seen[id] = struct{}{}
	*out = append(*out, id)
}

func validateShortPrefix(prefix string) error {
	if len(prefix) < MinShortPrefix || len(prefix) > hash.HexSize {
		return fmt.Errorf("%w: %q", ErrInvalidPrefix, prefix)
	}
	for _, r := range prefix {
		if !isLowerHexDigit(r) {
			return fmt.Errorf("%w: %q", ErrInvalidPrefix, prefix)
		}
	}
	return nil
}

func isLowerHexDigit(r rune) bool {
	return (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')
}

func decodePrefix(prefix string) (hash.ObjectID, int) {
	var id hash.ObjectID
	full := len(prefix) / 2
	if full > 0 {
		_, _ = hex.Decode(id[:full], []byte(prefix[:full*2]))
	}
	if len(prefix)%2 == 1 {
		var pad [1]byte
		_, _ = hex.Decode(pad[:], []byte{prefix[full*2], '0'})
		id[full] = pad[0]
	}
	return id, len(prefix) * 4
}
