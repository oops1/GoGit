package pack

import (
	"bytes"
	"crypto/rand"
	"errors"
	"io"
	"testing"
	"testing/iotest"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
)

type streamingSource struct {
	*reusingSource
	streams   map[hash.ObjectID]int
	streamErr error
	body      func(data []byte) io.Reader
}

func newStreamingSource(plain *fakeSource, packs ...*memoryPack) *streamingSource {
	return &streamingSource{reusingSource: newReusingSource(plain, packs...), streams: map[hash.ObjectID]int{}}
}

func (s *streamingSource) Stream(id hash.ObjectID) (object.Type, int64, io.ReadCloser, error) {
	s.streams[id]++
	if s.streamErr != nil {
		return 0, 0, nil, s.streamErr
	}
	obj := s.objects[id]
	var body io.Reader = bytes.NewReader(obj.data)
	if s.body != nil {
		body = s.body(obj.data)
	}
	return obj.kind, int64(len(obj.data)), io.NopCloser(body), nil
}

func TestWritePackNeverDeltifiesObjectsAboveTheBigFileThreshold(t *testing.T) {
	plain := newFakeSource()
	var big []hash.ObjectID
	for seed := range 6 {
		big = append(big, plain.add(object.TypeBlob, similarBlob(seed, 200)))
	}
	smallIDs := []hash.ObjectID{plain.add(object.TypeBlob, similarBlob(0, 20)), plain.add(object.TypeBlob, similarBlob(1, 20))}

	var out bytes.Buffer
	result, err := WritePack(t.Context(), &out, plain, append(big, smallIDs...), WriteOptions{Window: 10, Depth: 50, BigFileThreshold: 4096})
	if err != nil {
		t.Fatalf("WritePack returned error %v", err)
	}

	for _, id := range big {
		if head := headerOf(t, out.Bytes(), result.Entries, id); head.Kind != KindBlob {
			t.Fatalf("the big object %s was stored as %s", id, head.Kind)
		}
	}
	deltas := 0
	for _, id := range smallIDs {
		if headerOf(t, out.Bytes(), result.Entries, id).Kind.IsDelta() {
			deltas++
		}
	}
	if deltas != 1 {
		t.Fatalf("%d small objects became deltas, want 1", deltas)
	}
	requireReadBack(t, out.Bytes(), result.Entries, plain)
}

func TestWritePackStreamsObjectsAboveTheBigFileThreshold(t *testing.T) {
	plain := newFakeSource()
	big := plain.add(object.TypeBlob, similarBlob(0, 200))
	small := plain.add(object.TypeTree, []byte("100644 blob deadbeef\tfile.txt\n"))
	source := newStreamingSource(plain)

	var out bytes.Buffer
	result, err := WritePack(t.Context(), &out, source, []hash.ObjectID{big, small}, WriteOptions{Window: 10, Depth: 50, BigFileThreshold: 1024})
	if err != nil {
		t.Fatalf("WritePack returned error %v", err)
	}

	if source.streams[big] != 1 || source.gets[big] != 0 || source.streams[small] != 0 || source.gets[small] != 1 {
		t.Fatalf("streams = %v, reads = %v", source.streams, source.gets)
	}
	requireReadBack(t, out.Bytes(), result.Entries, plain)
}

func TestWritePackCopiesAStoredBigObjectInsteadOfStreamingIt(t *testing.T) {
	plain := newFakeSource()
	big := plain.add(object.TypeBlob, similarBlob(0, 200))
	raw, index, _ := writtenPackPair(t, plain, []hash.ObjectID{big}, WriteOptions{})
	source := newStreamingSource(plain, newMemoryPack(t, "old", raw, index))

	var out bytes.Buffer
	result, err := WritePack(t.Context(), &out, source, []hash.ObjectID{big}, WriteOptions{Window: 10, Depth: 50, BigFileThreshold: 1024})
	if err != nil {
		t.Fatalf("WritePack returned error %v", err)
	}

	if source.streams[big] != 0 || source.gets[big] != 0 || !bytes.Equal(out.Bytes(), raw) {
		t.Fatalf("streams = %v, reads = %v, identical = %v", source.streams, source.gets, bytes.Equal(out.Bytes(), raw))
	}
	requireReadBack(t, out.Bytes(), result.Entries, plain)
}

func TestWritePackReportsBrokenStreams(t *testing.T) {
	boom := errors.New("stream is broken")
	for _, tt := range []struct {
		name  string
		setup func(source *streamingSource)
		want  error
	}{
		{"a stream that does not open", func(source *streamingSource) { source.streamErr = boom }, boom},
		{"a stream shorter than the object", func(source *streamingSource) {
			source.body = func(data []byte) io.Reader { return bytes.NewReader(data[:len(data)/2]) }
		}, ErrSizeMismatch},
		{"a stream that fails while reading", func(source *streamingSource) {
			source.body = func(data []byte) io.Reader { return io.MultiReader(bytes.NewReader(data[:1]), iotest.ErrReader(boom)) }
		}, boom},
	} {
		t.Run(tt.name, func(t *testing.T) {
			plain := newFakeSource()
			big := plain.add(object.TypeBlob, similarBlob(0, 200))
			source := newStreamingSource(plain)
			tt.setup(source)

			_, err := WritePack(t.Context(), &bytes.Buffer{}, source, []hash.ObjectID{big}, WriteOptions{BigFileThreshold: 1024})

			if !errors.Is(err, tt.want) {
				t.Fatalf("WritePack returned %v, want %v", err, tt.want)
			}
		})
	}
}

func TestPackWriterWriteStreamReportsWriteFailures(t *testing.T) {
	head := encodeObjectHead(KindBlob, 1)
	for _, limit := range []int64{0, int64(len(head)) + 2} {
		writer := newPackWriter(&limitedWriter{limit: limit})
		if _, err := writer.writeStream(hash.Zero, KindBlob, 1, bytes.NewReader([]byte("x"))); !errors.Is(err, errWriteLimitReached) {
			t.Fatalf("writeStream with a limit of %d returned %v", limit, err)
		}
	}
}

func TestPackWriterWriteStoredReportsWriteFailures(t *testing.T) {
	plain := newFakeSource()
	noise := make([]byte, 3*copyBufferSize)
	if _, err := rand.Read(noise); err != nil {
		t.Fatalf("rand.Read returned error %v", err)
	}
	large := plain.add(object.TypeBlob, noise)
	small := plain.add(object.TypeTree, []byte("100644 blob deadbeef\tfile.txt\n"))
	raw, index, _ := writtenPackPair(t, plain, []hash.ObjectID{large, small}, WriteOptions{})
	reuse := memoryReuse(newMemoryPack(t, "old", raw, index))
	for _, tt := range []struct {
		name  string
		id    hash.ObjectID
		limit int64
	}{
		{"the header", small, 0},
		{"a small body", small, 1},
		{"a large body", large, 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			stored, err := reuse.find(tt.id)
			if err != nil || stored == nil {
				t.Fatalf("find = (%+v, %v)", stored, err)
			}
			writer := newPackWriter(&limitedWriter{limit: tt.limit})
			body, intact, err := writer.readStored(stored)
			if err != nil || !intact {
				t.Fatalf("readStored = (%v, %v)", intact, err)
			}

			if _, err := writer.writeStored(tt.id, stored.head.Kind, nil, body, stored); !errors.Is(err, errWriteLimitReached) {
				t.Fatalf("writeStored returned %v, want %v", err, errWriteLimitReached)
			}
		})
	}
}

func TestWritePackDeltifiesLooseObjectsAgainstStoredOnes(t *testing.T) {
	plain := newFakeSource()
	big, small := deltaPair(plain)
	raw, index, _ := writtenPackPair(t, plain, []hash.ObjectID{big}, WriteOptions{})
	source := newReusingSource(plain, newMemoryPack(t, "old", raw, index))

	var out bytes.Buffer
	result, err := WritePack(t.Context(), &out, source, []hash.ObjectID{big, small}, WriteOptions{Window: 10, Depth: 50})
	if err != nil {
		t.Fatalf("WritePack returned error %v", err)
	}

	if head := headerOf(t, out.Bytes(), result.Entries, small); head.Kind != KindOffsetDelta || source.gets[big] != 1 || source.gets[small] != 1 {
		t.Fatalf("header = %+v, reads = %v", head, source.gets)
	}
	requireReadBack(t, out.Bytes(), result.Entries, plain)
}

func TestWritePackReportsObjectsItCannotLoadForTheDeltaSearch(t *testing.T) {
	lost := errors.New("object vanished")
	for _, tt := range []struct {
		name   string
		stored bool
		thin   bool
		lose   func(big, small hash.ObjectID) hash.ObjectID
	}{
		{"the target of a window search", false, false, func(_, small hash.ObjectID) hash.ObjectID { return small }},
		{"a stored candidate", true, false, func(big, _ hash.ObjectID) hash.ObjectID { return big }},
		{"the target of a thin search", false, true, func(_, small hash.ObjectID) hash.ObjectID { return small }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			plain := newFakeSource()
			big, small := deltaPair(plain)
			var packs []*memoryPack
			if tt.stored {
				raw, index, _ := writtenPackPair(t, plain, []hash.ObjectID{big}, WriteOptions{})
				packs = append(packs, newMemoryPack(t, "old", raw, index))
			}
			opts := WriteOptions{Window: 10, Depth: 50}
			ids := []hash.ObjectID{big, small}
			if tt.thin {
				opts.Thin = map[hash.ObjectID]struct{}{big: {}}
				ids = []hash.ObjectID{small}
			}
			plain.failNextGet(tt.lose(big, small), lost)

			_, err := WritePack(t.Context(), &bytes.Buffer{}, newReusingSource(plain, packs...), ids, opts)

			if !errors.Is(err, lost) {
				t.Fatalf("WritePack returned %v, want %v", err, lost)
			}
		})
	}
}
