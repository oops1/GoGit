package pack

import (
	"cmp"
	"encoding/binary"
	"fmt"
	"slices"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

type Reuse struct {
	files   []*PackFile
	reverse map[*PackFile]*reverseIndex
}

type reverseIndex struct {
	offsets   []int64
	positions []int
}

type storedObject struct {
	file *PackFile
	head ObjectHeader
	end  int64
	crc  uint32
	base hash.ObjectID
}

func NewReuse(stores ...*Store) *Reuse {
	reuse := &Reuse{reverse: make(map[*PackFile]*reverseIndex)}
	for _, store := range stores {
		reuse.files = append(reuse.files, store.acquire()...)
	}
	return reuse
}

func (r *Reuse) Close() {
	if r == nil {
		return
	}
	release(r.files)
	r.files = nil
}

func (r *Reuse) find(id hash.ObjectID) (*storedObject, error) {
	if r == nil {
		return nil, nil
	}
	for _, file := range r.files {
		if file.Index.Version() != indexVersion {
			continue
		}
		position, ok, err := file.Index.Position(id)
		if err != nil {
			return nil, err
		}
		if ok {
			return r.locate(file, position)
		}
	}
	return nil, nil
}

func (r *Reuse) locate(file *PackFile, position int) (*storedObject, error) {
	entry, err := file.Index.EntryAt(position)
	if err != nil {
		return nil, err
	}
	head, err := file.Pack.HeaderAt(entry.Offset)
	if err != nil {
		return nil, err
	}
	reverse, err := r.reverseOf(file)
	if err != nil {
		return nil, err
	}
	stored := &storedObject{file: file, head: head, crc: entry.CRC32, end: file.Pack.size - hash.Size, base: head.BaseID}
	at, _ := slices.BinarySearch(reverse.offsets, entry.Offset)
	if at+1 < len(reverse.offsets) {
		stored.end = reverse.offsets[at+1]
	}
	if stored.end < head.DataOffset {
		return nil, fmt.Errorf("%w: the object at %d of %s overlaps the next one at %d", ErrBadOffset, head.Offset, file.Name, stored.end)
	}
	if head.Kind != KindOffsetDelta {
		return stored, nil
	}
	base, found := slices.BinarySearch(reverse.offsets, head.BaseOffset)
	if !found {
		return nil, fmt.Errorf("%w: the delta at %d of %s refers to %d, where no object starts", ErrBadOffset, head.Offset, file.Name, head.BaseOffset)
	}
	if stored.base, err = file.Index.idAt(reverse.positions[base]); err != nil {
		return nil, err
	}
	return stored, nil
}

func (r *Reuse) reverseOf(file *PackFile) (*reverseIndex, error) {
	if reverse, ok := r.reverse[file]; ok {
		return reverse, nil
	}
	offsets, err := file.Index.offsetTable()
	if err != nil {
		return nil, err
	}
	positions := make([]int, len(offsets))
	for position := range positions {
		positions[position] = position
	}
	slices.SortFunc(positions, func(a, b int) int { return cmp.Compare(offsets[a], offsets[b]) })
	reverse := &reverseIndex{offsets: make([]int64, len(offsets)), positions: positions}
	for at, position := range positions {
		reverse.offsets[at] = offsets[position]
	}
	r.reverse[file] = reverse
	return reverse, nil
}

func (x *Index) offsetTable() ([]int64, error) {
	raw := make([]byte, x.count*offsetSize)
	if err := readFull(x.source, raw, x.offsets); err != nil {
		return nil, err
	}
	offsets := make([]int64, x.count)
	for position := range offsets {
		value := binary.BigEndian.Uint32(raw[position*offsetSize:])
		if value&largeOffsetFlag == 0 {
			offsets[position] = int64(value)
			continue
		}
		offset, err := x.offsetAt(position)
		if err != nil {
			return nil, err
		}
		offsets[position] = offset
	}
	return offsets, nil
}
