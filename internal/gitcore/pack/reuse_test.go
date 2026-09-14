package pack

import (
	"bytes"
	"crypto/rand"
	"errors"
	"io"
	"slices"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
)

var errReadInjected = errors.New("injected read failure")

type failingReaderAt struct {
	source io.ReaderAt
	fail   func(size int, offset int64) bool
}

func (r *failingReaderAt) ReadAt(into []byte, offset int64) (int, error) {
	if r.fail != nil && r.fail(len(into), offset) {
		return 0, errReadInjected
	}
	return r.source.ReadAt(into, offset)
}

type memoryPack struct {
	pack  *failingReaderAt
	index *failingReaderAt
	file  *PackFile
}

func newMemoryPack(t testing.TB, name string, raw, indexBytes []byte) *memoryPack {
	t.Helper()
	mem := &memoryPack{pack: &failingReaderAt{source: bytes.NewReader(raw)}, index: &failingReaderAt{source: bytes.NewReader(indexBytes)}}
	index, err := NewIndex(mem.index, int64(len(indexBytes)))
	if err != nil {
		t.Fatalf("NewIndex returned error %v", err)
	}
	packfile, err := NewPack(mem.pack, int64(len(raw)), WithIndex(index))
	if err != nil {
		t.Fatalf("NewPack returned error %v", err)
	}
	mem.file = &PackFile{Name: name, Index: index, Pack: packfile}
	return mem
}

func memoryReuse(packs ...*memoryPack) *Reuse {
	reuse := &Reuse{reverse: make(map[*PackFile]*reverseIndex)}
	for _, mem := range packs {
		reuse.files = append(reuse.files, mem.file)
	}
	return reuse
}

func writtenPackPair(t testing.TB, source ObjectSource, ids []hash.ObjectID, opts WriteOptions) ([]byte, []byte, WriteResult) {
	t.Helper()
	var raw bytes.Buffer
	result, err := WritePack(t.Context(), &raw, source, ids, opts)
	if err != nil {
		t.Fatalf("WritePack returned error %v", err)
	}
	return raw.Bytes(), requireWriteIndex(t, result.Entries, result.Checksum), result
}

type reusingSource struct {
	*describingSource
	packs []*memoryPack
	reuse *Reuse
}

func newReusingSource(plain *fakeSource, packs ...*memoryPack) *reusingSource {
	return &reusingSource{describingSource: newDescribingSource(plain), packs: packs}
}

func (s *reusingSource) PackReuse() *Reuse {
	s.reuse = memoryReuse(s.packs...)
	return s.reuse
}

func entryOf(t testing.TB, entries []Entry, id hash.ObjectID) Entry {
	t.Helper()
	for _, entry := range entries {
		if entry.ID == id {
			return entry
		}
	}
	t.Fatalf("%s is not in the pack", id)
	return Entry{}
}

func headerOf(t testing.TB, packBytes []byte, entries []Entry, id hash.ObjectID) ObjectHeader {
	t.Helper()
	head, err := openWrittenPack(t, packBytes).HeaderAt(entryOf(t, entries, id).Offset)
	if err != nil {
		t.Fatalf("HeaderAt returned error %v", err)
	}
	return head
}

func requireReadBack(t testing.TB, packBytes []byte, entries []Entry, source *fakeSource) {
	t.Helper()
	packfile := openWrittenPack(t, packBytes)
	for _, entry := range entries {
		kind, data, err := packfile.ObjectAt(entry.Offset)
		want := source.objects[entry.ID]
		if err != nil || kind != want.kind || !bytes.Equal(data, want.data) {
			t.Fatalf("%s read back as (%v, %d bytes, %v)", entry.ID, kind, len(data), err)
		}
	}
}

func deltaPair(source *fakeSource) (hash.ObjectID, hash.ObjectID) {
	base := similarBlob(0, 300)
	big := source.add(object.TypeBlob, append(bytes.Clone(base), "a tail only the larger blob has\n"...))
	small := source.add(object.TypeBlob, base)
	return big, small
}

func TestWritePackRewritesAPackItAlreadyHoldsByteForByteWithoutReadingObjects(t *testing.T) {
	plain, ids := similarPackSource()
	opts := WriteOptions{Window: 10, Depth: 50}
	raw, index, first := writtenPackPair(t, plain, ids, opts)
	source := newReusingSource(plain, newMemoryPack(t, "old", raw, index))

	var again bytes.Buffer
	result, err := WritePack(t.Context(), &again, source, ids, opts)
	if err != nil {
		t.Fatalf("WritePack returned error %v", err)
	}

	if !bytes.Equal(again.Bytes(), raw) || result.Checksum != first.Checksum {
		t.Fatalf("the rewritten pack differs: %s, want %s", result.Checksum, first.Checksum)
	}
	for id, reads := range source.gets {
		if reads != 0 {
			t.Fatalf("%s was read %d times", id, reads)
		}
	}
	deltas := 0
	for _, entry := range result.Entries {
		if headerOf(t, again.Bytes(), result.Entries, entry.ID).Kind.IsDelta() {
			deltas++
		}
	}
	if deltas == 0 {
		t.Fatal("the pack holds no delta, so no delta was reused")
	}
}

func TestWritePackWritesTheBaseOfAReusedDeltaFirst(t *testing.T) {
	plain := newFakeSource()
	big, small := deltaPair(plain)
	opts := WriteOptions{Window: 10, Depth: 50}
	raw, index, _ := writtenPackPair(t, plain, []hash.ObjectID{big, small}, opts)
	source := newReusingSource(plain, newMemoryPack(t, "old", raw, index))
	opts.NameHashes = map[hash.ObjectID]uint32{small: 2, big: 1}

	var out bytes.Buffer
	result, err := WritePack(t.Context(), &out, source, []hash.ObjectID{small, big}, opts)
	if err != nil {
		t.Fatalf("WritePack returned error %v", err)
	}

	head := headerOf(t, out.Bytes(), result.Entries, small)
	if result.Entries[0].ID != big || head.Kind != KindOffsetDelta || head.BaseOffset != result.Entries[0].Offset {
		t.Fatalf("entries = %+v, delta header = %+v", result.Entries, head)
	}
	if source.gets[small]+source.gets[big] != 0 {
		t.Fatalf("objects were read: %v", source.gets)
	}
	requireReadBack(t, out.Bytes(), result.Entries, plain)
}

func TestWritePackRecompressesAStoredDeltaWhoseBaseStaysOut(t *testing.T) {
	plain := newFakeSource()
	big, small := deltaPair(plain)
	raw, index, _ := writtenPackPair(t, plain, []hash.ObjectID{big, small}, WriteOptions{Window: 10, Depth: 50})
	source := newReusingSource(plain, newMemoryPack(t, "old", raw, index))

	var out bytes.Buffer
	result, err := WritePack(t.Context(), &out, source, []hash.ObjectID{small}, WriteOptions{Window: 10, Depth: 50})
	if err != nil {
		t.Fatalf("WritePack returned error %v", err)
	}

	if head := headerOf(t, out.Bytes(), result.Entries, small); head.Kind != KindBlob || source.gets[small] != 1 {
		t.Fatalf("header = %+v, reads = %v", head, source.gets)
	}
	requireReadBack(t, out.Bytes(), result.Entries, plain)
}

func TestWritePackKeepsAStoredDeltaAgainstAThinBase(t *testing.T) {
	plain := newFakeSource()
	big, small := deltaPair(plain)
	raw, index, _ := writtenPackPair(t, plain, []hash.ObjectID{big, small}, WriteOptions{Window: 10, Depth: 50})
	source := newReusingSource(plain, newMemoryPack(t, "old", raw, index))

	var out bytes.Buffer
	result, err := WritePack(t.Context(), &out, source, []hash.ObjectID{small}, WriteOptions{Window: 10, Depth: 50, Thin: map[hash.ObjectID]struct{}{big: {}}})
	if err != nil {
		t.Fatalf("WritePack returned error %v", err)
	}

	if head := headerOf(t, out.Bytes(), result.Entries, small); head.Kind != KindRefDelta || head.BaseID != big || source.gets[small] != 0 {
		t.Fatalf("header = %+v, reads = %v", head, source.gets)
	}
}

func TestWritePackBreaksStoredDeltaChainsDeeperThanTheDepth(t *testing.T) {
	plain := newFakeSource()
	content := similarBlob(0, 300)
	top := plain.add(object.TypeBlob, append(bytes.Clone(content), "second tail\nfirst tail\n"...))
	middle := plain.add(object.TypeBlob, append(bytes.Clone(content), "first tail\n"...))
	bottom := plain.add(object.TypeBlob, content)
	ids := []hash.ObjectID{top, middle, bottom}
	raw, index, first := writtenPackPair(t, plain, ids, WriteOptions{Window: 1, Depth: 50})
	if head := headerOf(t, raw, first.Entries, bottom); head.BaseOffset != entryOf(t, first.Entries, middle).Offset {
		t.Fatalf("the old pack holds no chain: %+v", head)
	}
	source := newReusingSource(plain, newMemoryPack(t, "old", raw, index))

	var out bytes.Buffer
	result, err := WritePack(t.Context(), &out, source, ids, WriteOptions{Window: 1, Depth: 1})
	if err != nil {
		t.Fatalf("WritePack returned error %v", err)
	}

	topOffset := entryOf(t, result.Entries, top).Offset
	middleHead, bottomHead := headerOf(t, out.Bytes(), result.Entries, middle), headerOf(t, out.Bytes(), result.Entries, bottom)
	if middleHead.BaseOffset != topOffset || bottomHead.BaseOffset != topOffset || source.gets[middle] != 0 {
		t.Fatalf("middle = %+v, bottom = %+v, top at %d, reads = %v", middleHead, bottomHead, topOffset, source.gets)
	}
	requireReadBack(t, out.Bytes(), result.Entries, plain)
}

func TestWritePackRecompressesAStoredObjectWhoseChecksumDoesNotMatch(t *testing.T) {
	plain, ids := similarPackSource()
	opts := WriteOptions{Window: 10, Depth: 50}
	raw, _, first := writtenPackPair(t, plain, ids, opts)
	tree := ids[len(ids)-1]
	entries := slices.Clone(first.Entries)
	for i := range entries {
		if entries[i].ID == tree {
			entries[i].CRC32++
		}
	}
	source := newReusingSource(plain, newMemoryPack(t, "old", raw, requireWriteIndex(t, entries, first.Checksum)))

	var out bytes.Buffer
	result, err := WritePack(t.Context(), &out, source, ids, opts)
	if err != nil {
		t.Fatalf("WritePack returned error %v", err)
	}

	if source.gets[tree] != 1 {
		t.Fatalf("the tree with a wrong checksum was read %d times", source.gets[tree])
	}
	requireReadBack(t, out.Bytes(), result.Entries, plain)
}

func TestWritePackIgnoresPacksWhoseIndexHasNoChecksums(t *testing.T) {
	plain, ids := similarPackSource()
	opts := WriteOptions{Window: 10, Depth: 50}
	raw, index, _ := writtenPackPair(t, plain, ids, opts)
	mem := newMemoryPack(t, "old", raw, index)
	mem.file.Index.version = indexVersionOne
	source := newReusingSource(plain, mem)

	if _, err := WritePack(t.Context(), &bytes.Buffer{}, source, ids, opts); err != nil {
		t.Fatalf("WritePack returned error %v", err)
	}

	for _, id := range ids {
		if source.gets[id] == 0 {
			t.Fatalf("%s was copied from a pack without checksums", id)
		}
	}
}

func TestWritePackReportsAStoredObjectItCannotRead(t *testing.T) {
	plain := newFakeSource()
	noise := make([]byte, 3*copyBufferSize)
	if _, err := rand.Read(noise); err != nil {
		t.Fatalf("rand.Read returned error %v", err)
	}
	large := plain.add(object.TypeBlob, noise)
	small := plain.add(object.TypeTree, []byte("100644 blob deadbeef\tfile.txt\n"))
	raw, index, first := writtenPackPair(t, plain, []hash.ObjectID{large, small}, WriteOptions{})
	for _, tt := range []struct {
		name string
		id   hash.ObjectID
		fail func(head ObjectHeader) func(int, int64) bool
	}{
		{"a small object", small, func(head ObjectHeader) func(int, int64) bool {
			return func(size int, offset int64) bool { return offset == head.Offset && size != maxObjectHeaderSize }
		}},
		{"a large object while checking it", large, func(head ObjectHeader) func(int, int64) bool {
			return func(size int, offset int64) bool { return offset == head.Offset && size == copyBufferSize }
		}},
		{"a large object while copying it", large, func(head ObjectHeader) func(int, int64) bool {
			return func(_ int, offset int64) bool { return offset == head.DataOffset }
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			mem := newMemoryPack(t, "old", raw, index)
			head, err := mem.file.Pack.HeaderAt(entryOf(t, first.Entries, tt.id).Offset)
			if err != nil {
				t.Fatalf("HeaderAt returned error %v", err)
			}
			mem.pack.fail = tt.fail(head)

			_, err = WritePack(t.Context(), &bytes.Buffer{}, newReusingSource(plain, mem), []hash.ObjectID{tt.id}, WriteOptions{})

			if !errors.Is(err, errReadInjected) {
				t.Fatalf("WritePack returned %v, want %v", err, errReadInjected)
			}
		})
	}
}

func TestWritePackResolvesStoredDeltasThatReferToEachOther(t *testing.T) {
	plain := newFakeSource()
	base := bytes.Repeat([]byte("cycle base line\n"), 40)
	grown := append(bytes.Clone(base), "grown\n"...)
	baseID, grownID := plain.add(object.TypeBlob, base), plain.add(object.TypeBlob, grown)
	first := newPackBuilder()
	first.addRefDelta(t, baseID, slices.Concat(deltaSizes(int64(len(base)), int64(len(grown))), copyOp(0, uint32(len(base))), insertOp([]byte("grown\n"))))
	firstRaw, firstIndex := packIndexPair(t, first, []hash.ObjectID{grownID})
	second := newPackBuilder()
	second.addRefDelta(t, grownID, slices.Concat(deltaSizes(int64(len(grown)), int64(len(base))), copyOp(0, uint32(len(base)))))
	secondRaw, secondIndex := packIndexPair(t, second, []hash.ObjectID{baseID})
	source := newReusingSource(plain, newMemoryPack(t, "first", firstRaw, firstIndex), newMemoryPack(t, "second", secondRaw, secondIndex))

	var out bytes.Buffer
	result, err := WritePack(t.Context(), &out, source, []hash.ObjectID{grownID, baseID}, WriteOptions{Window: 10, Depth: 50})
	if err != nil {
		t.Fatalf("WritePack returned error %v", err)
	}

	if head := headerOf(t, out.Bytes(), result.Entries, grownID); head.Kind != KindOffsetDelta || source.gets[grownID] != 0 || source.gets[baseID] != 1 {
		t.Fatalf("header = %+v, reads = %v", head, source.gets)
	}
	requireReadBack(t, out.Bytes(), result.Entries, plain)
}

type findFixture struct {
	raw, index     []byte
	baseID, target hash.ObjectID
}

func newFindFixture(t *testing.T) findFixture {
	t.Helper()
	content := bytes.Repeat([]byte("reuse find content\n"), 20)
	target := append(bytes.Clone(content), 'y')
	builder := newPackBuilder()
	baseOffset := builder.addObject(t, KindBlob, content)
	builder.addOffsetDelta(t, baseOffset, slices.Concat(deltaSizes(int64(len(content)), int64(len(target))), copyOp(0, uint32(len(content))), insertOp([]byte("y"))))
	fixture := findFixture{baseID: hash.SumSHA1("blob", content), target: hash.SumSHA1("blob", target)}
	if fixture.baseID[0] == fixture.target[0] {
		t.Fatal("the fixture objects share a fanout bucket")
	}
	fixture.raw, fixture.index = packIndexPair(t, builder, []hash.ObjectID{fixture.baseID, fixture.target})
	return fixture
}

func TestReuseFindLocatesAStoredDeltaAndItsBase(t *testing.T) {
	fixture := newFindFixture(t)
	reuse := memoryReuse(newMemoryPack(t, "old", fixture.raw, fixture.index))

	stored, err := reuse.find(fixture.target)
	if err != nil || stored == nil || stored.base != fixture.baseID || stored.end != int64(len(fixture.raw)-hash.Size) || stored.head.Kind != KindOffsetDelta {
		t.Fatalf("find = (%+v, %v)", stored, err)
	}
	if missing, err := reuse.find(hash.ObjectID{0xee}); missing != nil || err != nil {
		t.Fatalf("find of an unknown object = (%+v, %v)", missing, err)
	}
	var none *Reuse
	if stored, err := none.find(fixture.target); stored != nil || err != nil {
		t.Fatalf("find without packs = (%+v, %v)", stored, err)
	}
	none.Close()
}

func TestReuseFindReportsWhatItCannotRead(t *testing.T) {
	fixture := newFindFixture(t)
	for _, tt := range []struct {
		name  string
		setup func(mem *memoryPack, reuse *Reuse)
	}{
		{"the names of the index", func(mem *memoryPack, _ *Reuse) {
			mem.index.fail = func(size int, _ int64) bool { return size == hash.Size }
		}},
		{"the checksums of the index", func(mem *memoryPack, _ *Reuse) {
			crcs, offsets := mem.file.Index.crcs, mem.file.Index.offsets
			mem.index.fail = func(_ int, offset int64) bool { return offset >= crcs && offset < offsets }
		}},
		{"the object header", func(mem *memoryPack, _ *Reuse) {
			mem.pack.fail = func(size int, _ int64) bool { return size == maxObjectHeaderSize }
		}},
		{"the offsets of the index", func(mem *memoryPack, _ *Reuse) {
			offsets := mem.file.Index.offsets
			mem.index.fail = func(size int, offset int64) bool { return size == 2*offsetSize && offset == offsets }
		}},
		{"the name of the delta base", func(mem *memoryPack, reuse *Reuse) {
			position, _, _ := mem.file.Index.Position(fixture.baseID)
			at := mem.file.Index.names + int64(position)*hash.Size
			mem.index.fail = func(_ int, offset int64) bool { return offset == at && len(reuse.reverse) > 0 }
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			mem := newMemoryPack(t, "old", fixture.raw, fixture.index)
			reuse := memoryReuse(mem)
			tt.setup(mem, reuse)

			if _, err := reuse.find(fixture.target); !errors.Is(err, errReadInjected) {
				t.Fatalf("find returned %v, want %v", err, errReadInjected)
			}
		})
	}
}

func TestReuseFindRejectsOffsetsThatDoNotStartObjects(t *testing.T) {
	content := bytes.Repeat([]byte("offset fixture\n"), 30)
	stray := newPackBuilder()
	strayBase := stray.addObject(t, KindBlob, content)
	stray.add(KindOffsetDelta, 4, encodeBaseOffset(int64(len(stray.body))-strayBase-1), deflate(t, deltaSizes(1, 1)))
	strayID := hash.SumSHA1("blob", []byte("stray delta"))
	strayRaw, strayIndex := packIndexPair(t, stray, []hash.ObjectID{hash.SumSHA1("blob", content), strayID})

	overlap := newPackBuilder()
	overlapOffset := overlap.addObject(t, KindBlob, content)
	overlapRaw := overlap.bytes()
	overlapID := hash.SumSHA1("blob", content)
	overlapIndex := buildIndex([]Entry{
		{ID: overlapID, Offset: overlapOffset, CRC32: overlap.sums[0]},
		{ID: hash.SumSHA1("blob", []byte("inside the header")), Offset: overlapOffset + 1},
	}, hash.ObjectID(overlapRaw[len(overlapRaw)-hash.Size:]))

	for _, tt := range []struct {
		name       string
		raw, index []byte
		id         hash.ObjectID
	}{
		{"a delta base inside another object", strayRaw, strayIndex, strayID},
		{"an object that overlaps the next one", overlapRaw, overlapIndex, overlapID},
	} {
		t.Run(tt.name, func(t *testing.T) {
			reuse := memoryReuse(newMemoryPack(t, "old", tt.raw, tt.index))

			if _, err := reuse.find(tt.id); !errors.Is(err, ErrBadOffset) {
				t.Fatalf("find returned %v, want %v", err, ErrBadOffset)
			}
		})
	}
}

func TestOffsetTableReadsLargeOffsetsAndReportsReadFailures(t *testing.T) {
	raw := buildIndex([]Entry{{ID: hash.ObjectID{1}, Offset: 12}, {ID: hash.ObjectID{2}, Offset: 1 << 33}}, hash.Zero)
	reader := &failingReaderAt{source: bytes.NewReader(raw)}
	index, err := NewIndex(reader, int64(len(raw)))
	if err != nil {
		t.Fatalf("NewIndex returned error %v", err)
	}

	if offsets, err := index.offsetTable(); err != nil || !slices.Equal(offsets, []int64{12, 1 << 33}) {
		t.Fatalf("offsetTable = (%v, %v)", offsets, err)
	}
	for _, at := range []int64{index.larges, index.offsets} {
		reader.fail = func(_ int, offset int64) bool { return offset == at }
		if _, err := index.offsetTable(); !errors.Is(err, errReadInjected) {
			t.Fatalf("offsetTable with a failing read at %d returned %v", at, err)
		}
	}
}

func TestNewReuseHoldsTheFilesOfAStoreUntilItCloses(t *testing.T) {
	source, ids := writeFileFixture()
	dir := t.TempDir()
	if _, err := WritePackFile(t.Context(), dir, source, ids, WriteOptions{}); err != nil {
		t.Fatalf("WritePackFile returned error %v", err)
	}
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("Open returned error %v", err)
	}
	defer func() { _ = store.Close() }()

	reuse := NewReuse(store)
	file := store.Files()[0]
	if stored, err := reuse.find(ids[0]); err != nil || stored == nil || file.users.Load() != 1 {
		t.Fatalf("find = (%+v, %v) with %d users", stored, err, file.users.Load())
	}
	reuse.Close()

	if users := file.users.Load(); users != 0 {
		t.Fatalf("the pack keeps %d users after Close", users)
	}
}

func TestWritePackCopiesALargeStoredObjectWholesale(t *testing.T) {
	plain := newFakeSource()
	noise := make([]byte, 3*copyBufferSize)
	if _, err := rand.Read(noise); err != nil {
		t.Fatalf("rand.Read returned error %v", err)
	}
	large := plain.add(object.TypeBlob, noise)
	raw, index, _ := writtenPackPair(t, plain, []hash.ObjectID{large}, WriteOptions{})
	source := newReusingSource(plain, newMemoryPack(t, "old", raw, index))

	var out bytes.Buffer
	if _, err := WritePack(t.Context(), &out, source, []hash.ObjectID{large}, WriteOptions{}); err != nil {
		t.Fatalf("WritePack returned error %v", err)
	}

	if !bytes.Equal(out.Bytes(), raw) || source.gets[large] != 0 {
		t.Fatalf("identical = %v, reads = %v", bytes.Equal(out.Bytes(), raw), source.gets)
	}
}

func TestWritePackReportsFailuresAroundReusedAndSearchedDeltas(t *testing.T) {
	plain := newFakeSource()
	big, small := deltaPair(plain)
	opts := WriteOptions{Window: 10, Depth: 50}
	raw, index, first := writtenPackPair(t, plain, []hash.ObjectID{big, small}, opts)

	t.Run("an index it cannot search", func(t *testing.T) {
		mem := newMemoryPack(t, "old", raw, index)
		mem.index.fail = func(size int, _ int64) bool { return size == hash.Size }
		if _, err := WritePack(t.Context(), &bytes.Buffer{}, newReusingSource(plain, mem), []hash.ObjectID{big, small}, opts); !errors.Is(err, errReadInjected) {
			t.Fatalf("WritePack returned %v, want %v", err, errReadInjected)
		}
	})
	t.Run("a base written ahead of its delta", func(t *testing.T) {
		reordered := opts
		reordered.NameHashes = map[hash.ObjectID]uint32{small: 2, big: 1}
		source := newReusingSource(plain, newMemoryPack(t, "old", raw, index))
		if _, err := WritePack(t.Context(), &limitedWriter{limit: headerSize}, source, []hash.ObjectID{small, big}, reordered); !errors.Is(err, errWriteLimitReached) {
			t.Fatalf("WritePack returned %v, want %v", err, errWriteLimitReached)
		}
	})
	t.Run("a delta found by the search", func(t *testing.T) {
		limit := &limitedWriter{limit: entryOf(t, first.Entries, small).Offset}
		if _, err := WritePack(t.Context(), limit, plain, []hash.ObjectID{big, small}, opts); !errors.Is(err, errWriteLimitReached) {
			t.Fatalf("WritePack returned %v, want %v", err, errWriteLimitReached)
		}
	})
}
