package pack

import (
	"compress/zlib"
	"context"
	"crypto/sha1"
	"encoding/binary"
	"fmt"
	stdhash "hash"
	"hash/crc32"
	"io"
	"slices"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/progress"
)

type ObjectSource interface {
	Get(id hash.ObjectID) (object.Type, []byte, error)
}

type WriteOptions struct {
	Window   int
	Depth    int
	Thin     map[hash.ObjectID]struct{}
	Progress progress.Func
}

type WriteResult struct {
	Checksum hash.ObjectID
	Entries  []Entry
	Objects  int
	Bytes    int64
}

type writeObject struct {
	id   hash.ObjectID
	kind object.Type
	data []byte
}

type writeChoice struct {
	kind    Kind
	payload []byte
	base    []byte
	depth   int
}

type packedObject struct {
	writeObject
	offset int64
	depth  int
}

type thinBase struct {
	id   hash.ObjectID
	kind object.Type
	data []byte
}

func WritePack(ctx context.Context, dst io.Writer, src ObjectSource, ids []hash.ObjectID, opts WriteOptions) (WriteResult, error) {
	objects, err := fetchObjects(src, ids)
	if err != nil {
		return WriteResult{}, err
	}
	thins, err := fetchThinBases(src, opts.Thin)
	if err != nil {
		return WriteResult{}, err
	}
	order := sortForDelta(objects)
	writer := newPackWriter(dst)
	if err := writer.writeHeader(len(order)); err != nil {
		return WriteResult{}, err
	}
	result := WriteResult{Entries: make([]Entry, 0, len(order)), Objects: len(order)}
	written := make([]packedObject, 0, len(order))
	total := int64(len(order))
	for i, obj := range order {
		if err := ctx.Err(); err != nil {
			return WriteResult{}, err
		}
		offset := writer.position
		choice := chooseEncoding(obj, offset, written, thins, opts)
		opts.Progress.Count(progress.PhaseCompressing, int64(i)+1, total)
		entry, err := writer.writeObject(obj.id, choice)
		if err != nil {
			return WriteResult{}, err
		}
		result.Entries = append(result.Entries, entry)
		written = append(written, packedObject{writeObject: obj, offset: entry.Offset, depth: choice.depth})
		opts.Progress.Count(progress.PhaseWriting, int64(i)+1, total)
	}
	checksum, err := writer.finish()
	if err != nil {
		return WriteResult{}, err
	}
	result.Checksum = checksum
	result.Bytes = writer.position
	return result, nil
}

func fetchObjects(src ObjectSource, ids []hash.ObjectID) ([]writeObject, error) {
	objects := make([]writeObject, 0, len(ids))
	for _, id := range ids {
		kind, data, err := src.Get(id)
		if err != nil {
			return nil, fmt.Errorf("pack: get %s: %w", id, err)
		}
		objects = append(objects, writeObject{id: id, kind: kind, data: data})
	}
	return objects, nil
}

func fetchThinBases(src ObjectSource, thin map[hash.ObjectID]struct{}) ([]thinBase, error) {
	if len(thin) == 0 {
		return nil, nil
	}
	ids := make([]hash.ObjectID, 0, len(thin))
	for id := range thin {
		ids = append(ids, id)
	}
	slices.SortFunc(ids, func(a, b hash.ObjectID) int { return a.Compare(b) })
	bases := make([]thinBase, 0, len(ids))
	for _, id := range ids {
		kind, data, err := src.Get(id)
		if err != nil {
			return nil, fmt.Errorf("pack: get thin base %s: %w", id, err)
		}
		bases = append(bases, thinBase{id: id, kind: kind, data: data})
	}
	return bases, nil
}

func sortForDelta(objects []writeObject) []writeObject {
	order := slices.Clone(objects)
	slices.SortStableFunc(order, func(a, b writeObject) int {
		if a.kind != b.kind {
			return int(a.kind) - int(b.kind)
		}
		return len(b.data) - len(a.data)
	})
	return order
}

func chooseEncoding(obj writeObject, offset int64, written []packedObject, thins []thinBase, opts WriteOptions) writeChoice {
	raw := writeChoice{kind: Kind(obj.kind), payload: obj.data}
	if opts.Window <= 0 || opts.Depth <= 0 {
		return raw
	}
	best := searchOffsetDelta(obj, offset, written, opts)
	if ref := searchRefDelta(obj, thins); ref != nil && (best == nil || len(ref.payload) < len(best.payload)) {
		best = ref
	}
	if best == nil || len(best.payload) >= len(obj.data) {
		return raw
	}
	return *best
}

func searchOffsetDelta(obj writeObject, offset int64, written []packedObject, opts WriteOptions) *writeChoice {
	var best *writeChoice
	count := 0
	for i := len(written) - 1; i >= 0 && count < opts.Window; i-- {
		candidate := written[i]
		if candidate.kind != obj.kind {
			continue
		}
		count++
		if candidate.depth+1 > opts.Depth {
			continue
		}
		delta := EncodeDelta(candidate.data, obj.data)
		if best == nil || len(delta) < len(best.payload) {
			best = &writeChoice{
				kind:    KindOffsetDelta,
				payload: delta,
				base:    encodeOffsetDelta(offset - candidate.offset),
				depth:   candidate.depth + 1,
			}
		}
	}
	return best
}

func searchRefDelta(obj writeObject, thins []thinBase) *writeChoice {
	var best *writeChoice
	for _, candidate := range thins {
		if candidate.kind != obj.kind {
			continue
		}
		delta := EncodeDelta(candidate.data, obj.data)
		if best == nil || len(delta) < len(best.payload) {
			id := candidate.id
			best = &writeChoice{kind: KindRefDelta, payload: delta, base: id[:], depth: 1}
		}
	}
	return best
}

func encodeObjectHead(kind Kind, size int64) []byte {
	current := byte(kind)<<kindShift | byte(size)&sizeMask
	size >>= sizeBits
	var out []byte
	for size > 0 {
		out = append(out, current|continuation)
		current = byte(size) & payloadMask
		size >>= payloadBits
	}
	return append(out, current)
}

func encodeOffsetDelta(distance int64) []byte {
	var raw [maxObjectHeaderSize]byte
	last := len(raw) - 1
	raw[last] = byte(distance) & payloadMask
	for distance >>= payloadBits; distance > 0; distance >>= payloadBits {
		distance--
		last--
		raw[last] = continuation | byte(distance)&payloadMask
	}
	return raw[last:]
}

type packWriter struct {
	dst      io.Writer
	digest   stdhash.Hash
	sum      stdhash.Hash32
	summing  bool
	position int64
}

func newPackWriter(dst io.Writer) *packWriter {
	return &packWriter{dst: dst, digest: sha1.New(), sum: crc32.NewIEEE()}
}

func (w *packWriter) Write(chunk []byte) (int, error) {
	written, err := w.dst.Write(chunk)
	if written > 0 {
		_, _ = w.digest.Write(chunk[:written])
		if w.summing {
			_, _ = w.sum.Write(chunk[:written])
		}
		w.position += int64(written)
	}
	if err != nil {
		return written, fmt.Errorf("pack: write %d bytes at %d: %w", len(chunk), w.position, err)
	}
	return written, nil
}

func (w *packWriter) startSum() {
	w.sum.Reset()
	w.summing = true
}

func (w *packWriter) stopSum() uint32 {
	w.summing = false
	return w.sum.Sum32()
}

func (w *packWriter) writeHeader(count int) error {
	var head [headerSize]byte
	copy(head[:len(packMagic)], packMagic)
	binary.BigEndian.PutUint32(head[4:8], packVersion)
	binary.BigEndian.PutUint32(head[8:12], uint32(count))
	_, err := w.Write(head[:])
	return err
}

func (w *packWriter) writeObject(id hash.ObjectID, choice writeChoice) (Entry, error) {
	offset := w.position
	head := encodeObjectHead(choice.kind, int64(len(choice.payload)))
	head = append(head, choice.base...)
	w.startSum()
	if _, err := w.Write(head); err != nil {
		return Entry{}, err
	}
	deflate := zlib.NewWriter(w)
	if _, err := deflate.Write(choice.payload); err != nil {
		return Entry{}, err
	}
	if err := deflate.Close(); err != nil {
		return Entry{}, err
	}
	return Entry{ID: id, Offset: offset, CRC32: w.stopSum()}, nil
}

func (w *packWriter) finish() (hash.ObjectID, error) {
	var sum [hash.Size]byte
	w.digest.Sum(sum[:0])
	trailer := hash.ObjectID(sum)
	if _, err := w.dst.Write(trailer[:]); err != nil {
		return hash.Zero, fmt.Errorf("pack: write trailer: %w", err)
	}
	w.position += hash.Size
	return trailer, nil
}
