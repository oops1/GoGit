package odb

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
)

func packedBenchmarkDB(b *testing.B, opts Options) (*DB, []hash.ObjectID) {
	b.Helper()
	objects := newObjectsDir(b)
	copyFixturePacks(b, objects)
	ids := make([]hash.ObjectID, 0, len(packFixtureObjects(b)))
	for _, item := range packFixtureObjects(b) {
		ids = append(ids, item.id)
	}
	return openDB(b, objects, opts), ids
}

func BenchmarkGetFromPackWithAWarmCache(b *testing.B) {
	db, ids := packedBenchmarkDB(b, Options{})
	for _, id := range ids {
		if _, _, err := db.Get(id); err != nil {
			b.Fatalf("Get(%s) returned error %v", id, err)
		}
	}
	at := 0
	b.ReportAllocs()
	for b.Loop() {
		if _, _, err := db.Get(ids[at%len(ids)]); err != nil {
			b.Fatalf("Get returned error %v", err)
		}
		at++
	}
}

func BenchmarkGetFromPackWithAColdCache(b *testing.B) {
	db, ids := packedBenchmarkDB(b, Options{CacheBytes: 1, PackBytes: 1})
	at := 0
	b.ReportAllocs()
	for b.Loop() {
		if _, _, err := db.Get(ids[at%len(ids)]); err != nil {
			b.Fatalf("Get returned error %v", err)
		}
		at++
	}
}

func BenchmarkPutBlobOfOneMegabyte(b *testing.B) {
	db := openDB(b, newObjectsDir(b), Options{})
	payload := bytes.Repeat([]byte("gogit object database benchmark "), 1<<15)
	counter := uint64(0)
	b.SetBytes(int64(len(payload)))
	b.ReportAllocs()
	for b.Loop() {
		counter++
		binary.LittleEndian.PutUint64(payload, counter)
		if _, err := db.Put(object.TypeBlob, payload); err != nil {
			b.Fatalf("Put returned error %v", err)
		}
	}
}

func BenchmarkWeakCacheOfParsedCommits(b *testing.B) {
	cache := newWeakCache[object.Commit]()
	var id hash.ObjectID
	counter := uint64(0)
	b.ReportAllocs()
	for b.Loop() {
		counter++
		binary.LittleEndian.PutUint64(id[:], counter)
		cache.put(id, &object.Commit{Message: "m"})
		if cache.get(id) == nil {
			b.Fatal("the commit just stored is gone")
		}
	}
}

func BenchmarkGetFromPackWithAWarmCacheInParallel(b *testing.B) {
	db, ids := packedBenchmarkDB(b, Options{})
	for _, id := range ids {
		if _, _, err := db.Get(id); err != nil {
			b.Fatalf("Get(%s) returned error %v", id, err)
		}
	}
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		at := 0
		for pb.Next() {
			if _, _, err := db.Get(ids[at%len(ids)]); err != nil {
				b.Errorf("Get returned error %v", err)
				return
			}
			at++
		}
	})
}

func benchmarkRawCacheInParallel(b *testing.B, limit int64) {
	cache := newRawCache(limit)
	const entries = 4096
	ids := make([]hash.ObjectID, entries)
	for at := range ids {
		binary.LittleEndian.PutUint64(ids[at][4:], uint64(at)*2654435761)
		ids[at][0] = byte(at * 7)
		cache.put(ids[at], object.TypeBlob, []byte("payload"))
	}
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		at := 0
		for pb.Next() {
			if _, _, ok := cache.get(ids[at%entries]); !ok {
				b.Error("an entry is missing")
				return
			}
			at += 7
		}
	})
}

func BenchmarkRawCacheOfOneShardInParallel(b *testing.B) { benchmarkRawCacheInParallel(b, 1<<20) }

func BenchmarkRawCacheOfDefaultSizeInParallel(b *testing.B) {
	benchmarkRawCacheInParallel(b, DefaultCacheBytes)
}

func BenchmarkRawCacheOfSixteenShardsInParallel(b *testing.B) {
	benchmarkRawCacheInParallel(b, 16*rawShardBytes)
}
