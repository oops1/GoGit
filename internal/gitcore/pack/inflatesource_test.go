package pack

import (
	"bytes"
	"compress/zlib"
	"io"
	"testing"
)

type plainReader struct{ source io.Reader }

func (r plainReader) Read(p []byte) (int, error) { return r.source.Read(p) }

func deflated(t *testing.T, payload []byte) []byte {
	t.Helper()
	var out bytes.Buffer
	writer := zlib.NewWriter(&out)
	if _, err := writer.Write(payload); err != nil {
		t.Fatalf("Write returned error %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close returned error %v", err)
	}
	return out.Bytes()
}

func TestInflatingFromAByteReaderConsumesExactlyTheCompressedStream(t *testing.T) {
	payload := bytes.Repeat([]byte("exact consumption matters for a streamed pack\n"), 40)
	compressed := deflated(t, payload)
	source := bytes.NewReader(append(append([]byte(nil), compressed...), []byte("next object follows")...))

	into := make([]byte, len(payload))
	if err := inflateExact(source, into); err != nil {
		t.Fatalf("inflateExact returned error %v", err)
	}

	if !bytes.Equal(into, payload) {
		t.Fatal("the inflated bytes differ")
	}
	if consumed := len(compressed); source.Size()-int64(source.Len()) != int64(consumed) {
		t.Fatalf("the source was read up to %d, want exactly %d", source.Size()-int64(source.Len()), consumed)
	}
}

func TestInflatingFromASourceWithoutByteReadingStillWorks(t *testing.T) {
	payload := bytes.Repeat([]byte("a section of a packfile\n"), 200)
	source := plainReader{source: bytes.NewReader(deflated(t, payload))}
	into := make([]byte, len(payload))

	if err := inflateExact(source, into); err != nil {
		t.Fatalf("inflateExact returned error %v", err)
	}

	if !bytes.Equal(into, payload) {
		t.Fatal("the inflated bytes differ")
	}
}

func TestInflatingFromASourceWithoutByteReadingDoesNotAllocateABufferEachTime(t *testing.T) {
	if raceEnabled {
		t.Skip("the race detector makes sync.Pool drop values, so allocation counts are not stable")
	}
	payload := bytes.Repeat([]byte("small object\n"), 20)
	compressed := deflated(t, payload)
	into := make([]byte, len(payload))

	allocated := testing.AllocsPerRun(50, func() {
		if err := inflateExact(plainReader{source: bytes.NewReader(compressed)}, into); err != nil {
			t.Fatalf("inflateExact returned error %v", err)
		}
	})

	if allocated > 4 {
		t.Fatalf("inflating a small object allocated %v times, want the buffers pooled", allocated)
	}
}

func TestAnInflaterGoesBackToThePoolWithoutHoldingItsSource(t *testing.T) {
	source := plainReader{source: bytes.NewReader(deflated(t, []byte("x")))}
	reader, err := acquireInflater(source)
	if err != nil {
		t.Fatalf("acquireInflater returned error %v", err)
	}
	pooled := reader.(*pooledInflater)

	releaseInflater(reader)

	if pooled.buffer.Buffered() != 0 {
		t.Fatal("a released inflater still buffers bytes of its source")
	}
}

func TestAcquireInflaterReportsAStreamThatIsNotZlibAndReturnsTheInflater(t *testing.T) {
	if _, err := acquireInflater(plainReader{source: bytes.NewReader([]byte("not zlib at all"))}); err == nil {
		t.Fatal("acquireInflater accepted a stream that is not zlib")
	}
}
