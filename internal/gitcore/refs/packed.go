package refs

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

const (
	packedHeaderPrefix = "# pack-refs with:"
	traitPeeled        = "peeled"
	traitFullyPeeled   = "fully-peeled"
	traitSorted        = "sorted"
)

type packedSnapshot struct {
	refs        []Ref
	peeled      bool
	fullyPeeled bool
}

func (p *packedSnapshot) find(name Name) (Ref, bool) {
	index, ok := slices.BinarySearchFunc(p.refs, name, func(ref Ref, target Name) int {
		return strings.Compare(string(ref.Name), string(target))
	})
	if !ok {
		return Ref{}, false
	}
	return p.refs[index], true
}

func (p *packedSnapshot) hasPrefix(prefix string) bool {
	for _, ref := range p.refs {
		if strings.HasPrefix(string(ref.Name), prefix) {
			return true
		}
	}
	return false
}

func (s *Store) loadPacked() (*packedSnapshot, error) {
	data, err := s.common.read(packedRefsFile)
	if errors.Is(err, ErrNotFound) {
		return &packedSnapshot{}, nil
	}
	if err != nil {
		return nil, err
	}
	return parsePackedRefs(data)
}

func parsePackedRefs(data []byte) (*packedSnapshot, error) {
	snapshot := &packedSnapshot{}
	rest := data
	if len(rest) > 0 && rest[0] == '#' {
		line, tail, err := cutLine(rest)
		if err != nil {
			return nil, err
		}
		traits, ok := strings.CutPrefix(string(line), packedHeaderPrefix)
		if !ok {
			return nil, fmt.Errorf("%w: unknown header %q", ErrMalformedPacked, line)
		}
		for _, trait := range strings.Fields(traits) {
			switch trait {
			case traitPeeled:
				snapshot.peeled = true
			case traitFullyPeeled:
				snapshot.fullyPeeled = true
			}
		}
		rest = tail
	}
	for len(rest) > 0 {
		line, tail, err := cutLine(rest)
		if err != nil {
			return nil, err
		}
		rest = tail
		if line[0] == '^' {
			if err := attachPeeled(snapshot, line); err != nil {
				return nil, err
			}
			continue
		}
		ref, err := parsePackedLine(line)
		if err != nil {
			return nil, err
		}
		snapshot.refs = append(snapshot.refs, ref)
	}
	slices.SortFunc(snapshot.refs, func(a, b Ref) int {
		return strings.Compare(string(a.Name), string(b.Name))
	})
	for index := 1; index < len(snapshot.refs); index++ {
		if snapshot.refs[index-1].Name == snapshot.refs[index].Name {
			return nil, fmt.Errorf("%w: duplicate reference %s", ErrMalformedPacked, snapshot.refs[index].Name)
		}
	}
	return snapshot, nil
}

func cutLine(data []byte) ([]byte, []byte, error) {
	index := bytes.IndexByte(data, '\n')
	if index < 0 {
		return nil, nil, fmt.Errorf("%w: unterminated line %q", ErrMalformedPacked, data)
	}
	if index == 0 {
		return nil, nil, fmt.Errorf("%w: empty line", ErrMalformedPacked)
	}
	return data[:index], data[index+1:], nil
}

func parsePackedLine(line []byte) (Ref, error) {
	if len(line) < hash.HexSize+2 {
		return Ref{}, fmt.Errorf("%w: short line %q", ErrMalformedPacked, line)
	}
	id, err := hash.FromHex(line[:hash.HexSize])
	if err != nil {
		return Ref{}, fmt.Errorf("%w: %w", ErrMalformedPacked, err)
	}
	if !isSpaceByte(line[hash.HexSize]) {
		return Ref{}, fmt.Errorf("%w: no separator in %q", ErrMalformedPacked, line)
	}
	name := Name(line[hash.HexSize+1:])
	if err := CheckFormat(string(name), AllowOneLevel); err != nil {
		return Ref{}, fmt.Errorf("%w: %w", ErrMalformedPacked, err)
	}
	return Ref{Name: name, Target: id}, nil
}

func attachPeeled(snapshot *packedSnapshot, line []byte) error {
	if len(snapshot.refs) == 0 {
		return fmt.Errorf("%w: peeled line %q without a reference", ErrMalformedPacked, line)
	}
	last := &snapshot.refs[len(snapshot.refs)-1]
	if !last.Peeled.IsZero() {
		return fmt.Errorf("%w: second peeled line for %s", ErrMalformedPacked, last.Name)
	}
	if len(line) != hash.HexSize+1 {
		return fmt.Errorf("%w: bad peeled line %q", ErrMalformedPacked, line)
	}
	id, err := hash.FromHex(line[1:])
	if err != nil {
		return fmt.Errorf("%w: %w", ErrMalformedPacked, err)
	}
	last.Peeled = id
	return nil
}

func isSpaceByte(current byte) bool {
	return current == ' ' || current == '\t' || current == '\r' || current == '\v' || current == '\f'
}

func encodePackedRefs(refs []Ref, peeled bool) []byte {
	var text strings.Builder
	text.WriteString(packedHeaderPrefix)
	if peeled {
		text.WriteString(" " + traitPeeled + " " + traitFullyPeeled)
	}
	text.WriteString(" " + traitSorted + " \n")
	for _, ref := range refs {
		text.WriteString(ref.Target.String())
		text.WriteByte(' ')
		text.WriteString(string(ref.Name))
		text.WriteByte('\n')
		if !ref.Peeled.IsZero() {
			text.WriteByte('^')
			text.WriteString(ref.Peeled.String())
			text.WriteByte('\n')
		}
	}
	return []byte(text.String())
}
