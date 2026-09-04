package pack

import (
	"bytes"
	"slices"
	"testing"
)

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
