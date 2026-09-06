package pack

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
)

type fakeObject struct {
	kind object.Type
	data []byte
}

type fakeSource struct {
	objects map[hash.ObjectID]fakeObject
	failFor hash.ObjectID
	failErr error
}

func newFakeSource() *fakeSource {
	return &fakeSource{objects: make(map[hash.ObjectID]fakeObject)}
}

func (s *fakeSource) Get(id hash.ObjectID) (object.Type, []byte, error) {
	if s.failErr != nil && id == s.failFor {
		return 0, nil, s.failErr
	}
	obj, ok := s.objects[id]
	if !ok {
		return 0, nil, fmt.Errorf("fakeSource: unknown object %s", id)
	}
	return obj.kind, obj.data, nil
}

func (s *fakeSource) add(kind object.Type, data []byte) hash.ObjectID {
	id := hash.SumSHA1(kind.String(), data)
	s.objects[id] = fakeObject{kind: kind, data: data}
	return id
}

func (s *fakeSource) failNextGet(id hash.ObjectID, err error) {
	s.failFor = id
	s.failErr = err
}

func similarBlob(seed, lines int) []byte {
	var buf bytes.Buffer
	for line := range lines {
		state := 0
		if line%9 <= seed {
			state = 1
		}
		fmt.Fprintf(&buf, "line %05d state %d filler aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n", line, state)
	}
	return buf.Bytes()
}

func openWrittenPack(t testing.TB, packBytes []byte, opts ...Option) *Pack {
	t.Helper()
	packfile, err := NewPack(bytes.NewReader(packBytes), int64(len(packBytes)), opts...)
	if err != nil {
		t.Fatalf("NewPack returned error %v", err)
	}
	return packfile
}

func requireWriteIndex(t testing.TB, entries []Entry, checksum hash.ObjectID) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := WriteIndex(&buf, entries, checksum); err != nil {
		t.Fatalf("WriteIndex returned error %v", err)
	}
	return buf.Bytes()
}

func openWrittenIndex(t testing.TB, indexBytes []byte) *Index {
	t.Helper()
	index, err := NewIndex(bytes.NewReader(indexBytes), int64(len(indexBytes)))
	if err != nil {
		t.Fatalf("NewIndex returned error %v", err)
	}
	return index
}

func TestWritePackRoundTripsEveryObjectByteForByte(t *testing.T) {
	source := newFakeSource()
	var ids []hash.ObjectID
	ids = append(ids, source.add(object.TypeBlob, []byte("hello, world")))
	ids = append(ids, source.add(object.TypeBlob, []byte{}))
	ids = append(ids, source.add(object.TypeTree, []byte("100644 blob deadbeef\tfile.txt\n")))
	ids = append(ids, source.add(object.TypeCommit, []byte("tree deadbeef\nmessage\n")))
	ids = append(ids, source.add(object.TypeTag, []byte("object deadbeef\ntag v1\n")))
	ids = append(ids, source.add(object.TypeBlob, similarBlob(3, 20)))

	var buf bytes.Buffer
	result, err := WritePack(t.Context(), &buf, source, ids, WriteOptions{})
	if err != nil {
		t.Fatalf("WritePack returned error %v", err)
	}
	if result.Objects != len(ids) {
		t.Fatalf("Objects = %d, want %d", result.Objects, len(ids))
	}
	if len(result.Entries) != len(ids) {
		t.Fatalf("len(Entries) = %d, want %d", len(result.Entries), len(ids))
	}
	if result.Bytes != int64(buf.Len()) {
		t.Fatalf("Bytes = %d, want %d", result.Bytes, buf.Len())
	}

	packfile := openWrittenPack(t, buf.Bytes())
	defer func() { _ = packfile.Close() }()
	if packfile.Checksum() != result.Checksum {
		t.Fatalf("Checksum = %s, trailer holds %s", result.Checksum, packfile.Checksum())
	}
	if err := packfile.Verify(); err != nil {
		t.Fatalf("Verify returned error %v", err)
	}
	if packfile.Count() != len(ids) {
		t.Fatalf("Count = %d, want %d", packfile.Count(), len(ids))
	}

	for _, entry := range result.Entries {
		wantKind, wantData, err := source.Get(entry.ID)
		if err != nil {
			t.Fatalf("Get(%s) returned error %v", entry.ID, err)
		}
		kind, data, err := packfile.ObjectAt(entry.Offset)
		if err != nil {
			t.Fatalf("ObjectAt(%d) returned error %v", entry.Offset, err)
		}
		if kind != wantKind {
			t.Errorf("ObjectAt(%d) kind = %s, want %s", entry.Offset, kind, wantKind)
		}
		if !bytes.Equal(data, wantData) {
			t.Errorf("ObjectAt(%d) gave %d bytes, want %d", entry.Offset, len(data), len(wantData))
		}
	}
}

func TestWritePackWithWindowIndexRoundTrips(t *testing.T) {
	source := newFakeSource()
	var ids []hash.ObjectID
	for i := range 8 {
		ids = append(ids, source.add(object.TypeBlob, similarBlob(i, 400)))
	}
	var buf bytes.Buffer
	result, err := WritePack(t.Context(), &buf, source, ids, WriteOptions{Window: 8, Depth: 8})
	if err != nil {
		t.Fatalf("WritePack returned error %v", err)
	}

	indexBytes := requireWriteIndex(t, result.Entries, result.Checksum)
	index := openWrittenIndex(t, indexBytes)
	defer func() { _ = index.Close() }()
	if err := index.Verify(); err != nil {
		t.Fatalf("Index.Verify returned error %v", err)
	}
	if index.Count() != len(ids) {
		t.Fatalf("Count = %d, want %d", index.Count(), len(ids))
	}
	if index.PackHash() != result.Checksum {
		t.Fatalf("PackHash = %s, want %s", index.PackHash(), result.Checksum)
	}

	packfile := openWrittenPack(t, buf.Bytes(), WithIndex(index))
	defer func() { _ = packfile.Close() }()
	if err := packfile.Verify(); err != nil {
		t.Fatalf("Pack.Verify returned error %v", err)
	}
	for _, id := range ids {
		offset, ok, err := index.Lookup(id)
		if err != nil || !ok {
			t.Fatalf("Lookup(%s) returned (%v, %v)", id, ok, err)
		}
		wantKind, wantData, _ := source.Get(id)
		kind, data, err := packfile.ObjectAt(offset)
		if err != nil {
			t.Fatalf("ObjectAt(%d) returned error %v", offset, err)
		}
		if kind != wantKind || !bytes.Equal(data, wantData) {
			t.Errorf("ObjectAt(%d) did not reproduce %s", offset, id)
		}
	}
}

func TestWritePackDeltasShrinkThePackfile(t *testing.T) {
	source := newFakeSource()
	var ids []hash.ObjectID
	for i := range 10 {
		ids = append(ids, source.add(object.TypeBlob, similarBlob(i, 500)))
	}

	var withoutDelta bytes.Buffer
	if _, err := WritePack(t.Context(), &withoutDelta, source, ids, WriteOptions{}); err != nil {
		t.Fatalf("WritePack (no window) returned error %v", err)
	}
	var withDelta bytes.Buffer
	if _, err := WritePack(t.Context(), &withDelta, source, ids, WriteOptions{Window: 10, Depth: 10}); err != nil {
		t.Fatalf("WritePack (with window) returned error %v", err)
	}
	if withDelta.Len() >= withoutDelta.Len() {
		t.Fatalf("delta pack holds %d bytes, non-delta pack holds %d, want smaller", withDelta.Len(), withoutDelta.Len())
	}
}

func TestWritePackRespectsChainDepthLimit(t *testing.T) {
	source := newFakeSource()
	var ids []hash.ObjectID
	content := similarBlob(0, 300)
	ids = append(ids, source.add(object.TypeBlob, content))
	for i := 1; i < 12; i++ {
		content = append(bytes.Clone(content), []byte(fmt.Sprintf("tail %d\n", i))...)
		ids = append(ids, source.add(object.TypeBlob, content))
	}

	const depth = 3
	var buf bytes.Buffer
	result, err := WritePack(t.Context(), &buf, source, ids, WriteOptions{Window: len(ids), Depth: depth})
	if err != nil {
		t.Fatalf("WritePack returned error %v", err)
	}
	packfile := openWrittenPack(t, buf.Bytes())
	defer func() { _ = packfile.Close() }()

	foundDelta := false
	for _, entry := range result.Entries {
		chainDepth := 0
		head, err := packfile.HeaderAt(entry.Offset)
		if err != nil {
			t.Fatalf("HeaderAt(%d) returned error %v", entry.Offset, err)
		}
		for head.Kind == KindOffsetDelta {
			foundDelta = true
			chainDepth++
			if chainDepth > depth {
				t.Fatalf("object at %d has a delta chain deeper than %d", entry.Offset, depth)
			}
			head, err = packfile.HeaderAt(head.BaseOffset)
			if err != nil {
				t.Fatalf("HeaderAt(%d) returned error %v", head.BaseOffset, err)
			}
		}
	}
	if !foundDelta {
		t.Fatal("no delta was produced, the depth limit was never exercised")
	}
}

func TestWritePackUsesRefDeltaForThinBases(t *testing.T) {
	source := newFakeSource()
	base := similarBlob(0, 400)
	baseID := source.add(object.TypeBlob, base)
	targetData := append(bytes.Clone(base), []byte("extra tail data appended to the thin target\n")...)
	targetID := source.add(object.TypeBlob, targetData)

	var buf bytes.Buffer
	result, err := WritePack(t.Context(), &buf, source, []hash.ObjectID{targetID},
		WriteOptions{Window: 4, Depth: 4, Thin: map[hash.ObjectID]struct{}{baseID: {}}})
	if err != nil {
		t.Fatalf("WritePack returned error %v", err)
	}
	if len(result.Entries) != 1 {
		t.Fatalf("len(Entries) = %d, want 1", len(result.Entries))
	}
	packfile := openWrittenPack(t, buf.Bytes())
	defer func() { _ = packfile.Close() }()
	head, err := packfile.HeaderAt(result.Entries[0].Offset)
	if err != nil {
		t.Fatalf("HeaderAt returned error %v", err)
	}
	if head.Kind != KindRefDelta {
		t.Fatalf("Kind = %s, want ref-delta", head.Kind)
	}
	if head.BaseID != baseID {
		t.Fatalf("BaseID = %s, want %s", head.BaseID, baseID)
	}
	_, data, err := applyRefDeltaForTest(t, packfile, head, base)
	if err != nil {
		t.Fatalf("resolving the ref-delta returned error %v", err)
	}
	if !bytes.Equal(data, targetData) {
		t.Fatalf("resolved data does not match the target")
	}
}

func applyRefDeltaForTest(t testing.TB, packfile *Pack, head ObjectHeader, base []byte) (object.Type, []byte, error) {
	t.Helper()
	if err := packfile.checkSize(head); err != nil {
		return 0, nil, err
	}
	compressed := packfile.dataReader(head)
	raw := make([]byte, head.Size)
	if err := inflateExact(compressed, raw); err != nil {
		return 0, nil, err
	}
	data, err := ApplyDelta(base, raw)
	if err != nil {
		return 0, nil, err
	}
	return object.TypeBlob, data, nil
}

func TestWritePackPropagatesObjectSourceErrors(t *testing.T) {
	source := newFakeSource()
	id := source.add(object.TypeBlob, []byte("content"))
	wantErr := errors.New("boom")
	source.failNextGet(id, wantErr)

	var buf bytes.Buffer
	_, err := WritePack(t.Context(), &buf, source, []hash.ObjectID{id}, WriteOptions{})
	if !errors.Is(err, wantErr) {
		t.Fatalf("WritePack returned error %v, want wrapping %v", err, wantErr)
	}
}

func TestWritePackPropagatesThinBaseSourceErrors(t *testing.T) {
	source := newFakeSource()
	id := source.add(object.TypeBlob, []byte("content"))
	baseID := hash.SumSHA1("blob", []byte("missing base"))
	wantErr := errors.New("thin boom")
	source.failNextGet(baseID, wantErr)

	var buf bytes.Buffer
	_, err := WritePack(t.Context(), &buf, source, []hash.ObjectID{id},
		WriteOptions{Window: 1, Depth: 1, Thin: map[hash.ObjectID]struct{}{baseID: {}}})
	if !errors.Is(err, wantErr) {
		t.Fatalf("WritePack returned error %v, want wrapping %v", err, wantErr)
	}
}

func TestWritePackStopsWhenContextIsCanceled(t *testing.T) {
	source := newFakeSource()
	id := source.add(object.TypeBlob, []byte("content"))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	var buf bytes.Buffer
	_, err := WritePack(ctx, &buf, source, []hash.ObjectID{id}, WriteOptions{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("WritePack returned error %v, want %v", err, context.Canceled)
	}
}

type limitedWriter struct {
	limit int64
	total int64
}

func (w *limitedWriter) Write(chunk []byte) (int, error) {
	if w.total >= w.limit {
		return 0, errWriteLimitReached
	}
	w.total += int64(len(chunk))
	return len(chunk), nil
}

var errWriteLimitReached = errors.New("writer_test: write limit reached")

func fullyWrittenPack(t testing.TB, source *fakeSource, ids []hash.ObjectID, opts WriteOptions) []byte {
	t.Helper()
	var buf bytes.Buffer
	if _, err := WritePack(t.Context(), &buf, source, ids, opts); err != nil {
		t.Fatalf("WritePack returned error %v", err)
	}
	return buf.Bytes()
}

func TestWritePackReportsErrorsFromEveryStage(t *testing.T) {
	source := newFakeSource()
	var ids []hash.ObjectID
	ids = append(ids, source.add(object.TypeBlob, []byte("first object content")))
	ids = append(ids, source.add(object.TypeBlob, []byte("second object content, a little longer")))
	full := fullyWrittenPack(t, source, ids, WriteOptions{})

	cases := []struct {
		name  string
		limit int64
	}{
		{"header", 0},
		{"object", headerSize},
		{"trailer", int64(len(full)) - hash.Size},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			writer := &limitedWriter{limit: tc.limit}
			_, err := WritePack(t.Context(), writer, source, ids, WriteOptions{})
			if !errors.Is(err, errWriteLimitReached) {
				t.Fatalf("WritePack returned error %v, want wrapping %v", err, errWriteLimitReached)
			}
		})
	}
}

func TestWriteIndexReportsErrorsFromEveryStage(t *testing.T) {
	source := newFakeSource()
	var ids []hash.ObjectID
	ids = append(ids, source.add(object.TypeBlob, []byte("first")))
	ids = append(ids, source.add(object.TypeBlob, []byte("second, a bit longer than the first")))
	var buf bytes.Buffer
	result, err := WritePack(t.Context(), &buf, source, ids, WriteOptions{})
	if err != nil {
		t.Fatalf("WritePack returned error %v", err)
	}
	var full bytes.Buffer
	if err := WriteIndex(&full, result.Entries, result.Checksum); err != nil {
		t.Fatalf("WriteIndex returned error %v", err)
	}
	bodyLen := int64(len(full.Bytes())) - hash.Size - hash.Size

	cases := []struct {
		name  string
		limit int64
	}{
		{"body", 0},
		{"packChecksum", bodyLen},
		{"checksum", bodyLen + hash.Size},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			writer := &limitedWriter{limit: tc.limit}
			err := WriteIndex(writer, result.Entries, result.Checksum)
			if !errors.Is(err, errWriteLimitReached) {
				t.Fatalf("WriteIndex returned error %v, want wrapping %v", err, errWriteLimitReached)
			}
		})
	}
}

func TestWritePackSortsMultipleThinBasesByID(t *testing.T) {
	source := newFakeSource()
	base1 := similarBlob(0, 300)
	base1ID := source.add(object.TypeBlob, base1)
	base2 := similarBlob(5, 300)
	base2ID := source.add(object.TypeBlob, base2)
	targetData := append(bytes.Clone(base1), []byte("extra tail unique to the target\n")...)
	targetID := source.add(object.TypeBlob, targetData)

	var buf bytes.Buffer
	result, err := WritePack(t.Context(), &buf, source, []hash.ObjectID{targetID}, WriteOptions{
		Window: 4, Depth: 4,
		Thin: map[hash.ObjectID]struct{}{base1ID: {}, base2ID: {}},
	})
	if err != nil {
		t.Fatalf("WritePack returned error %v", err)
	}
	if len(result.Entries) != 1 {
		t.Fatalf("len(Entries) = %d, want 1", len(result.Entries))
	}
}

func TestSearchRefDeltaSkipsThinBasesOfADifferentType(t *testing.T) {
	target := writeObject{kind: object.TypeBlob, data: similarBlob(0, 200)}
	thins := []thinBase{
		{kind: object.TypeTree, data: []byte("tree content unrelated to the blob")},
		{kind: object.TypeBlob, id: hash.SumSHA1("blob", similarBlob(0, 200)), data: similarBlob(0, 200)},
	}
	choice := searchRefDelta(target, thins)
	if choice == nil {
		t.Fatal("searchRefDelta returned nil, want the matching blob base")
	}
	if choice.kind != KindRefDelta {
		t.Fatalf("kind = %s, want ref-delta", choice.kind)
	}
}

func TestPackWriterWriteObjectFailsWhenTheCompressedStreamCannotBeWritten(t *testing.T) {
	payload := make([]byte, 256*1024)
	if _, err := rand.Read(payload); err != nil {
		t.Fatalf("rand.Read returned error %v", err)
	}
	writer := newPackWriter(&limitedWriter{limit: 4})
	choice := writeChoice{kind: KindBlob, payload: payload}
	if _, err := writer.writeObject(hash.Zero, choice); !errors.Is(err, errWriteLimitReached) {
		t.Fatalf("writeObject returned error %v, want wrapping %v", err, errWriteLimitReached)
	}
}

func TestPackWriterWriteObjectFailsWhenTheStreamCannotBeClosed(t *testing.T) {
	const zlibHeaderSize = 2
	head := encodeObjectHead(KindBlob, 1)
	writer := newPackWriter(&limitedWriter{limit: int64(len(head)) + zlibHeaderSize})
	choice := writeChoice{kind: KindBlob, payload: []byte("x")}
	if _, err := writer.writeObject(hash.Zero, choice); !errors.Is(err, errWriteLimitReached) {
		t.Fatalf("writeObject returned error %v, want wrapping %v", err, errWriteLimitReached)
	}
}

func TestChooseEncodingIgnoresDifferentTypeCandidates(t *testing.T) {
	blob := writeObject{id: hash.SumSHA1("blob", []byte("blob content")), kind: object.TypeBlob, data: []byte("blob content")}
	written := []packedObject{{
		writeObject: writeObject{kind: object.TypeTree, data: []byte("tree content, unrelated to the blob above")},
		offset:      0,
	}}
	choice := chooseEncoding(blob, 100, written, nil, WriteOptions{Window: 4, Depth: 4})
	if choice.kind == KindOffsetDelta || choice.kind == KindRefDelta {
		t.Fatalf("chooseEncoding used a delta across mismatched types: %s", choice.kind)
	}
}

func TestSortForDeltaGroupsByTypeThenDecreasingSize(t *testing.T) {
	objects := []writeObject{
		{kind: object.TypeBlob, data: make([]byte, 10)},
		{kind: object.TypeTree, data: make([]byte, 5)},
		{kind: object.TypeBlob, data: make([]byte, 30)},
		{kind: object.TypeTree, data: make([]byte, 40)},
	}
	order := sortForDelta(objects)
	for i := 1; i < len(order); i++ {
		if order[i-1].kind > order[i].kind {
			t.Fatalf("order[%d].kind = %s came after order[%d].kind = %s", i-1, order[i-1].kind, i, order[i].kind)
		}
		if order[i-1].kind == order[i].kind && len(order[i-1].data) < len(order[i].data) {
			t.Fatalf("within kind %s, size %d came before %d", order[i].kind, len(order[i-1].data), len(order[i].data))
		}
	}
}
