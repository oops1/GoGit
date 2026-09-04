package odb

import (
	"runtime"
	"testing"
	"weak"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
)

func idFrom(t testing.TB, prefix byte) hash.ObjectID {
	t.Helper()
	var id hash.ObjectID
	id[0] = prefix
	return id
}

func TestRawCacheServesStoredEntries(t *testing.T) {
	cache := newRawCache(1024)
	id := idFrom(t, 1)
	cache.put(id, object.TypeBlob, []byte("payload"))
	kind, data, ok := cache.get(id)
	if !ok || kind != object.TypeBlob || string(data) != "payload" {
		t.Fatalf("get gave (%s, %q, %v)", kind, data, ok)
	}
	if cache.len() != 1 || cache.bytes() != int64(len("payload")) {
		t.Fatalf("cache holds %d entries and %d bytes", cache.len(), cache.bytes())
	}
}

func TestRawCacheReportsMissingEntries(t *testing.T) {
	cache := newRawCache(1024)
	if _, _, ok := cache.get(idFrom(t, 2)); ok {
		t.Fatal("get found an entry that was never stored")
	}
}

func TestRawCacheKeepsOneCopyOfRepeatedEntries(t *testing.T) {
	cache := newRawCache(1024)
	id := idFrom(t, 3)
	cache.put(id, object.TypeBlob, []byte("abcd"))
	cache.put(id, object.TypeBlob, []byte("abcd"))
	if cache.len() != 1 || cache.bytes() != 4 {
		t.Fatalf("cache holds %d entries and %d bytes", cache.len(), cache.bytes())
	}
}

func TestRawCacheEvictsLeastRecentlyUsedEntries(t *testing.T) {
	cache := newRawCache(8)
	first, second, third := idFrom(t, 4), idFrom(t, 5), idFrom(t, 6)
	cache.put(first, object.TypeBlob, []byte("1234"))
	cache.put(second, object.TypeBlob, []byte("5678"))
	if _, _, ok := cache.get(first); !ok {
		t.Fatal("the first entry left the cache too early")
	}
	cache.put(third, object.TypeBlob, []byte("9012"))
	if _, _, ok := cache.get(second); ok {
		t.Fatal("the least recently used entry survived")
	}
	if _, _, ok := cache.get(first); !ok {
		t.Fatal("the recently used entry was evicted")
	}
}

func TestRawCacheRefusesEntriesLargerThanTheLimit(t *testing.T) {
	cache := newRawCache(4)
	cache.put(idFrom(t, 7), object.TypeBlob, []byte("far too long"))
	if cache.len() != 0 {
		t.Fatalf("cache holds %d entries", cache.len())
	}
}

func TestRawCachePurgeEmptiesEverything(t *testing.T) {
	cache := newRawCache(1024)
	cache.put(idFrom(t, 8), object.TypeBlob, []byte("payload"))
	cache.purge()
	if cache.len() != 0 || cache.bytes() != 0 {
		t.Fatalf("cache holds %d entries and %d bytes", cache.len(), cache.bytes())
	}
}

func TestWeakCacheServesLiveValues(t *testing.T) {
	cache := newWeakCache[object.Blob]()
	id := idFrom(t, 9)
	blob := &object.Blob{Data: []byte("live")}
	cache.put(id, blob)
	if got := cache.get(id); got != blob {
		t.Fatalf("get gave %v, want the stored value", got)
	}
	runtime.KeepAlive(blob)
}

func TestWeakCacheReportsMissingValues(t *testing.T) {
	cache := newWeakCache[object.Blob]()
	if got := cache.get(idFrom(t, 10)); got != nil {
		t.Fatalf("get gave %v for an unknown name", got)
	}
}

func TestWeakCacheForgetsCollectedValues(t *testing.T) {
	cache := newWeakCache[object.Blob]()
	id := idFrom(t, 11)
	cache.put(id, &object.Blob{Data: []byte("garbage")})
	collect()
	if got := cache.get(id); got != nil {
		t.Fatalf("get gave %v after collection", got)
	}
	if cache.len() != 0 {
		t.Fatalf("cache holds %d entries after collection", cache.len())
	}
}

func TestWeakCacheDropKeepsLiveValues(t *testing.T) {
	cache := newWeakCache[object.Blob]()
	id := idFrom(t, 12)
	blob := &object.Blob{Data: []byte("live")}
	cache.put(id, blob)
	cache.drop(idFrom(t, 13))
	cache.drop(id)
	if cache.len() != 1 {
		t.Fatalf("cache holds %d entries, want 1", cache.len())
	}
	runtime.KeepAlive(blob)
}

func TestWeakCacheDropRemovesCollectedValues(t *testing.T) {
	cache := newWeakCache[object.Blob]()
	id := idFrom(t, 14)
	cache.put(id, &object.Blob{Data: []byte("garbage")})
	collect()
	cache.drop(id)
	if cache.len() != 0 {
		t.Fatalf("cache holds %d entries after drop", cache.len())
	}
}

func TestWeakCachePurgeEmptiesEverything(t *testing.T) {
	cache := newWeakCache[object.Blob]()
	blob := &object.Blob{Data: []byte("live")}
	cache.put(idFrom(t, 15), blob)
	cache.purge()
	if cache.len() != 0 {
		t.Fatalf("cache holds %d entries after purge", cache.len())
	}
	runtime.KeepAlive(blob)
}

func TestObjectCachePurgeClearsEveryLayer(t *testing.T) {
	cache := newObjectCache(1024)
	blob := &object.Blob{Data: []byte("live")}
	cache.raw.put(idFrom(t, 16), object.TypeBlob, []byte("payload"))
	cache.commits.put(idFrom(t, 17), &object.Commit{})
	cache.trees.put(idFrom(t, 18), &object.Tree{})
	cache.purge()
	if cache.raw.len() != 0 || cache.commits.len() != 0 || cache.trees.len() != 0 {
		t.Fatalf("cache holds %d raw, %d commits and %d trees",
			cache.raw.len(), cache.commits.len(), cache.trees.len())
	}
	runtime.KeepAlive(blob)
}

func collect() {
	for range 4 {
		runtime.GC()
	}
}

func storeWeak[T any](cache *weakCache[T], id hash.ObjectID, value *T) {
	cache.items[id] = weak.Make(value)
}

func TestWeakCacheForgetsCollectedValuesOnRead(t *testing.T) {
	cache := newWeakCache[object.Blob]()
	id := idFrom(t, 19)
	storeWeak(cache, id, &object.Blob{Data: []byte("garbage")})
	collect()
	if got := cache.get(id); got != nil {
		t.Fatalf("get gave %v after collection", got)
	}
	if cache.len() != 0 {
		t.Fatalf("cache holds %d entries after a read", cache.len())
	}
}
