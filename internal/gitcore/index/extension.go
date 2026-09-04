package index

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"slices"
	"strconv"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
)

const (
	extCacheTree      = "TREE"
	extResolveUndo    = "REUC"
	extUntracked      = "UNTR"
	extSplitIndex     = "link"
	extEndOfEntries   = "EOIE"
	extOffsetTable    = "IEOT"
	extensionHeader   = 8
	endOfEntriesSize  = 4 + hash.Size
	offsetTableStride = 8
	untrackedStatSize = 36
	untrackedFlags    = 4
)

type extension struct {
	signature string
	data      []byte
}

var extensionRank = map[string]int{
	extOffsetTable:  0,
	extSplitIndex:   1,
	extCacheTree:    2,
	extResolveUndo:  3,
	extUntracked:    4,
	extEndOfEntries: 6,
}

func rankOf(signature string) int {
	if rank, ok := extensionRank[signature]; ok {
		return rank
	}
	return 5
}

func optionalExtension(signature string) bool {
	return signature[0] >= 'A' && signature[0] <= 'Z'
}

type ResolveUndoEntry struct {
	Path  string
	Modes [3]object.Mode
	IDs   [3]hash.ObjectID
}

type UntrackedCache struct {
	Ident            []string
	InfoExcludeStat  Stat
	ExcludesFileStat Stat
	DirFlags         uint32
	InfoExcludeID    hash.ObjectID
	ExcludesFileID   hash.ObjectID
	ExcludePerDir    string
	Directories      uint64
	Raw              []byte
}

type EndOfEntries struct {
	Offset uint32
	ID     hash.ObjectID
}

type OffsetBlock struct {
	Offset uint32
	Count  uint32
}

type OffsetTable struct {
	Version uint32
	Blocks  []OffsetBlock
}

func parseResolveUndo(data []byte) ([]ResolveUndoEntry, error) {
	var entries []ResolveUndoEntry
	pos := 0
	for pos < len(data) {
		path, next, err := readCString(data, pos)
		if err != nil {
			return nil, fmt.Errorf("%w: resolve undo path", err)
		}
		entry := ResolveUndoEntry{Path: path}
		for stage := range entry.Modes {
			text, after, err := readCString(data, next)
			if err != nil {
				return nil, fmt.Errorf("%w: resolve undo mode of %q", err, path)
			}
			mode, err := strconv.ParseUint(text, 8, 32)
			if err != nil {
				return nil, fmt.Errorf("%w: resolve undo mode %q of %q", ErrMalformed, text, path)
			}
			entry.Modes[stage] = object.Mode(mode)
			next = after
		}
		for stage, mode := range entry.Modes {
			if mode == 0 {
				continue
			}
			if len(data)-next < hash.Size {
				return nil, fmt.Errorf("%w: resolve undo object id of %q", ErrTruncated, path)
			}
			entry.IDs[stage] = hash.ObjectID(data[next : next+hash.Size])
			next += hash.Size
		}
		entries = append(entries, entry)
		pos = next
	}
	return entries, nil
}

func encodeResolveUndo(entries []ResolveUndoEntry) []byte {
	var buf bytes.Buffer
	for _, entry := range entries {
		buf.WriteString(entry.Path)
		buf.WriteByte(0)
		for _, mode := range entry.Modes {
			buf.WriteString(strconv.FormatUint(uint64(mode), 8))
			buf.WriteByte(0)
		}
		for stage, mode := range entry.Modes {
			if mode != 0 {
				buf.Write(entry.IDs[stage][:])
			}
		}
	}
	return buf.Bytes()
}

func parseUntracked(data []byte) (*UntrackedCache, error) {
	identSize, pos, err := decodeVarint(data)
	if err != nil {
		return nil, fmt.Errorf("%w: untracked cache identity length", err)
	}
	if identSize > uint64(len(data)-pos) {
		return nil, fmt.Errorf("%w: untracked cache identity of %d bytes", ErrTruncated, identSize)
	}
	cache := &UntrackedCache{Raw: bytes.Clone(data)}
	for _, part := range bytes.Split(data[pos:pos+int(identSize)], []byte{0}) {
		if len(part) > 0 {
			cache.Ident = append(cache.Ident, string(part))
		}
	}
	pos += int(identSize)
	if len(data)-pos < 2*untrackedStatSize+untrackedFlags+2*hash.Size {
		return nil, fmt.Errorf("%w: untracked cache header", ErrTruncated)
	}
	cache.InfoExcludeStat = parseUntrackedStat(data[pos:])
	cache.ExcludesFileStat = parseUntrackedStat(data[pos+untrackedStatSize:])
	pos += 2 * untrackedStatSize
	cache.DirFlags = binary.BigEndian.Uint32(data[pos:])
	pos += untrackedFlags
	cache.InfoExcludeID = hash.ObjectID(data[pos : pos+hash.Size])
	cache.ExcludesFileID = hash.ObjectID(data[pos+hash.Size : pos+2*hash.Size])
	pos += 2 * hash.Size
	perDir, pos, err := readCString(data, pos)
	if err != nil {
		return nil, fmt.Errorf("%w: untracked cache per directory file name", err)
	}
	cache.ExcludePerDir = perDir
	directories, _, err := decodeVarint(data[pos:])
	if err != nil {
		return nil, fmt.Errorf("%w: untracked cache directory count", err)
	}
	cache.Directories = directories
	return cache, nil
}

func parseEndOfEntries(data []byte) (*EndOfEntries, error) {
	if len(data) != endOfEntriesSize {
		return nil, fmt.Errorf("%w: end of index entries holds %d bytes", ErrMalformed, len(data))
	}
	return &EndOfEntries{
		Offset: binary.BigEndian.Uint32(data),
		ID:     hash.ObjectID(data[4:]),
	}, nil
}

func parseOffsetTable(data []byte) (*OffsetTable, error) {
	if len(data) < 4 || (len(data)-4)%offsetTableStride != 0 {
		return nil, fmt.Errorf("%w: index entry offset table holds %d bytes", ErrMalformed, len(data))
	}
	table := &OffsetTable{Version: binary.BigEndian.Uint32(data)}
	for pos := 4; pos < len(data); pos += offsetTableStride {
		table.Blocks = append(table.Blocks, OffsetBlock{
			Offset: binary.BigEndian.Uint32(data[pos:]),
			Count:  binary.BigEndian.Uint32(data[pos+4:]),
		})
	}
	return table, nil
}

func encodeOffsetTable(table *OffsetTable, offsets []int) []byte {
	buf := binary.BigEndian.AppendUint32(nil, table.Version)
	at := 0
	for _, block := range table.Blocks {
		buf = binary.BigEndian.AppendUint32(buf, uint32(offsets[at]))
		buf = binary.BigEndian.AppendUint32(buf, block.Count)
		at += int(block.Count)
	}
	return buf
}

func (t *OffsetTable) covers(entries int) bool {
	total := uint64(0)
	for _, block := range t.Blocks {
		if block.Count == 0 {
			return false
		}
		total += uint64(block.Count)
	}
	return total == uint64(entries)
}

func parseStat(data []byte) Stat {
	return Stat{
		CTime: timeOf(binary.BigEndian.Uint32(data), binary.BigEndian.Uint32(data[4:])),
		MTime: timeOf(binary.BigEndian.Uint32(data[8:]), binary.BigEndian.Uint32(data[12:])),
		Dev:   binary.BigEndian.Uint32(data[16:]),
		Ino:   binary.BigEndian.Uint32(data[20:]),
		UID:   binary.BigEndian.Uint32(data[28:]),
		GID:   binary.BigEndian.Uint32(data[32:]),
		Size:  binary.BigEndian.Uint32(data[36:]),
	}
}

func parseUntrackedStat(data []byte) Stat {
	return Stat{
		CTime: timeOf(binary.BigEndian.Uint32(data), binary.BigEndian.Uint32(data[4:])),
		MTime: timeOf(binary.BigEndian.Uint32(data[8:]), binary.BigEndian.Uint32(data[12:])),
		Dev:   binary.BigEndian.Uint32(data[16:]),
		Ino:   binary.BigEndian.Uint32(data[20:]),
		UID:   binary.BigEndian.Uint32(data[24:]),
		GID:   binary.BigEndian.Uint32(data[28:]),
		Size:  binary.BigEndian.Uint32(data[32:]),
	}
}

func (x *Index) extensionAt(signature string) int {
	return slices.IndexFunc(x.extensions, func(ext extension) bool { return ext.signature == signature })
}

func (x *Index) ensureExtension(signature string) {
	if x.extensionAt(signature) >= 0 {
		return
	}
	rank := rankOf(signature)
	at := len(x.extensions)
	for position, ext := range x.extensions {
		if rankOf(ext.signature) > rank {
			at = position
			break
		}
	}
	x.extensions = slices.Insert(x.extensions, at, extension{signature: signature})
}
