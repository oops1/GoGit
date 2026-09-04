package index

import (
	"crypto/sha1"
	"encoding/binary"
	"fmt"
	"io"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
)

const (
	signature       = "DIRC"
	headerSize      = 12
	statSize        = 40
	flagsSize       = 2
	baseEntrySize   = statSize + hash.Size + flagsSize
	entryAlignment  = 8
	nameMask        = 0x0fff
	flagAssumeValid = 0x8000
	flagExtended    = 0x4000
	stageShift      = 12
	stageMask       = 0x3000
	extSkipWorktree = 0x4000
	extIntentToAdd  = 0x2000
	knownExtFlags   = extSkipWorktree | extIntentToAdd
)

func Read(r io.Reader) (*Index, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("index: read: %w", err)
	}
	return decode(data)
}

func ReadFile(path string) (*Index, error) {
	file, err := fsOpen(path)
	if err != nil {
		return nil, fmt.Errorf("index: open %s: %w", path, err)
	}
	defer file.Close()
	info, err := fsStat(file)
	if err != nil {
		return nil, fmt.Errorf("index: stat %s: %w", path, err)
	}
	idx, err := Read(file)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	idx.Timestamp = info.ModTime()
	return idx, nil
}

type decoder struct {
	data     []byte
	pos      int
	version  int
	previous string
}

func decode(data []byte) (*Index, error) {
	if len(data) < headerSize+hash.Size {
		return nil, fmt.Errorf("%w: the index holds %d bytes", ErrTruncated, len(data))
	}
	if string(data[:4]) != signature {
		return nil, fmt.Errorf("%w: %q", ErrBadSignature, data[:4])
	}
	version := int(binary.BigEndian.Uint32(data[4:8]))
	if version < Version2 || version > Version4 {
		return nil, fmt.Errorf("%w: %d", ErrUnsupportedVersion, version)
	}
	body := data[:len(data)-hash.Size]
	trailer := hash.ObjectID(data[len(data)-hash.Size:])
	if !trailer.IsZero() {
		if sum := hash.ObjectID(sha1.Sum(body)); sum != trailer {
			return nil, fmt.Errorf("%w: %s instead of %s", ErrChecksum, sum, trailer)
		}
	}
	count := binary.BigEndian.Uint32(data[8:12])
	if uint64(count) > uint64(len(body)-headerSize)/baseEntrySize {
		return nil, fmt.Errorf("%w: the header announces %d entries", ErrTruncated, count)
	}
	idx := &Index{Version: version, SkipHash: trailer.IsZero()}
	dec := &decoder{data: body, pos: headerSize, version: version}
	idx.entries = make([]*Entry, 0, count)
	for range count {
		entry, err := dec.entry()
		if err != nil {
			return nil, err
		}
		idx.entries = append(idx.entries, entry)
	}
	if err := dec.extensions(idx); err != nil {
		return nil, err
	}
	if err := verifyOrder(idx.entries); err != nil {
		return nil, err
	}
	return idx, nil
}

func (d *decoder) entry() (*Entry, error) {
	if len(d.data)-d.pos < baseEntrySize {
		return nil, fmt.Errorf("%w: an entry starts at %d", ErrTruncated, d.pos)
	}
	raw := d.data[d.pos:]
	entry := &Entry{
		Stat: parseStat(raw),
		Mode: object.Mode(binary.BigEndian.Uint32(raw[24:])),
		ID:   hash.ObjectID(raw[statSize : statSize+hash.Size]),
	}
	flags := binary.BigEndian.Uint16(raw[statSize+hash.Size:])
	entry.AssumeValid = flags&flagAssumeValid != 0
	entry.Stage = Stage(flags & stageMask >> stageShift)
	base := baseEntrySize
	if flags&flagExtended != 0 {
		if len(d.data)-d.pos < baseEntrySize+flagsSize {
			return nil, fmt.Errorf("%w: extended flags at %d", ErrTruncated, d.pos)
		}
		extended := binary.BigEndian.Uint16(raw[baseEntrySize:])
		if extended&^knownExtFlags != 0 {
			return nil, fmt.Errorf("%w: entry flags 0x%04x at %d", ErrUnsupported, extended, d.pos)
		}
		entry.SkipWorktree = extended&extSkipWorktree != 0
		entry.IntentToAdd = extended&extIntentToAdd != 0
		base += flagsSize
	}
	name, size, err := d.name(raw, base, int(flags&nameMask))
	if err != nil {
		return nil, err
	}
	entry.Path = name
	d.previous = name
	d.pos += size
	return entry, nil
}

func (d *decoder) name(raw []byte, base, declared int) (string, int, error) {
	if d.version < Version4 {
		return d.paddedName(raw, base, declared)
	}
	return d.compressedName(raw, base, declared)
}

func (d *decoder) paddedName(raw []byte, base, declared int) (string, int, error) {
	length := declared
	if declared == nameMask {
		end := indexOfZero(raw, base)
		if end < 0 {
			return "", 0, fmt.Errorf("%w: an entry name at %d is not terminated", ErrTruncated, d.pos)
		}
		length = end - base
	}
	if base+length >= len(raw) || raw[base+length] != 0 {
		return "", 0, fmt.Errorf("%w: an entry name at %d is not terminated", ErrTruncated, d.pos)
	}
	size := (base + length + entryAlignment) &^ (entryAlignment - 1)
	if size > len(raw) {
		return "", 0, fmt.Errorf("%w: an entry of %d bytes starts at %d", ErrTruncated, size, d.pos)
	}
	return string(raw[base : base+length]), size, nil
}

func (d *decoder) compressedName(raw []byte, base, declared int) (string, int, error) {
	strip, read, err := decodeVarint(raw[base:])
	if err != nil {
		return "", 0, fmt.Errorf("%w: an entry name at %d", err, d.pos)
	}
	if strip > uint64(len(d.previous)) {
		return "", 0, fmt.Errorf("%w: an entry at %d strips %d of %d bytes",
			ErrMalformed, d.pos, strip, len(d.previous))
	}
	common := d.previous[:uint64(len(d.previous))-strip]
	start := base + read
	end := indexOfZero(raw, start)
	if end < 0 {
		return "", 0, fmt.Errorf("%w: an entry name at %d is not terminated", ErrTruncated, d.pos)
	}
	if declared != nameMask && declared != len(common)+end-start {
		return "", 0, fmt.Errorf("%w: an entry at %d declares %d name bytes, %d follow",
			ErrMalformed, d.pos, declared, len(common)+end-start)
	}
	return common + string(raw[start:end]), end + 1, nil
}

func indexOfZero(raw []byte, from int) int {
	for at := from; at < len(raw); at++ {
		if raw[at] == 0 {
			return at
		}
	}
	return -1
}

func verifyOrder(entries []*Entry) error {
	for at := 1; at < len(entries); at++ {
		previous, current := entries[at-1], entries[at]
		switch order := comparePathStage(previous.Path, previous.Stage, current.Path, current.Stage); {
		case order > 0:
			return fmt.Errorf("%w: %q stage %d follows %q stage %d",
				ErrUnsorted, current.Path, current.Stage, previous.Path, previous.Stage)
		case order == 0:
			return fmt.Errorf("%w: %q appears twice at stage %d", ErrUnsorted, current.Path, current.Stage)
		case previous.Path == current.Path && previous.Stage == StageMerged:
			return fmt.Errorf("%w: %q is merged and staged at the same time", ErrUnsorted, current.Path)
		}
	}
	return nil
}

func (d *decoder) extensions(idx *Index) error {
	for d.pos < len(d.data) {
		if len(d.data)-d.pos < extensionHeader {
			return fmt.Errorf("%w: an extension header starts at %d", ErrTruncated, d.pos)
		}
		name := string(d.data[d.pos : d.pos+4])
		size := binary.BigEndian.Uint32(d.data[d.pos+4:])
		if uint64(size) > uint64(len(d.data)-d.pos-extensionHeader) {
			return fmt.Errorf("%w: extension %s announces %d bytes", ErrTruncated, name, size)
		}
		payload := d.data[d.pos+extensionHeader : d.pos+extensionHeader+int(size)]
		if err := loadExtension(idx, name, payload); err != nil {
			return err
		}
		d.pos += extensionHeader + int(size)
	}
	return nil
}

func loadExtension(idx *Index, name string, payload []byte) error {
	var err error
	switch name {
	case extCacheTree:
		idx.CacheTree, err = parseCacheTree(payload)
	case extResolveUndo:
		idx.ResolveUndo, err = parseResolveUndo(payload)
	case extUntracked:
		idx.Untracked, err = parseUntracked(payload)
	case extEndOfEntries:
		idx.EndOfEntries, err = parseEndOfEntries(payload)
	case extOffsetTable:
		idx.OffsetTable, err = parseOffsetTable(payload)
	case extSplitIndex:
		err = fmt.Errorf("%w: the index is split, its entries live in a shared index file", ErrUnsupported)
	default:
		if !optionalExtension(name) {
			err = fmt.Errorf("%w: %s", ErrUnsupportedExtension, name)
		}
	}
	if err != nil {
		return err
	}
	idx.extensions = append(idx.extensions, extension{signature: name, data: payload})
	return nil
}
