package refs

import (
	"bytes"
	"errors"
	"fmt"
	"iter"
	"os"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
)

type ReflogEntry struct {
	Old       hash.ObjectID
	New       hash.ObjectID
	Committer object.Signature
	Message   string
}

func reflogPath(name Name) string { return logsDir + "/" + string(name) }

func (s *Store) Reflog(name Name) iter.Seq2[ReflogEntry, error] {
	return func(yield func(ReflogEntry, error) bool) {
		if err := name.Validate(); err != nil {
			yield(ReflogEntry{}, err)
			return
		}
		data, err := s.treeFor(name).read(reflogPath(name))
		if errors.Is(err, ErrNotFound) {
			return
		}
		if err != nil {
			yield(ReflogEntry{}, err)
			return
		}
		if len(data) == 0 {
			return
		}
		for _, line := range bytes.Split(bytes.TrimSuffix(data, []byte("\n")), []byte("\n")) {
			entry, err := ParseReflogLine(line)
			if err != nil {
				yield(ReflogEntry{}, err)
				return
			}
			if !yield(entry, nil) {
				return
			}
		}
	}
}

func (s *Store) ReflogLast(name Name) (ReflogEntry, error) {
	last := ReflogEntry{}
	found := false
	for entry, err := range s.Reflog(name) {
		if err != nil {
			return ReflogEntry{}, err
		}
		last, found = entry, true
	}
	if !found {
		return ReflogEntry{}, fmt.Errorf("%w: reflog of %s", ErrNotFound, name)
	}
	return last, nil
}

func ParseReflogLine(line []byte) (ReflogEntry, error) {
	if len(line) < 2*hash.HexSize+2 {
		return ReflogEntry{}, fmt.Errorf("%w: short line %q", ErrMalformedReflog, line)
	}
	old, err := hash.FromHex(line[:hash.HexSize])
	if err != nil {
		return ReflogEntry{}, fmt.Errorf("%w: %w", ErrMalformedReflog, err)
	}
	if line[hash.HexSize] != ' ' {
		return ReflogEntry{}, fmt.Errorf("%w: no separator in %q", ErrMalformedReflog, line)
	}
	rest := line[hash.HexSize+1:]
	current, err := hash.FromHex(rest[:hash.HexSize])
	if err != nil {
		return ReflogEntry{}, fmt.Errorf("%w: %w", ErrMalformedReflog, err)
	}
	if rest[hash.HexSize] != ' ' {
		return ReflogEntry{}, fmt.Errorf("%w: no separator in %q", ErrMalformedReflog, line)
	}
	rest = rest[hash.HexSize+1:]
	signature, message, _ := bytes.Cut(rest, []byte("\t"))
	committer, err := object.ParseSignature(signature)
	if err != nil {
		return ReflogEntry{}, fmt.Errorf("%w: %w", ErrMalformedReflog, err)
	}
	return ReflogEntry{Old: old, New: current, Committer: committer, Message: string(message)}, nil
}

func FormatReflogMessage(message string) string {
	var text strings.Builder
	space := true
	for index := range len(message) {
		current := message[index]
		if space && isReflogSpace(current) {
			continue
		}
		space = false
		if current != '\n' {
			text.WriteByte(current)
			continue
		}
		text.WriteByte(' ')
		space = true
	}
	return strings.TrimRight(text.String(), " \t\r\v\f")
}

func isReflogSpace(current byte) bool {
	return current == '\n' || isSpaceByte(current)
}

func (s *Store) shouldCreateReflog(name Name) bool {
	if name == HEAD {
		return true
	}
	switch s.reflogPolicy() {
	case ReflogAlways:
		return true
	case ReflogDisabled:
		return false
	}
	return name.IsBranch() || name.IsRemote() || strings.HasPrefix(string(name), NotesPrefix)
}

func (s *Store) reflogPolicy() ReflogPolicy {
	if s.opts.Reflog != ReflogDefault {
		return s.opts.Reflog
	}
	if s.opts.Bare {
		return ReflogDisabled
	}
	return ReflogEnabled
}

func (s *Store) appendReflog(name Name, old, current hash.ObjectID, message string) error {
	from := s.treeFor(name)
	path := reflogPath(name)
	if !s.shouldCreateReflog(name) && !from.isFile(path) {
		return nil
	}
	if s.opts.Committer == nil {
		return fmt.Errorf("%w: %s", ErrMissingCommitter, name)
	}
	line := old.String() + " " + current.String() + " " + s.opts.Committer().String()
	if text := FormatReflogMessage(message); text != "" {
		line += "\t" + text
	}
	file, err := from.create(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND)
	if err != nil {
		return fmt.Errorf("%w: %s: %w", ErrWriteFailed, path, err)
	}
	defer func() { _ = file.Close() }()
	if _, err := fsWrite(file, []byte(line+"\n")); err != nil {
		return fmt.Errorf("%w: %s: %w", ErrWriteFailed, path, err)
	}
	return nil
}

func (s *Store) removeReflog(name Name) error {
	from := s.treeFor(name)
	if err := from.remove(reflogPath(name)); err != nil {
		return err
	}
	from.removeEmptyDirs(reflogPath(name), keepRefDirs+1)
	return nil
}
