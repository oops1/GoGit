package pack

import (
	"bytes"
	"os"
	"slices"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
)

func reversedRefDeltaChain(b *testing.B, length int) []byte {
	b.Helper()
	contents := make([][]byte, length+1)
	contents[0] = bytes.Repeat([]byte("index pack benchmark base line\n"), 32)
	for step := 1; step <= length; step++ {
		contents[step] = append(slices.Clone(contents[step-1]), byte(step))
	}
	builder := newPackBuilder()
	for step := length; step >= 1; step-- {
		base := contents[step-1]
		size := int64(len(base))
		delta := slices.Concat(deltaSizes(size, size+1), copyOp(0, uint32(size)), insertOp([]byte{byte(step)}))
		builder.addRefDelta(b, hash.SumSHA1(object.TypeBlob.String(), base), delta)
	}
	builder.addObject(b, KindBlob, contents[0])
	return builder.bytes()
}

func benchmarkIndexPack(b *testing.B, raw []byte) {
	b.Helper()
	root := b.TempDir()
	b.SetBytes(int64(len(raw)))
	b.ReportAllocs()
	for b.Loop() {
		dir, err := os.MkdirTemp(root, "run")
		if err != nil {
			b.Fatal(err)
		}
		if _, err := IndexPack(b.Context(), bytes.NewReader(raw), dir, IndexOptions{}); err != nil {
			b.Fatalf("IndexPack returned error %v", err)
		}
	}
}

func BenchmarkIndexPackReversedRefDeltaChain(b *testing.B) {
	benchmarkIndexPack(b, reversedRefDeltaChain(b, 1000))
}

func BenchmarkIndexPackGitPack(b *testing.B) {
	benchmarkIndexPack(b, readFixture(b, fixturePackPath(b, fixtureName(b, offsetPack))))
}

func BenchmarkIndexFind(b *testing.B) {
	index := openFixtureIndex(b, fixtureName(b, offsetPack))
	ids := slices.Collect(index.Objects())
	if len(ids) == 0 {
		b.Fatal("the index holds no objects")
	}
	at := 0
	b.ReportAllocs()
	for b.Loop() {
		id := ids[at%len(ids)]
		if _, ok := index.Find(id); !ok {
			b.Fatalf("Find(%s) found nothing", id)
		}
		at++
	}
}

func BenchmarkObjectAtDeltaChain(b *testing.B) {
	builder := newPackBuilder()
	base := bytes.Repeat([]byte("delta chain benchmark payload "), 64)
	offset := builder.addObject(b, KindBlob, base)
	size := int64(len(base))
	for step := range 10 {
		delta := slices.Concat(deltaSizes(size, size+1), copyOp(0, uint32(size)), insertOp([]byte{byte(step)}))
		size++
		offset = builder.addOffsetDelta(b, offset, delta)
	}
	raw := builder.bytes()
	packfile, err := NewPack(bytes.NewReader(raw), int64(len(raw)), WithCache(NewCache(1)))
	if err != nil {
		b.Fatalf("NewPack returned error %v", err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, _, err := packfile.ObjectAt(offset); err != nil {
			b.Fatalf("ObjectAt returned error %v", err)
		}
	}
}

func BenchmarkReaderWalksPackfile(b *testing.B) {
	raw := readFixture(b, fixturePackPath(b, fixtureName(b, offsetPack)))
	b.SetBytes(int64(len(raw)))
	b.ReportAllocs()
	for b.Loop() {
		reader, err := NewReader(bytes.NewReader(raw))
		if err != nil {
			b.Fatalf("NewReader returned error %v", err)
		}
		for {
			if _, err := reader.NextObject(); err != nil {
				break
			}
		}
	}
}

func BenchmarkStoreGet(b *testing.B) {
	store, err := Open(packsDir)
	if err != nil {
		b.Fatalf("Open returned error %v", err)
	}
	b.Cleanup(func() { _ = store.Close() })
	ids := slices.Collect(store.Objects())
	at := 0
	b.ReportAllocs()
	for b.Loop() {
		id := ids[at%len(ids)]
		if _, _, ok, err := store.Get(id); err != nil || !ok {
			b.Fatalf("Get(%s) returned (%v, %v)", id, ok, err)
		}
		at++
	}
}
