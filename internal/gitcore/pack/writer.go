package pack

import (
	"cmp"
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

const (
	DefaultBigFileThreshold = 512 << 20
	copyBufferSize          = 1 << 16
)

type ObjectSource interface {
	Get(id hash.ObjectID) (object.Type, []byte, error)
}

type objectInfoSource interface {
	Info(id hash.ObjectID) (object.Type, int64, error)
}

type objectStreamSource interface {
	Stream(id hash.ObjectID) (object.Type, int64, io.ReadCloser, error)
}

type reuseSource interface {
	PackReuse() *Reuse
}

type WriteOptions struct {
	Window           int
	Depth            int
	Thin             map[hash.ObjectID]struct{}
	Progress         progress.Func
	NameHashes       map[hash.ObjectID]uint32
	BigFileThreshold int64
}

type WriteResult struct {
	Checksum hash.ObjectID
	Entries  []Entry
	Objects  int
	Bytes    int64
}

type writeState uint8

const (
	stateWaiting writeState = iota
	stateWriting
	stateWritten
)

type writeObject struct {
	id     hash.ObjectID
	kind   object.Type
	size   int64
	name   uint32
	data   []byte
	loaded bool
	index  map[uint32][]int
	stored *storedObject
	state  writeState
	offset int64
	depth  int
}

type writeChoice struct {
	kind    Kind
	payload []byte
	base    []byte
	depth   int
}

type thinBase struct {
	id    hash.ObjectID
	kind  object.Type
	data  []byte
	index map[uint32][]int
}

type packSession struct {
	ctx       context.Context
	src       ObjectSource
	opts      WriteOptions
	reuse     *Reuse
	objects   []writeObject
	positions map[hash.ObjectID]int
	thins     []thinBase
	window    []int
	writer    *packWriter
	entries   []Entry
}

func WritePack(ctx context.Context, dst io.Writer, src ObjectSource, ids []hash.ObjectID, opts WriteOptions) (WriteResult, error) {
	objects, err := describeObjects(src, ids, opts.NameHashes)
	if err != nil {
		return WriteResult{}, err
	}
	thins, err := fetchThinBases(src, opts.Thin)
	if err != nil {
		return WriteResult{}, err
	}
	if opts.BigFileThreshold <= 0 {
		opts.BigFileThreshold = DefaultBigFileThreshold
	}
	session := &packSession{ctx: ctx, src: src, opts: opts, objects: sortForDelta(objects), thins: thins, writer: newPackWriter(dst)}
	if reusing, ok := src.(reuseSource); ok {
		session.reuse = reusing.PackReuse()
	}
	defer session.reuse.Close()
	return session.run()
}

func describeObjects(src ObjectSource, ids []hash.ObjectID, names map[hash.ObjectID]uint32) ([]writeObject, error) {
	info, lazy := src.(objectInfoSource)
	objects := make([]writeObject, 0, len(ids))
	for _, id := range ids {
		obj := writeObject{id: id, name: names[id]}
		var err error
		if lazy {
			obj.kind, obj.size, err = info.Info(id)
		} else {
			obj.kind, obj.data, err = src.Get(id)
			obj.size, obj.loaded = int64(len(obj.data)), true
		}
		if err != nil {
			return nil, fmt.Errorf("pack: get %s: %w", id, err)
		}
		objects = append(objects, obj)
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
	slices.SortStableFunc(objects, func(a, b writeObject) int {
		return cmp.Or(cmp.Compare(b.kind, a.kind), cmp.Compare(b.name, a.name), cmp.Compare(b.size, a.size))
	})
	return objects
}

func (s *packSession) run() (WriteResult, error) {
	s.positions = make(map[hash.ObjectID]int, len(s.objects))
	for at, obj := range s.objects {
		s.positions[obj.id] = at
	}
	if err := s.writer.writeHeader(len(s.objects)); err != nil {
		return WriteResult{}, err
	}
	s.entries = make([]Entry, 0, len(s.objects))
	for at := range s.objects {
		if err := s.write(at); err != nil {
			return WriteResult{}, err
		}
	}
	checksum, err := s.writer.finish()
	if err != nil {
		return WriteResult{}, err
	}
	return WriteResult{Checksum: checksum, Entries: s.entries, Objects: len(s.objects), Bytes: s.writer.position}, nil
}

func (s *packSession) write(at int) error {
	obj := &s.objects[at]
	if obj.state != stateWaiting {
		return nil
	}
	if err := s.ctx.Err(); err != nil {
		return err
	}
	obj.state = stateWriting
	stored, err := s.reuse.find(obj.id)
	if err != nil {
		return err
	}
	obj.stored = stored
	if done, err := s.writeStoredDelta(at); done || err != nil {
		return err
	}
	if obj.size > s.opts.BigFileThreshold {
		return s.writeWhole(at, false)
	}
	choice, err := s.searchDelta(at)
	if err != nil {
		return err
	}
	if choice == nil {
		return s.writeWhole(at, true)
	}
	entry, err := s.writer.writeObject(obj.id, *choice)
	if err != nil {
		return err
	}
	s.finish(at, entry, choice.depth, true)
	return nil
}

func (s *packSession) writeStoredDelta(at int) (bool, error) {
	stored := s.objects[at].stored
	if stored == nil || !stored.head.Kind.IsDelta() || s.opts.Depth <= 0 {
		return false, nil
	}
	baseAt, inPack := s.positions[stored.base]
	if !inPack {
		if _, thin := s.opts.Thin[stored.base]; !thin {
			return false, nil
		}
		return s.copyStored(at, KindRefDelta, stored.base[:], 1, false)
	}
	if err := s.write(baseAt); err != nil {
		return false, err
	}
	base := &s.objects[baseAt]
	if base.state != stateWritten || base.depth >= s.opts.Depth {
		return false, nil
	}
	return s.copyStored(at, KindOffsetDelta, encodeOffsetDelta(s.writer.position-base.offset), base.depth+1, false)
}

func (s *packSession) writeWhole(at int, searchable bool) error {
	obj := &s.objects[at]
	if obj.stored != nil && !obj.stored.head.Kind.IsDelta() {
		if done, err := s.copyStored(at, obj.stored.head.Kind, nil, 0, searchable); done || err != nil {
			return err
		}
	}
	if streamer, streams := s.src.(objectStreamSource); streams && !searchable && !obj.loaded {
		return s.writeStream(at, streamer)
	}
	if err := s.load(obj); err != nil {
		return err
	}
	entry, err := s.writer.writeObject(obj.id, writeChoice{kind: Kind(obj.kind), payload: obj.data})
	if err != nil {
		return err
	}
	s.finish(at, entry, 0, searchable)
	return nil
}

func (s *packSession) copyStored(at int, kind Kind, base []byte, depth int, searchable bool) (bool, error) {
	obj := &s.objects[at]
	raw, intact, err := s.writer.readStored(obj.stored)
	if err != nil || !intact {
		return false, err
	}
	entry, err := s.writer.writeStored(obj.id, kind, base, raw, obj.stored)
	if err != nil {
		return false, err
	}
	s.finish(at, entry, depth, searchable)
	return true, nil
}

func (s *packSession) writeStream(at int, streamer objectStreamSource) error {
	obj := &s.objects[at]
	kind, size, reader, err := streamer.Stream(obj.id)
	if err != nil {
		return fmt.Errorf("pack: get %s: %w", obj.id, err)
	}
	defer func() { _ = reader.Close() }()
	entry, err := s.writer.writeStream(obj.id, Kind(kind), size, reader)
	if err != nil {
		return err
	}
	s.finish(at, entry, 0, false)
	return nil
}

func (s *packSession) load(obj *writeObject) error {
	if obj.loaded {
		return nil
	}
	_, data, err := s.src.Get(obj.id)
	if err != nil {
		return fmt.Errorf("pack: get %s: %w", obj.id, err)
	}
	obj.data, obj.loaded = data, true
	return nil
}

func (s *packSession) finish(at int, entry Entry, depth int, searchable bool) {
	obj := &s.objects[at]
	obj.state, obj.offset, obj.depth = stateWritten, entry.Offset, depth
	s.entries = append(s.entries, entry)
	done, total := int64(len(s.entries)), int64(len(s.objects))
	s.opts.Progress.Count(progress.PhaseCompressing, done, total)
	s.remember(at, searchable)
	s.opts.Progress.Count(progress.PhaseWriting, done, total)
}

func (s *packSession) remember(at int, searchable bool) {
	if !searchable || s.opts.Window <= 0 {
		s.forget(at)
		return
	}
	s.window = append(s.window, at)
	if len(s.window) > s.opts.Window {
		s.forget(s.window[0])
		s.window = slices.Delete(s.window, 0, 1)
	}
}

func (s *packSession) forget(at int) {
	obj := &s.objects[at]
	obj.data, obj.index, obj.loaded = nil, nil, false
}

func (s *packSession) searchDelta(at int) (*writeChoice, error) {
	if s.opts.Window <= 0 || s.opts.Depth <= 0 {
		return nil, nil
	}
	obj := &s.objects[at]
	var best *writeChoice
	for i := len(s.window) - 1; i >= 0; i-- {
		candidate := &s.objects[s.window[i]]
		limit := s.deltaLimit(obj, candidate.size, candidate.depth, best)
		if limit <= 0 || !pairable(obj, candidate) {
			continue
		}
		if err := s.load(obj); err != nil {
			return nil, err
		}
		if err := s.load(candidate); err != nil {
			return nil, err
		}
		if candidate.index == nil {
			candidate.index = buildDeltaIndex(candidate.data)
		}
		if delta := encodeDelta(candidate.index, candidate.data, obj.data, limit); delta != nil {
			best = &writeChoice{kind: KindOffsetDelta, payload: delta, base: encodeOffsetDelta(s.writer.position - candidate.offset), depth: candidate.depth + 1}
		}
	}
	for i := range s.thins {
		thin := &s.thins[i]
		limit := s.deltaLimit(obj, int64(len(thin.data)), 0, best)
		if limit <= 0 || thin.kind != obj.kind {
			continue
		}
		if err := s.load(obj); err != nil {
			return nil, err
		}
		if thin.index == nil {
			thin.index = buildDeltaIndex(thin.data)
		}
		if delta := encodeDelta(thin.index, thin.data, obj.data, limit); delta != nil {
			id := thin.id
			best = &writeChoice{kind: KindRefDelta, payload: delta, base: id[:], depth: 1}
		}
	}
	return best, nil
}

func (s *packSession) deltaLimit(target *writeObject, baseSize int64, baseDepth int, best *writeChoice) int64 {
	if baseDepth >= s.opts.Depth {
		return 0
	}
	limit, depth := target.size/2-hash.Size, 1
	if best != nil {
		limit, depth = int64(len(best.payload))-1, best.depth
	}
	limit = limit * int64(s.opts.Depth-baseDepth) / int64(s.opts.Depth-depth+1)
	if limit <= 0 || baseSize < target.size && target.size-baseSize >= limit || target.size < baseSize/32 {
		return 0
	}
	return limit
}

func pairable(target, candidate *writeObject) bool {
	if candidate.kind != target.kind {
		return false
	}
	stored := target.stored
	return stored == nil || stored.head.Kind.IsDelta() || candidate.stored == nil || candidate.stored.file != stored.file
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
	deflate  *zlib.Writer
	buffer   []byte
}

func newPackWriter(dst io.Writer) *packWriter {
	w := &packWriter{dst: dst, digest: sha1.New(), sum: crc32.NewIEEE(), buffer: make([]byte, copyBufferSize)}
	w.deflate = zlib.NewWriter(w)
	return w
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
	w.deflate.Reset(w)
	if _, err := w.deflate.Write(choice.payload); err != nil {
		return Entry{}, err
	}
	if err := w.deflate.Close(); err != nil {
		return Entry{}, err
	}
	return Entry{ID: id, Offset: offset, CRC32: w.stopSum()}, nil
}

func (w *packWriter) writeStream(id hash.ObjectID, kind Kind, size int64, source io.Reader) (Entry, error) {
	offset := w.position
	w.startSum()
	if _, err := w.Write(encodeObjectHead(kind, size)); err != nil {
		return Entry{}, err
	}
	w.deflate.Reset(w)
	copied, err := io.CopyBuffer(w.deflate, io.LimitReader(source, size+1), w.buffer)
	if err != nil {
		return Entry{}, fmt.Errorf("pack: compress %s: %w", id, err)
	}
	if copied != size {
		return Entry{}, fmt.Errorf("%w: %s declares %d bytes and streams %d", ErrSizeMismatch, id, size, copied)
	}
	if err := w.deflate.Close(); err != nil {
		return Entry{}, err
	}
	return Entry{ID: id, Offset: offset, CRC32: w.stopSum()}, nil
}

func (w *packWriter) readStored(stored *storedObject) ([]byte, bool, error) {
	length := stored.end - stored.head.Offset
	section := io.NewSectionReader(stored.file.Pack.source, stored.head.Offset, length)
	if length <= int64(len(w.buffer)) {
		raw := w.buffer[:length]
		if _, err := io.ReadFull(section, raw); err != nil {
			return nil, false, fmt.Errorf("pack: read the object at %d of %s: %w", stored.head.Offset, stored.file.Name, err)
		}
		return raw[stored.head.DataOffset-stored.head.Offset:], crc32.ChecksumIEEE(raw) == stored.crc, nil
	}
	sum := crc32.NewIEEE()
	if _, err := io.CopyBuffer(sum, section, w.buffer); err != nil {
		return nil, false, fmt.Errorf("pack: read the object at %d of %s: %w", stored.head.Offset, stored.file.Name, err)
	}
	return nil, sum.Sum32() == stored.crc, nil
}

func (w *packWriter) writeStored(id hash.ObjectID, kind Kind, base, raw []byte, stored *storedObject) (Entry, error) {
	offset := w.position
	w.startSum()
	if _, err := w.Write(append(encodeObjectHead(kind, stored.head.Size), base...)); err != nil {
		return Entry{}, err
	}
	if raw != nil {
		if _, err := w.Write(raw); err != nil {
			return Entry{}, err
		}
		return Entry{ID: id, Offset: offset, CRC32: w.stopSum()}, nil
	}
	data := io.NewSectionReader(stored.file.Pack.source, stored.head.DataOffset, stored.end-stored.head.DataOffset)
	if _, err := io.CopyBuffer(w, data, w.buffer); err != nil {
		return Entry{}, fmt.Errorf("pack: copy the object at %d of %s: %w", stored.head.Offset, stored.file.Name, err)
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
