package pack

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
)

func packOf(t *testing.T, raw []byte, opts ...Option) *Pack {
	t.Helper()
	packfile, err := NewPack(bytes.NewReader(raw), int64(len(raw)), opts...)
	if err != nil {
		t.Fatalf("NewPack returned error %v", err)
	}
	return packfile
}

func chainBuilder(t *testing.T, links int) (*packBuilder, int64) {
	t.Helper()
	builder := newPackBuilder()
	base := []byte("chain base content that the deltas keep copying from")
	offset := builder.addObject(t, KindBlob, base)
	size := int64(len(base))
	for step := range links {
		tail := insertOp([]byte{byte('a' + step)})
		delta := slices.Concat(deltaSizes(size, size+1), copyOp(0, uint32(size)), tail)
		size++
		offset = builder.addOffsetDelta(t, offset, delta)
	}
	return builder, offset
}

func TestOpenPackReadsEveryFixture(t *testing.T) {
	for _, name := range fixtureNames(t) {
		t.Run(name, func(t *testing.T) {
			packfile := openFixturePack(t, name)
			records := verifyRecords(t, name)
			if packfile.Count() != len(records) {
				t.Fatalf("Count = %d, git counted %d", packfile.Count(), len(records))
			}
			if packfile.Version() != packVersion {
				t.Fatalf("Version = %d, want %d", packfile.Version(), packVersion)
			}
			if packfile.Path() != fixturePackPath(t, name) {
				t.Fatalf("Path = %q, want %q", packfile.Path(), fixturePackPath(t, name))
			}
			if packfile.Size() == 0 {
				t.Fatal("Size is zero")
			}
			if err := packfile.Verify(); err != nil {
				t.Fatalf("Verify returned error %v", err)
			}
		})
	}
}

func TestObjectAtRebuildsEveryObjectGitPacked(t *testing.T) {
	table := objectTypes(t)
	for _, name := range fixtureNames(t) {
		t.Run(name, func(t *testing.T) {
			index := openFixtureIndex(t, name)
			packfile := openFixturePack(t, name, WithIndex(index))
			for _, record := range verifyRecords(t, name) {
				kind, data, err := packfile.ObjectAt(record.offset)
				if err != nil {
					t.Fatalf("ObjectAt(%d) returned error %v", record.offset, err)
				}
				if kind != record.kind {
					t.Errorf("ObjectAt(%d) gave %s, git says %s", record.offset, kind, record.kind)
				}
				if got := hash.SumSHA1(kind.String(), data); got != record.id {
					t.Errorf("ObjectAt(%d) rebuilt %s, want %s", record.offset, got, record.id)
				}
				want, ok := table[record.id]
				if !ok {
					t.Fatalf("the object table does not list %s", record.id)
				}
				if int64(len(data)) != want.size || kind != want.kind {
					t.Errorf("%s is %d bytes of %s, git says %d bytes of %s",
						record.id, len(data), kind, want.size, want.kind)
				}
			}
		})
	}
}

func TestHeaderAtAgreesWithVerifyPack(t *testing.T) {
	for _, name := range fixtureNames(t) {
		t.Run(name, func(t *testing.T) {
			index := openFixtureIndex(t, name)
			packfile := openFixturePack(t, name)
			deltas := 0
			for _, record := range verifyRecords(t, name) {
				head, err := packfile.HeaderAt(record.offset)
				if err != nil {
					t.Fatalf("HeaderAt(%d) returned error %v", record.offset, err)
				}
				if head.Size != record.size {
					t.Errorf("HeaderAt(%d) declares %d bytes, git says %d", record.offset, head.Size, record.size)
				}
				if head.Kind.IsDelta() != record.isDelta {
					t.Errorf("HeaderAt(%d) gave kind %s, git says delta=%v", record.offset, head.Kind, record.isDelta)
				}
				if !record.isDelta {
					if head.Kind.Type() != record.kind {
						t.Errorf("HeaderAt(%d) gave %s, git says %s", record.offset, head.Kind, record.kind)
					}
					continue
				}
				deltas++
				switch head.Kind {
				case KindOffsetDelta:
					want, ok := index.Find(record.base)
					if !ok {
						t.Fatalf("the index does not hold the base %s", record.base)
					}
					if head.BaseOffset != want {
						t.Errorf("HeaderAt(%d) points at %d, git says the base sits at %d",
							record.offset, head.BaseOffset, want)
					}
				default:
					if head.BaseID != record.base {
						t.Errorf("HeaderAt(%d) points at %s, git says %s", record.offset, head.BaseID, record.base)
					}
				}
			}
			if deltas == 0 {
				t.Fatal("the fixture holds no deltas")
			}
		})
	}
}

func TestKindNames(t *testing.T) {
	cases := map[Kind]string{
		KindCommit:      "commit",
		KindTree:        "tree",
		KindBlob:        "blob",
		KindTag:         "tag",
		KindOffsetDelta: "ofs-delta",
		KindRefDelta:    "ref-delta",
		Kind(5):         "unknown",
	}
	for kind, want := range cases {
		if got := kind.String(); got != want {
			t.Errorf("Kind(%d).String() = %q, want %q", uint8(kind), got, want)
		}
	}
	if got := KindRefDelta.Type(); got != 0 {
		t.Errorf("KindRefDelta.Type() = %d, want 0", got)
	}
	if got := KindTree.Type(); got != object.TypeTree {
		t.Errorf("KindTree.Type() = %s, want %s", got, object.TypeTree)
	}
}

func TestNewPackRejectsBrokenFiles(t *testing.T) {
	valid := readFixture(t, fixturePackPath(t, fixtureName(t, offsetPack)))
	badMagic := slices.Clone(valid)
	badMagic[0] = 'x'
	badVersion := slices.Clone(valid)
	binary.BigEndian.PutUint32(badVersion[4:], 3)

	cases := []struct {
		name string
		raw  []byte
		want error
	}{
		{"tooShort", valid[:headerSize+hash.Size-1], ErrTruncated},
		{"badMagic", badMagic, ErrBadMagic},
		{"version3", badVersion, ErrUnsupportedVersion},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewPack(bytes.NewReader(tc.raw), int64(len(tc.raw))); !errors.Is(err, tc.want) {
				t.Fatalf("NewPack returned %v, want %v", err, tc.want)
			}
		})
	}
}

func TestNewPackPropagatesReadErrors(t *testing.T) {
	raw := readFixture(t, fixturePackPath(t, fixtureName(t, offsetPack)))
	size := int64(len(raw))
	cases := []struct {
		name string
		from int64
		to   int64
	}{
		{"header", 0, 1},
		{"trailer", size - hash.Size, size},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			source := brokenReaderAt{data: raw, from: tc.from, to: tc.to}
			if _, err := NewPack(source, size); !errors.Is(err, errRead) {
				t.Fatalf("NewPack returned %v, want %v", err, errRead)
			}
		})
	}
}

func TestOpenPackReportsMissingFile(t *testing.T) {
	if _, err := OpenPack(filepath.Join(t.TempDir(), "absent.pack")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("OpenPack returned %v, want %v", err, os.ErrNotExist)
	}
}

func TestOpenPackReportsBrokenFile(t *testing.T) {
	path := writeTemp(t, filepath.Join(t.TempDir(), "broken.pack"), bytes.Repeat([]byte{0}, headerSize+hash.Size))
	if _, err := OpenPack(path); !errors.Is(err, ErrBadMagic) {
		t.Fatalf("OpenPack returned %v, want %v", err, ErrBadMagic)
	}
}

func TestNewPackFileRejectsClosedFile(t *testing.T) {
	file, err := os.Open(fixturePackPath(t, fixtureName(t, offsetPack)))
	if err != nil {
		t.Fatalf("Open returned error %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Close returned error %v", err)
	}
	if _, err := NewPackFile(file); err == nil {
		t.Fatal("NewPackFile accepted a closed file")
	}
}

func TestPackWithoutFileClosesQuietly(t *testing.T) {
	raw := readFixture(t, fixturePackPath(t, fixtureName(t, offsetPack)))
	if err := packOf(t, raw).Close(); err != nil {
		t.Fatalf("Close returned error %v", err)
	}
}

func TestVerifyDetectsPackDamage(t *testing.T) {
	raw := readFixture(t, fixturePackPath(t, fixtureName(t, offsetPack)))
	raw[headerSize] ^= 0xff
	if err := packOf(t, raw).Verify(); !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("Verify returned %v, want %v", err, ErrChecksumMismatch)
	}
}

func TestVerifyReportsPackReadFailure(t *testing.T) {
	raw := readFixture(t, fixturePackPath(t, fixtureName(t, offsetPack)))
	packfile := packOf(t, raw)
	packfile.source = brokenReaderAt{data: raw, from: 0, to: 1}
	if err := packfile.Verify(); !errors.Is(err, errRead) {
		t.Fatalf("Verify returned %v, want %v", err, errRead)
	}
}

func TestObjectAtRejectsOffsetsOutsideThePackfile(t *testing.T) {
	packfile := openFixturePack(t, fixtureName(t, offsetPack))
	for _, offset := range []int64{0, headerSize - 1, packfile.Size() - hash.Size, packfile.Size() + 1} {
		if _, _, err := packfile.ObjectAt(offset); !errors.Is(err, ErrBadOffset) {
			t.Fatalf("ObjectAt(%d) returned %v, want %v", offset, err, ErrBadOffset)
		}
	}
}

func TestObjectAtReportsHeaderReadFailure(t *testing.T) {
	raw := readFixture(t, fixturePackPath(t, fixtureName(t, offsetPack)))
	packfile := packOf(t, raw)
	packfile.source = brokenReaderAt{data: raw, from: headerSize, to: math.MaxInt64}
	if _, _, err := packfile.ObjectAt(headerSize); !errors.Is(err, errRead) {
		t.Fatalf("ObjectAt returned %v, want %v", err, errRead)
	}
}

func TestObjectAtRejectsBrokenHeaders(t *testing.T) {
	cases := []struct {
		name  string
		entry []byte
		want  error
	}{
		{"unknownKind", []byte{0x50, 0x00}, ErrUnknownObjectKind},
		{"reservedKind", []byte{0x00, 0x00}, ErrUnknownObjectKind},
		{"sizeOverflow", append(bytes.Repeat([]byte{0xb0 | continuation}, 11), 0x01), ErrBadObjectHeader},
		{"negativeSize", slices.Concat([]byte{0xb0 | continuation},
			bytes.Repeat([]byte{continuation | 0x7f}, 8), []byte{0x7f}), ErrBadObjectHeader},
		{"selfReferencingDelta", []byte{0x60 | 0x01, 0x00}, ErrBadOffset},
		{"deltaBeforeTheHeader", []byte{0x60 | 0x01, 0x7f}, ErrBadOffset},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			builder := newPackBuilder()
			offset := builder.addRaw(tc.entry)
			packfile := packOf(t, builder.bytes())
			if _, _, err := packfile.ObjectAt(offset); !errors.Is(err, tc.want) {
				t.Fatalf("ObjectAt returned %v, want %v", err, tc.want)
			}
		})
	}
}

func TestObjectAtRejectsHeadersRunningPastTheFile(t *testing.T) {
	head := slices.Concat(packMagic, binary.BigEndian.AppendUint32(nil, packVersion),
		binary.BigEndian.AppendUint32(nil, 1))
	raw := slices.Concat(head, []byte{0xf1, 0x01}, make([]byte, hash.Size-1))
	if _, _, err := packOf(t, raw).ObjectAt(headerSize); !errors.Is(err, ErrTruncated) {
		t.Fatalf("ObjectAt returned %v, want %v", err, ErrTruncated)
	}
}

func TestObjectAtRejectsDeltaOffsetOverflow(t *testing.T) {
	builder := newPackBuilder()
	offset := builder.addRaw(slices.Concat([]byte{0x60 | 0x01},
		bytes.Repeat([]byte{continuation | 0x7f}, 9), []byte{0x01}))
	if _, _, err := packOf(t, builder.bytes()).ObjectAt(offset); !errors.Is(err, ErrBadObjectHeader) {
		t.Fatalf("ObjectAt returned %v, want %v", err, ErrBadObjectHeader)
	}
}

func TestObjectAtRejectsImplausibleSize(t *testing.T) {
	builder := newPackBuilder()
	offset := builder.add(KindBlob, 1<<40, nil, deflate(t, []byte("small")))
	if _, _, err := packOf(t, builder.bytes()).ObjectAt(offset); !errors.Is(err, ErrObjectTooLarge) {
		t.Fatalf("ObjectAt returned %v, want %v", err, ErrObjectTooLarge)
	}
}

func TestObjectAtRejectsDataPastTheTrailer(t *testing.T) {
	builder := newPackBuilder()
	builder.addObject(t, KindBlob, []byte("content"))
	raw := builder.bytes()
	offset := int64(len(raw)) - hash.Size - 1
	raw[offset] = continuation | 0x30
	raw = appendChecksum(raw[:len(raw)-hash.Size])
	if _, _, err := packOf(t, raw).ObjectAt(offset); !errors.Is(err, ErrTruncated) {
		t.Fatalf("ObjectAt returned %v, want %v", err, ErrTruncated)
	}
}

func TestObjectAtRejectsBrokenStreams(t *testing.T) {
	builder := newPackBuilder()
	corrupt := builder.add(KindBlob, 8, nil, []byte{0x78, 0x9c, 0xff, 0xff, 0xff, 0xff})
	short := builder.add(KindBlob, 32, nil, deflate(t, []byte("short")))
	long := builder.add(KindBlob, 2, nil, deflate(t, []byte("longer than declared")))
	notZlib := builder.add(KindBlob, 4, nil, []byte{0x00, 0x00, 0x00, 0x00})
	packfile := packOf(t, builder.bytes())

	cases := []struct {
		name   string
		offset int64
		want   error
	}{
		{"corruptStream", corrupt, ErrDecompress},
		{"streamTooShort", short, ErrDecompress},
		{"streamTooLong", long, ErrSizeMismatch},
		{"noZlibHeader", notZlib, ErrDecompress},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := packfile.ObjectAt(tc.offset); !errors.Is(err, tc.want) {
				t.Fatalf("ObjectAt returned %v, want %v", err, tc.want)
			}
		})
	}
}

func TestObjectAtDetectsBrokenChecksumInTheStream(t *testing.T) {
	builder := newPackBuilder()
	compressed := deflate(t, []byte("content with a checksum"))
	compressed[len(compressed)-1] ^= 0xff
	offset := builder.add(KindBlob, 23, nil, compressed)
	if _, _, err := packOf(t, builder.bytes()).ObjectAt(offset); !errors.Is(err, ErrDecompress) {
		t.Fatalf("ObjectAt returned %v, want %v", err, ErrDecompress)
	}
}

func TestObjectAtServesTheCache(t *testing.T) {
	packfile := openFixturePack(t, fixtureName(t, offsetPack))
	record := verifyRecords(t, fixtureName(t, offsetPack))[0]
	_, first, err := packfile.ObjectAt(record.offset)
	if err != nil {
		t.Fatalf("ObjectAt returned error %v", err)
	}
	_, second, err := packfile.ObjectAt(record.offset)
	if err != nil {
		t.Fatalf("ObjectAt returned error %v", err)
	}
	if &first[0] != &second[0] {
		t.Fatal("the second read did not come from the cache")
	}
}

func TestObjectAtStopsTooDeepChains(t *testing.T) {
	builder, last := chainBuilder(t, 4)
	packfile := packOf(t, builder.bytes(), WithMaxDeltaDepth(2))
	if _, _, err := packfile.ObjectAt(last); !errors.Is(err, ErrDeltaChainTooDeep) {
		t.Fatalf("ObjectAt returned %v, want %v", err, ErrDeltaChainTooDeep)
	}
	deep := packOf(t, builder.bytes())
	if _, _, err := deep.ObjectAt(last); err != nil {
		t.Fatalf("ObjectAt returned error %v with the default depth", err)
	}
}

func TestObjectAtRejectsDeltaWithoutABase(t *testing.T) {
	builder := newPackBuilder()
	offset := builder.addRefDelta(t, idOfByte(0x42), slices.Concat(deltaSizes(0, 1), insertOp([]byte("a"))))
	if _, _, err := packOf(t, builder.bytes()).ObjectAt(offset); !errors.Is(err, ErrBaseNotFound) {
		t.Fatalf("ObjectAt returned %v, want %v", err, ErrBaseNotFound)
	}
}

func TestObjectAtRejectsBrokenDeltaPayload(t *testing.T) {
	builder := newPackBuilder()
	base := builder.addObject(t, KindBlob, []byte("base"))
	offset := builder.addOffsetDelta(t, base, []byte{0x04, 0x02, 0x00})
	if _, _, err := packOf(t, builder.bytes()).ObjectAt(offset); !errors.Is(err, ErrInvalidDelta) {
		t.Fatalf("ObjectAt returned %v, want %v", err, ErrInvalidDelta)
	}
}

func TestObjectAtReportsDeltaPayloadReadFailure(t *testing.T) {
	builder := newPackBuilder()
	base := builder.addObject(t, KindBlob, []byte("base"))
	offset := builder.addOffsetDelta(t, base, slices.Concat(deltaSizes(4, 4), copyOp(0, 4)))
	raw := builder.bytes()
	packfile := packOf(t, raw)
	head, err := packfile.HeaderAt(offset)
	if err != nil {
		t.Fatalf("HeaderAt returned error %v", err)
	}
	packfile.source = brokenReaderAt{data: raw, from: head.DataOffset, to: math.MaxInt64}
	if _, _, err := packfile.ObjectAt(offset); !errors.Is(err, ErrDecompress) {
		t.Fatalf("ObjectAt returned %v, want %v", err, ErrDecompress)
	}
}

func TestObjectAtResolvesRefDeltaThroughTheIndex(t *testing.T) {
	content := []byte("base object stored in the same packfile")
	id := hash.SumSHA1(object.TypeBlob.String(), content)
	builder := newPackBuilder()
	baseOffset := builder.addObject(t, KindBlob, content)
	delta := slices.Concat(deltaSizes(int64(len(content)), int64(len(content))+1),
		copyOp(0, uint32(len(content))), insertOp([]byte("!")))
	deltaOffset := builder.addRefDelta(t, id, delta)
	raw, indexRaw := packIndexPair(t, builder, []hash.ObjectID{id, idOfByte(0x99)})
	index := indexOf(t, indexRaw)
	packfile := packOf(t, raw, WithIndex(index))
	if _, _, err := packfile.ObjectAt(baseOffset); err != nil {
		t.Fatalf("ObjectAt returned error %v", err)
	}
	kind, data, err := packfile.ObjectAt(deltaOffset)
	if err != nil {
		t.Fatalf("ObjectAt returned error %v", err)
	}
	if kind != object.TypeBlob || string(data) != string(content)+"!" {
		t.Fatalf("ObjectAt gave %s %q", kind, data)
	}
}

func TestObjectAtReportsIndexFailureWhileResolvingRefDelta(t *testing.T) {
	builder := newPackBuilder()
	offset := builder.addRefDelta(t, idOfByte(0x01), slices.Concat(deltaSizes(0, 1), insertOp([]byte("a"))))
	indexRaw := buildIndex([]Entry{{ID: idOfByte(0x01), Offset: 12}}, hash.Zero)
	index := indexOf(t, indexRaw)
	index.source = brokenReaderAt{data: indexRaw, from: index.names, to: math.MaxInt64}
	if _, _, err := packOf(t, builder.bytes(), WithIndex(index)).ObjectAt(offset); !errors.Is(err, errRead) {
		t.Fatalf("ObjectAt returned %v, want %v", err, errRead)
	}
}

func TestObjectAtResolvesRefDeltaThroughAResolver(t *testing.T) {
	content := []byte("base object kept outside the packfile")
	id := hash.SumSHA1(object.TypeBlob.String(), content)
	builder := newPackBuilder()
	delta := slices.Concat(deltaSizes(int64(len(content)), int64(len(content))+1),
		copyOp(0, uint32(len(content))), insertOp([]byte("?")))
	offset := builder.addRefDelta(t, id, delta)
	resolver := mapResolver{id: {kind: object.TypeBlob, data: content}}
	packfile := packOf(t, builder.bytes(), WithBaseResolver(resolver))
	kind, data, err := packfile.ObjectAt(offset)
	if err != nil {
		t.Fatalf("ObjectAt returned error %v", err)
	}
	if kind != object.TypeBlob || string(data) != string(content)+"?" {
		t.Fatalf("ObjectAt gave %s %q", kind, data)
	}
}

func TestThinPackNeedsAResolver(t *testing.T) {
	raw := readFixture(t, thinPackPath)
	packfile := packOf(t, raw)
	offset, delta := firstRefDelta(t, raw)
	if _, _, err := packfile.ObjectAt(offset); !errors.Is(err, ErrBaseNotFound) {
		t.Fatalf("ObjectAt returned %v, want %v", err, ErrBaseNotFound)
	}
	store := openFixtureStore(t)
	resolved := packOf(t, raw, WithBaseResolver(store))
	kind, data, err := resolved.ObjectAt(offset)
	if err != nil {
		t.Fatalf("ObjectAt returned error %v", err)
	}
	if kind == 0 || len(data) == 0 {
		t.Fatalf("ObjectAt gave %s and %d bytes", kind, len(data))
	}
	if _, ok := store.snapshot()[0].Index.Find(delta.BaseID); !ok {
		t.Fatalf("the store does not hold the base %s", delta.BaseID)
	}
}

func TestThinPackObjectsRebuildTheirNames(t *testing.T) {
	raw := readFixture(t, thinPackPath)
	store := openFixtureStore(t)
	packfile := packOf(t, raw, WithBaseResolver(store))
	reader := readerOf(t, raw)
	rebuilt := 0
	for entry, err := range readAll(t, reader) {
		if err != nil {
			t.Fatalf("NextObject returned error %v", err)
		}
		kind, data, err := packfile.ObjectAt(entry.Header.Offset)
		if err != nil {
			t.Fatalf("ObjectAt(%d) returned error %v", entry.Header.Offset, err)
		}
		id := hash.SumSHA1(kind.String(), data)
		if _, _, ok, err := store.Get(id); err != nil || !ok {
			t.Fatalf("the store does not hold the rebuilt object %s (%v)", id, err)
		}
		rebuilt++
	}
	if rebuilt == 0 {
		t.Fatal("the thin packfile holds no objects")
	}
}

func firstRefDelta(t *testing.T, raw []byte) (int64, ObjectHeader) {
	t.Helper()
	reader := readerOf(t, raw)
	for entry, err := range readAll(t, reader) {
		if err != nil {
			t.Fatalf("NextObject returned error %v", err)
		}
		if entry.Header.Kind == KindRefDelta {
			return entry.Header.Offset, entry.Header
		}
	}
	t.Fatal("the packfile holds no reference deltas")
	return 0, ObjectHeader{}
}

type resolvedObject struct {
	kind object.Type
	data []byte
}

type mapResolver map[hash.ObjectID]resolvedObject

func (m mapResolver) ResolveBase(id hash.ObjectID, depth int) (object.Type, []byte, error) {
	found, ok := m[id]
	if !ok {
		return 0, nil, ErrBaseNotFound
	}
	return found.kind, found.data, nil
}

func TestOptionsFallBackToTheDefaultDepth(t *testing.T) {
	applied := newSettings([]Option{WithMaxDeltaDepth(0)})
	if applied.maxDepth != DefaultMaxDeltaDepth {
		t.Fatalf("maxDepth = %d, want %d", applied.maxDepth, DefaultMaxDeltaDepth)
	}
	if applied.cache == nil {
		t.Fatal("newSettings left the cache empty")
	}
}

func TestObjectAtRejectsImplausibleDeltaSize(t *testing.T) {
	builder := newPackBuilder()
	base := builder.addObject(t, KindBlob, []byte("base"))
	delta := slices.Concat(deltaSizes(4, 4), copyOp(0, 4))
	offset := builder.add(KindOffsetDelta, 1<<40, encodeBaseOffset(int64(len(builder.body))-base), deflate(t, delta))
	if _, _, err := packOf(t, builder.bytes()).ObjectAt(offset); !errors.Is(err, ErrObjectTooLarge) {
		t.Fatalf("ObjectAt returned %v, want %v", err, ErrObjectTooLarge)
	}
}
