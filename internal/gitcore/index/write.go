package index

import (
	"crypto/sha1"
	"encoding/binary"
	"errors"
	"fmt"
	stdhash "hash"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
)

func (x *Index) Write(w io.Writer, version int) error {
	data, err := x.encode(version)
	if err != nil {
		return err
	}
	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("index: write: %w", err)
	}
	return nil
}

func (x *Index) WriteFile(path string, version int) error {
	data, err := x.encode(version)
	if err != nil {
		return err
	}
	dir, base := filepath.Split(path)
	if dir == "" {
		dir = "."
	}
	root, err := fsOpenRoot(filepath.Clean(dir))
	if err != nil {
		return fmt.Errorf("index: open %s: %w", dir, err)
	}
	defer root.Close()
	return writeThroughLock(root, base, path, data)
}

func writeThroughLock(root *os.Root, base, path string, data []byte) error {
	lock := base + lockSuffix
	file, err := fsCreate(root, lock, os.O_WRONLY|os.O_CREATE|os.O_EXCL)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("%w: %s%s", ErrLocked, path, lockSuffix)
		}
		return fmt.Errorf("index: create %s%s: %w", path, lockSuffix, err)
	}
	if _, err := fsWrite(file, data); err != nil {
		_ = fsClose(file)
		_ = fsRemove(root, lock)
		return fmt.Errorf("index: write %s%s: %w", path, lockSuffix, err)
	}
	if err := fsClose(file); err != nil {
		_ = fsRemove(root, lock)
		return fmt.Errorf("index: close %s%s: %w", path, lockSuffix, err)
	}
	if err := fsRename(root, lock, base); err != nil {
		_ = fsRemove(root, lock)
		return fmt.Errorf("index: rename %s%s: %w", path, lockSuffix, err)
	}
	return nil
}

func (x *Index) effectiveVersion(version int) (int, error) {
	if version == 0 {
		version = x.Version
	}
	if version == 0 {
		version = Version2
	}
	if version < Version2 || version > Version4 {
		return 0, fmt.Errorf("%w: %d", ErrUnsupportedVersion, version)
	}
	if version == Version4 {
		return version, nil
	}
	for _, entry := range x.entries {
		if entry.Extended() {
			return Version3, nil
		}
	}
	return Version2, nil
}

func (x *Index) encode(version int) ([]byte, error) {
	version, err := x.effectiveVersion(version)
	if err != nil {
		return nil, err
	}
	buf := make([]byte, 0, headerSize+len(x.entries)*(baseEntrySize+entryAlignment)+hash.Size)
	buf = append(buf, signature...)
	buf = binary.BigEndian.AppendUint32(buf, uint32(version))
	buf = binary.BigEndian.AppendUint32(buf, uint32(len(x.entries)))
	offsets := make([]int, len(x.entries))
	fresh := x.blockStarts()
	previous := ""
	for at, entry := range x.entries {
		offsets[at] = len(buf)
		buf = appendEntry(buf, entry, version, previous, !fresh[at])
		previous = entry.Path
	}
	buf = x.appendExtensions(buf, offsets)
	if x.SkipHash {
		return append(buf, make([]byte, hash.Size)...), nil
	}
	sum := sha1.Sum(buf)
	return append(buf, sum[:]...), nil
}

func (x *Index) blockStarts() map[int]bool {
	if x.OffsetTable == nil || !x.OffsetTable.covers(len(x.entries)) {
		return nil
	}
	starts := make(map[int]bool, len(x.OffsetTable.Blocks))
	at := 0
	for _, block := range x.OffsetTable.Blocks {
		starts[at] = true
		at += int(block.Count)
	}
	return starts
}

func appendEntry(buf []byte, entry *Entry, version int, previous string, sharePrefix bool) []byte {
	buf = appendStat(buf, entry.Stat, entry.Mode)
	buf = append(buf, entry.ID[:]...)
	flags := uint16(entry.Stage&3) << stageShift
	if entry.AssumeValid {
		flags |= flagAssumeValid
	}
	if len(entry.Path) < nameMask {
		flags |= uint16(len(entry.Path))
	} else {
		flags |= nameMask
	}
	base := baseEntrySize
	if entry.Extended() {
		flags |= flagExtended
		base += flagsSize
	}
	buf = binary.BigEndian.AppendUint16(buf, flags)
	if entry.Extended() {
		buf = binary.BigEndian.AppendUint16(buf, extendedFlagsOf(entry))
	}
	if version < Version4 {
		buf = append(buf, entry.Path...)
		filled := base + len(entry.Path)
		return append(buf, make([]byte, (filled+entryAlignment)&^(entryAlignment-1)-filled)...)
	}
	common := 0
	if sharePrefix {
		common = commonPrefix(previous, entry.Path)
	}
	buf = appendVarint(buf, uint64(len(previous)-common))
	buf = append(buf, entry.Path[common:]...)
	return append(buf, 0)
}

func extendedFlagsOf(entry *Entry) uint16 {
	var flags uint16
	if entry.SkipWorktree {
		flags |= extSkipWorktree
	}
	if entry.IntentToAdd {
		flags |= extIntentToAdd
	}
	return flags
}

func commonPrefix(previous, path string) int {
	limit := min(len(previous), len(path))
	common := 0
	for common < limit && previous[common] == path[common] {
		common++
	}
	return common
}

func appendStat(buf []byte, stat Stat, mode object.Mode) []byte {
	buf = appendTime(buf, stat.CTime)
	buf = appendTime(buf, stat.MTime)
	buf = binary.BigEndian.AppendUint32(buf, stat.Dev)
	buf = binary.BigEndian.AppendUint32(buf, stat.Ino)
	buf = binary.BigEndian.AppendUint32(buf, uint32(mode))
	buf = binary.BigEndian.AppendUint32(buf, stat.UID)
	buf = binary.BigEndian.AppendUint32(buf, stat.GID)
	return binary.BigEndian.AppendUint32(buf, stat.Size)
}

func appendTime(buf []byte, stamp time.Time) []byte {
	buf = binary.BigEndian.AppendUint32(buf, uint32(stamp.Unix()))
	return binary.BigEndian.AppendUint32(buf, uint32(stamp.Nanosecond()))
}

func (x *Index) appendExtensions(buf []byte, offsets []int) []byte {
	entriesEnd := len(buf)
	digest := sha1.New()
	for _, ext := range x.extensions {
		payload, ok := x.extensionPayload(ext, offsets, entriesEnd, digest)
		if !ok {
			continue
		}
		header := binary.BigEndian.AppendUint32([]byte(ext.signature), uint32(len(payload)))
		if ext.signature != extEndOfEntries {
			_, _ = digest.Write(header)
		}
		buf = append(buf, header...)
		buf = append(buf, payload...)
	}
	return buf
}

func (x *Index) extensionPayload(ext extension, offsets []int, entriesEnd int, digest stdhash.Hash) ([]byte, bool) {
	switch ext.signature {
	case extCacheTree:
		if x.CacheTree == nil {
			return nil, false
		}
		return encodeCacheTree(x.CacheTree), true
	case extResolveUndo:
		if len(x.ResolveUndo) == 0 {
			return nil, false
		}
		return encodeResolveUndo(x.ResolveUndo), true
	case extUntracked:
		if x.Untracked == nil {
			return nil, false
		}
		return x.Untracked.Raw, true
	case extOffsetTable:
		if x.OffsetTable == nil || !x.OffsetTable.covers(len(offsets)) {
			return nil, false
		}
		return encodeOffsetTable(x.OffsetTable, offsets), true
	case extEndOfEntries:
		if x.EndOfEntries == nil {
			return nil, false
		}
		return digest.Sum(binary.BigEndian.AppendUint32(nil, uint32(entriesEnd))), true
	default:
		return ext.data, true
	}
}
