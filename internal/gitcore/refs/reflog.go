package refs

import (
	"bytes"
	"errors"
	"fmt"
	"iter"
	"os"
	"slices"
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

func (s *Store) DropReflogEntry(name Name, position int) error {
	if err := name.Validate(); err != nil {
		return err
	}
	from := s.treeFor(name)
	lock, err := newLock(from, string(name))
	if err != nil {
		return err
	}
	defer lock.release()
	var entries []ReflogEntry
	for entry, err := range s.Reflog(name) {
		if err != nil {
			return err
		}
		entries = append(entries, entry)
	}
	at := len(entries) - 1 - position
	if position < 0 || at < 0 {
		return fmt.Errorf("%w: %s@{%d}", ErrNotFound, name, position)
	}
	if at+1 < len(entries) {
		entries[at+1].Old = hash.Zero
		if at > 0 {
			entries[at+1].Old = entries[at-1].New
		}
	}
	entries = slices.Delete(entries, at, at+1)
	if len(entries) == 0 {
		lock.release()
		tx := s.Begin()
		_ = tx.Delete(name, hash.Zero)
		return tx.Commit()
	}
	var text strings.Builder
	for _, entry := range entries {
		text.WriteString(formatReflogLine(entry.Old, entry.New, entry.Committer, entry.Message) + "\n")
	}
	logLock, err := newLock(from, reflogPath(name))
	if err != nil {
		return err
	}
	defer logLock.release()
	if err := logLock.write([]byte(text.String())); err != nil {
		return err
	}
	if err := lock.write([]byte(entries[len(entries)-1].New.String() + "\n")); err != nil {
		return err
	}
	if err := logLock.commit(); err != nil {
		return err
	}
	return lock.commit()
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
	if name == HEAD || name == StashName {
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

func formatReflogLine(old, current hash.ObjectID, committer object.Signature, text string) string {
	line := old.String() + " " + current.String() + " " + committer.String()
	if text != "" {
		line += "\t" + text
	}
	return line
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
	line := formatReflogLine(old, current, s.opts.Committer(), FormatReflogMessage(message))
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
