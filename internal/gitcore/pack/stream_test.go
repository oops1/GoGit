package pack

import (
	"bytes"
	"errors"
	"io"
	"os"
	"slices"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
)

type staticResolver struct {
	kind object.Type
	data []byte
}

func (r staticResolver) ResolveBase(hash.ObjectID, int) (object.Type, []byte, error) {
	return r.kind, r.data, nil
}

func appendDelta(content []byte) []byte {
	return slices.Concat(deltaSizes(int64(len(content)), int64(len(content))+1), copyOp(0, uint32(len(content))), insertOp([]byte("+")))
}

func TestPackInfoAtDescribesDeltasWithoutResolvingThem(t *testing.T) {
	content := bytes.Repeat([]byte("info line\n"), 30)
	external := bytes.Repeat([]byte("external base\n"), 10)
	builder := newPackBuilder()
	base := builder.addObject(t, KindBlob, content)
	offsetDelta := builder.addOffsetDelta(t, base, appendDelta(content))
	refDelta := builder.addRefDelta(t, hash.ObjectID{1}, appendDelta(content))
	externalDelta := builder.addRefDelta(t, hash.SumSHA1("tree", external), appendDelta(external))
	ids := []hash.ObjectID{{1}, {2}, {3}, {4}}
	raw, index := packIndexPair(t, builder, ids)
	mem := newMemoryPack(t, "info", raw, index)
	packfile, err := NewPack(mem.pack, int64(len(raw)), WithIndex(mem.file.Index), WithBaseResolver(staticResolver{kind: object.TypeTree, data: external}))
	if err != nil {
		t.Fatalf("NewPack returned error %v", err)
	}

	for _, tt := range []struct {
		offset int64
		kind   object.Type
		size   int
	}{
		{base, object.TypeBlob, len(content)},
		{offsetDelta, object.TypeBlob, len(content) + 1},
		{refDelta, object.TypeBlob, len(content) + 1},
		{externalDelta, object.TypeTree, len(external) + 1},
	} {
		kind, size, err := packfile.InfoAt(tt.offset)
		if err != nil || kind != tt.kind || size != int64(tt.size) {
			t.Fatalf("InfoAt(%d) = (%v, %d, %v), want (%v, %d)", tt.offset, kind, size, err, tt.kind, tt.size)
		}
	}
}

func TestPackInfoAtReportsBrokenDeltas(t *testing.T) {
	content := bytes.Repeat([]byte("broken info\n"), 30)
	builder := newPackBuilder()
	base := builder.addObject(t, KindBlob, content)
	notZlib := builder.add(KindOffsetDelta, 20, encodeBaseOffset(int64(len(builder.body))-base), []byte("not zlib at all"))
	truncated := builder.add(KindOffsetDelta, 20, encodeBaseOffset(int64(len(builder.body))-base), deflate(t, []byte{1, 1}))
	badSource := builder.add(KindOffsetDelta, 20, encodeBaseOffset(int64(len(builder.body))-base), deflate(t, bytes.Repeat([]byte{0x80}, 20)))
	badTarget := builder.add(KindOffsetDelta, 20, encodeBaseOffset(int64(len(builder.body))-base), deflate(t, append([]byte{1}, bytes.Repeat([]byte{0x80}, 19)...)))
	chained := builder.addOffsetDelta(t, base, appendDelta(content))
	deep := builder.addOffsetDelta(t, chained, appendDelta(append(bytes.Clone(content), '+')))
	garbage := builder.addRaw([]byte{0x50})
	onGarbage := builder.addOffsetDelta(t, garbage, deltaSizes(1, 1))
	unknownBase := builder.addRefDelta(t, hash.SumSHA1("blob", []byte("nowhere")), deltaSizes(1, 1))
	indexedBase := builder.addRefDelta(t, hash.ObjectID{1}, appendDelta(content))
	ids := make([]hash.ObjectID, len(builder.offsets))
	for i := range ids {
		ids[i] = hash.ObjectID{byte(i + 1)}
	}
	raw, index := packIndexPair(t, builder, ids)

	for _, tt := range []struct {
		name   string
		offset int64
		want   error
	}{
		{"an offset outside the pack", int64(len(raw)), ErrBadOffset},
		{"delta data that is not zlib", notZlib, ErrDecompress},
		{"delta data shorter than its sizes", truncated, ErrDecompress},
		{"a source size that never ends", badSource, ErrInvalidDelta},
		{"a target size that never ends", badTarget, ErrInvalidDelta},
		{"a chain deeper than allowed", deep, ErrDeltaChainTooDeep},
		{"a base with an unknown kind", onGarbage, ErrUnknownObjectKind},
		{"a base nobody has", unknownBase, ErrBaseNotFound},
	} {
		t.Run(tt.name, func(t *testing.T) {
			mem := newMemoryPack(t, "broken", raw, index)
			packfile, err := NewPack(mem.pack, int64(len(raw)), WithIndex(mem.file.Index), WithMaxDeltaDepth(1))
			if err != nil {
				t.Fatalf("NewPack returned error %v", err)
			}

			if _, _, err := packfile.InfoAt(tt.offset); !errors.Is(err, tt.want) {
				t.Fatalf("InfoAt returned %v, want %v", err, tt.want)
			}
		})
	}

	t.Run("an index it cannot read", func(t *testing.T) {
		mem := newMemoryPack(t, "broken", raw, index)
		mem.index.fail = func(size int, _ int64) bool { return size == hash.Size }
		if _, _, err := mem.file.Pack.InfoAt(indexedBase); !errors.Is(err, errReadInjected) {
			t.Fatalf("InfoAt returned %v, want %v", err, errReadInjected)
		}
	})
}

func streamFixture(t *testing.T) ([]byte, []int64, []byte) {
	t.Helper()
	content := bytes.Repeat([]byte("streamed content\n"), 400)
	builder := newPackBuilder()
	offsets := []int64{
		builder.addObject(t, KindBlob, content),
		0,
		builder.addRefDelta(t, hash.SumSHA1("blob", []byte("nowhere")), deltaSizes(1, 1)),
		builder.add(KindBlob, 1<<40, nil, deflate(t, []byte("x"))),
		builder.add(KindBlob, 5, nil, []byte("plain")),
		builder.add(KindBlob, 100, nil, deflate(t, []byte("ten bytes!"))),
		builder.add(KindBlob, 3, nil, deflate(t, []byte("abcdef"))),
	}
	offsets[1] = builder.addOffsetDelta(t, offsets[0], appendDelta(content))
	return builder.bytes(), offsets, content
}

func TestPackStreamAtReadsWholeObjectsAndResolvedDeltas(t *testing.T) {
	raw, offsets, content := streamFixture(t)
	packfile := openWrittenPack(t, raw)

	kind, size, reader, err := packfile.StreamAt(offsets[0])
	if err != nil || kind != object.TypeBlob || size != int64(len(content)) {
		t.Fatalf("StreamAt = (%v, %d, %v)", kind, size, err)
	}
	data, err := io.ReadAll(reader)
	if err != nil || !bytes.Equal(data, content) {
		t.Fatalf("ReadAll = (%d bytes, %v)", len(data), err)
	}
	if read, err := reader.Read(make([]byte, 1)); read != 0 || err != io.EOF {
		t.Fatalf("Read after the end = (%d, %v), want EOF", read, err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("Close returned error %v", err)
	}
	if _, err := reader.Read(make([]byte, 1)); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("Read after Close returned %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("a second Close returned error %v", err)
	}

	kind, size, reader, err = packfile.StreamAt(offsets[1])
	if err != nil || kind != object.TypeBlob || size != int64(len(content))+1 {
		t.Fatalf("StreamAt of a delta = (%v, %d, %v)", kind, size, err)
	}
	if data, err := io.ReadAll(reader); err != nil || !bytes.Equal(data, append(bytes.Clone(content), '+')) {
		t.Fatalf("ReadAll of a delta = (%d bytes, %v)", len(data), err)
	}
}

func TestPackStreamAtReportsBrokenObjects(t *testing.T) {
	raw, offsets, _ := streamFixture(t)
	packfile := openWrittenPack(t, raw)

	for _, tt := range []struct {
		name   string
		offset int64
		want   error
	}{
		{"an offset outside the pack", int64(len(raw)), ErrBadOffset},
		{"a delta without its base", offsets[2], ErrBaseNotFound},
		{"a size the pack cannot hold", offsets[3], ErrObjectTooLarge},
		{"data that is not zlib", offsets[4], ErrDecompress},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, _, _, err := packfile.StreamAt(tt.offset); !errors.Is(err, tt.want) {
				t.Fatalf("StreamAt returned %v, want %v", err, tt.want)
			}
		})
	}

	for _, tt := range []struct {
		name   string
		offset int64
		want   error
	}{
		{"a stream shorter than declared", offsets[5], ErrDecompress},
		{"a stream longer than declared", offsets[6], ErrSizeMismatch},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, _, reader, err := packfile.StreamAt(tt.offset)
			if err != nil {
				t.Fatalf("StreamAt returned error %v", err)
			}
			defer func() { _ = reader.Close() }()
			if _, err := io.ReadAll(reader); !errors.Is(err, tt.want) {
				t.Fatalf("ReadAll returned %v, want %v", err, tt.want)
			}
			if _, err := reader.Read(make([]byte, 1)); !errors.Is(err, tt.want) {
				t.Fatalf("a second Read returned %v, want %v", err, tt.want)
			}
		})
	}
}

func TestStoreStreamHoldsThePackUntilTheReaderCloses(t *testing.T) {
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

	kind, size, reader, ok, err := store.Stream(ids[0])
	want := source.objects[ids[0]]
	if err != nil || !ok || kind != want.kind || size != int64(len(want.data)) {
		t.Fatalf("Stream = (%v, %d, %v, %v)", kind, size, ok, err)
	}
	file := store.Files()[0]
	if users := file.users.Load(); users != 1 {
		t.Fatalf("the pack has %d users while streaming", users)
	}
	if data, err := io.ReadAll(reader); err != nil || !bytes.Equal(data, want.data) {
		t.Fatalf("ReadAll = (%d bytes, %v)", len(data), err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("Close returned error %v", err)
	}
	if err := reader.Close(); err != nil || file.users.Load() != 0 {
		t.Fatalf("a second Close = %v with %d users", err, file.users.Load())
	}
	if _, _, _, ok, err := store.Stream(hash.ObjectID{0xee}); ok || err != nil {
		t.Fatalf("Stream of an unknown object = (%v, %v)", ok, err)
	}
}

func TestStoreStreamReportsBrokenIndexesAndObjects(t *testing.T) {
	raw, offsets, _ := streamFixture(t)
	ids := []hash.ObjectID{{1}, {2}, {3}, {4}, {5}, {6}, {7}}
	entries := make([]Entry, len(ids))
	for i := range ids {
		entries[i] = Entry{ID: ids[i], Offset: offsets[i]}
	}
	index := buildIndex(entries, hash.ObjectID(raw[len(raw)-hash.Size:]))

	mem := newMemoryPack(t, "stream", raw, index)
	store := &Store{settings: newSettings(nil), files: []*PackFile{mem.file}}
	if _, _, _, _, err := store.Stream(ids[4]); !errors.Is(err, ErrDecompress) {
		t.Fatalf("Stream of data that is not zlib returned %v", err)
	}
	mem.index.fail = func(size int, _ int64) bool { return size == hash.Size }
	if _, _, _, _, err := store.Stream(ids[0]); !errors.Is(err, errReadInjected) {
		t.Fatalf("Stream with an unreadable index returned %v", err)
	}
}

func TestStoreForgetRetiresOnlyTheNamedPacks(t *testing.T) {
	raw, offsets, _ := streamFixture(t)
	index := buildIndex([]Entry{{ID: hash.ObjectID{1}, Offset: offsets[0]}}, hash.ObjectID(raw[len(raw)-hash.Size:]))
	kept, forgotten := newMemoryPack(t, "kept", raw, index), newMemoryPack(t, "forgotten", raw, index)
	store := &Store{settings: newSettings(nil), files: []*PackFile{kept.file, forgotten.file}}

	if err := store.Forget("forgotten", "absent"); err != nil {
		t.Fatalf("Forget returned error %v", err)
	}

	if files := store.Files(); len(files) != 1 || files[0] != kept.file || !forgotten.file.retired.Load() || kept.file.retired.Load() {
		t.Fatalf("files = %v", files)
	}
}
