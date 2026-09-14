package pack

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
)

type knownResolver struct {
	objects     mapResolver
	claimAll    bool
	containsErr error
}

func (k knownResolver) Contains(id hash.ObjectID) (bool, error) {
	if k.containsErr != nil {
		return false, k.containsErr
	}
	_, ok := k.objects[id]
	return ok || k.claimAll, nil
}

func (k knownResolver) ResolveBase(id hash.ObjectID, depth int) (object.Type, []byte, error) {
	return k.objects.ResolveBase(id, depth)
}

type failPastDataReader struct {
	data []byte
	err  error
}

func (r *failPastDataReader) Read(into []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, r.err
	}
	n := copy(into, r.data)
	r.data = r.data[n:]
	return n, nil
}

func blobID(data []byte) hash.ObjectID {
	return hash.SumSHA1(object.TypeBlob.String(), data)
}

func appendingDelta(base []byte, suffix string) []byte {
	size := int64(len(base))
	return slices.Concat(deltaSizes(size, size+int64(len(suffix))), copyOp(0, uint32(size)), insertOp([]byte(suffix)))
}

func baseWithOffsetDelta(t *testing.T) ([]byte, []byte, []byte, int64, int64) {
	t.Helper()
	base := []byte("an in-pack base the offset delta extends")
	target := append(slices.Clone(base), " and more"...)
	builder := newPackBuilder()
	baseOffset := builder.addObject(t, KindBlob, base)
	deltaOffset := builder.addOffsetDelta(t, baseOffset, appendingDelta(base, " and more"))
	return builder.bytes(), base, target, baseOffset, deltaOffset
}

func TestIndexPackRejectsJunkAfterTheTrailer(t *testing.T) {
	builder := newPackBuilder()
	builder.addObject(t, KindBlob, []byte("content followed by junk"))
	raw := append(builder.bytes(), "junk"...)
	dir := t.TempDir()

	if _, err := IndexPack(t.Context(), bytes.NewReader(raw), dir, IndexOptions{}); !errors.Is(err, ErrTrailingData) {
		t.Fatalf("IndexPack returned %v, want %v", err, ErrTrailingData)
	}
	requireEmptyDir(t, dir)
}

func TestIndexPackReportsAFailureToReadPastTheTrailer(t *testing.T) {
	builder := newPackBuilder()
	builder.addObject(t, KindBlob, []byte("content followed by a broken stream"))
	source := &failPastDataReader{data: builder.bytes(), err: errRead}
	dir := t.TempDir()

	if _, err := IndexPack(t.Context(), source, dir, IndexOptions{}); !errors.Is(err, errRead) {
		t.Fatalf("IndexPack returned %v, want %v", err, errRead)
	}
	requireEmptyDir(t, dir)
}

func TestIndexPackComparesObjectsItAlreadyHas(t *testing.T) {
	raw, base, target, _, _ := baseWithOffsetDelta(t)
	for _, tc := range []struct {
		name  string
		bases knownResolver
		want  error
	}{
		{"identicalCopies", knownResolver{objects: mapResolver{blobID(base): {kind: object.TypeBlob, data: base}, blobID(target): {kind: object.TypeBlob, data: target}}}, nil},
		{"plainObjectWithOtherBytes", knownResolver{objects: mapResolver{blobID(base): {kind: object.TypeBlob, data: []byte("forged")}}}, ErrCollision},
		{"plainObjectOfAnotherType", knownResolver{objects: mapResolver{blobID(base): {kind: object.TypeTree, data: base}}}, ErrCollision},
		{"resolvedDeltaWithOtherBytes", knownResolver{objects: mapResolver{blobID(target): {kind: object.TypeBlob, data: []byte("forged")}}}, ErrCollision},
		{"existenceCheckFails", knownResolver{containsErr: errRead}, errRead},
		{"existingObjectUnreadable", knownResolver{claimAll: true}, ErrBaseNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			_, err := IndexPack(t.Context(), bytes.NewReader(raw), dir, IndexOptions{Bases: tc.bases})
			if tc.want == nil && err != nil {
				t.Fatalf("IndexPack returned error %v", err)
			}
			if tc.want != nil {
				if !errors.Is(err, tc.want) {
					t.Fatalf("IndexPack returned %v, want %v", err, tc.want)
				}
				requireEmptyDir(t, dir)
			}
		})
	}
}

func TestIndexPackReportsACollisionBelowAnExternalBase(t *testing.T) {
	base := []byte("an external base the thin delta extends")
	target := append(slices.Clone(base), " thin"...)
	builder := newPackBuilder()
	builder.addRefDelta(t, blobID(base), appendingDelta(base, " thin"))
	bases := knownResolver{objects: mapResolver{
		blobID(base):   {kind: object.TypeBlob, data: base},
		blobID(target): {kind: object.TypeBlob, data: []byte("forged")},
	}}
	dir := t.TempDir()

	if _, err := IndexPack(t.Context(), bytes.NewReader(builder.bytes()), dir, IndexOptions{Bases: bases, FixThin: true}); !errors.Is(err, ErrCollision) {
		t.Fatalf("IndexPack returned %v, want %v", err, ErrCollision)
	}
	requireEmptyDir(t, dir)
}

func thinChain(seed int) ([]byte, []byte, []byte) {
	base := fmt.Appendf(nil, "thin chain base number %d with some padding text\n", seed)
	middle := append(slices.Clone(base), "middle\n"...)
	tail := append(slices.Clone(middle), "tail\n"...)
	return base, middle, tail
}

func thinChainPack(t *testing.T, base, middle []byte) []byte {
	t.Helper()
	builder := newPackBuilder()
	builder.addRefDelta(t, blobID(middle), appendingDelta(middle, "tail\n"))
	builder.addRefDelta(t, blobID(base), appendingDelta(base, "middle\n"))
	return builder.bytes()
}

func TestIndexPackNeverAppendsABaseThePackProducesItself(t *testing.T) {
	for _, middleFirst := range []bool{true, false} {
		t.Run(fmt.Sprintf("middleSortsFirst=%v", middleFirst), func(t *testing.T) {
			var base, middle, tail []byte
			for seed := 0; ; seed++ {
				base, middle, tail = thinChain(seed)
				if (blobID(middle).Compare(blobID(base)) < 0) == middleFirst {
					break
				}
			}
			local := mapResolver{
				blobID(base):   {kind: object.TypeBlob, data: base},
				blobID(middle): {kind: object.TypeBlob, data: middle},
			}
			dir := t.TempDir()

			result, err := IndexPack(t.Context(), bytes.NewReader(thinChainPack(t, base, middle)), dir, IndexOptions{Bases: local, FixThin: true})
			if err != nil {
				t.Fatalf("IndexPack returned error %v", err)
			}

			index, err := OpenIndex(result.IndexPath)
			if err != nil {
				t.Fatalf("OpenIndex returned error %v", err)
			}
			defer func() { _ = index.Close() }()
			got := slices.Collect(index.Objects())
			want := []hash.ObjectID{blobID(base), blobID(middle), blobID(tail)}
			slices.SortFunc(want, func(a, b hash.ObjectID) int { return a.Compare(b) })
			if result.Objects != 3 || !slices.Equal(got, want) {
				t.Fatalf("the index lists %v (%d objects), want each of %v once", got, result.Objects, want)
			}
			store := openStore(t, dir)
			if _, data, ok, err := store.Get(blobID(tail)); !ok || err != nil || !bytes.Equal(data, tail) {
				t.Fatalf("Get(tail) returned (%v, %v)", ok, err)
			}
		})
	}
}

func TestIndexPackRejectsAnOffsetDeltaWithoutAnObjectAtItsBase(t *testing.T) {
	builder := newPackBuilder()
	base := []byte("a base the delta misses by one byte")
	baseOffset := builder.addObject(t, KindBlob, base)
	builder.prepare()
	deltaOffset := int64(len(builder.body))
	delta := appendingDelta(base, "!")
	builder.add(KindOffsetDelta, int64(len(delta)), encodeBaseOffset(deltaOffset-(baseOffset+1)), deflate(t, delta))
	dir := t.TempDir()

	if _, err := IndexPack(t.Context(), bytes.NewReader(builder.bytes()), dir, IndexOptions{}); !errors.Is(err, ErrBaseNotFound) {
		t.Fatalf("IndexPack returned %v, want %v", err, ErrBaseNotFound)
	}
	requireEmptyDir(t, dir)
}

func TestIndexPackReportsTemporaryFilesThatFailToClose(t *testing.T) {
	builder := newPackBuilder()
	builder.addObject(t, KindBlob, []byte("a pack whose temporary files refuse to close"))
	raw := builder.bytes()
	for _, suffix := range []string{packSuffix, indexSuffix} {
		t.Run(suffix, func(t *testing.T) {
			original := fileClose
			fileClose = func(file *os.File) error {
				closeErr := file.Close()
				if strings.HasSuffix(file.Name(), suffix) {
					return errRead
				}
				return closeErr
			}
			t.Cleanup(func() { fileClose = original })

			if _, err := IndexPack(t.Context(), bytes.NewReader(raw), t.TempDir(), IndexOptions{}); !errors.Is(err, errRead) {
				t.Fatalf("IndexPack returned %v, want %v", err, errRead)
			}
		})
	}
}

func TestIndexerReportsReadFailuresWhileResolving(t *testing.T) {
	raw, _, _, baseOffset, deltaOffset := baseWithOffsetDelta(t)
	for _, tc := range []struct {
		name string
		at   int64
	}{
		{"base", baseOffset},
		{"delta", deltaOffset},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader := readerOf(t, raw)
			incoming := newIndexer(t.Context(), IndexOptions{}, reader.Count())
			if err := incoming.receive(reader); err != nil {
				t.Fatalf("receive returned error %v", err)
			}
			incoming.pack = packOf(t, raw)
			incoming.pack.source = brokenReaderAt{data: raw, from: tc.at, to: tc.at + 1}

			if err := incoming.resolve(); !errors.Is(err, errRead) {
				t.Fatalf("resolve returned %v, want %v", err, errRead)
			}
		})
	}
}
