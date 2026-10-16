package odb

import (
	"encoding/binary"
	"runtime"
	"testing"
	"weak"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
)

func numberedID(number uint64) hash.ObjectID {
	var id hash.ObjectID
	binary.LittleEndian.PutUint64(id[:], number)
	return id
}

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

func TestWeakCacheSweepsCollectedValuesOnceItGrowsPastItsThreshold(t *testing.T) {
	cache := newWeakCache[object.Blob]()
	for index := range 3 * weakSweepFloor {
		cache.put(numberedID(uint64(index)), &object.Blob{Data: []byte("garbage")})
		if index == weakSweepFloor {
			collect()
		}
	}

	cache.mu.Lock()
	held := len(cache.items)
	cache.mu.Unlock()
	if held >= 3*weakSweepFloor {
		t.Fatalf("the cache still holds %d entries, want the collected ones swept", held)
	}
}

func TestWeakCacheKeepsLiveValuesThroughASweep(t *testing.T) {
	cache := newWeakCache[object.Blob]()
	live := make([]*object.Blob, weakSweepFloor+10)
	for index := range live {
		live[index] = &object.Blob{Data: []byte("live")}
		cache.put(numberedID(uint64(index)), live[index])
	}

	if got := cache.len(); got != len(live) {
		t.Fatalf("the cache holds %d entries, want all %d live ones", got, len(live))
	}
	if cache.get(numberedID(5)) != live[5] {
		t.Fatal("a live value was lost in a sweep")
	}
	runtime.KeepAlive(live)
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

func TestRawCacheUsesOneShardPerTwoMegabytesUpToSixteen(t *testing.T) {
	tests := []struct {
		limit int64
		want  int
	}{
		{1, 1}, {rawShardBytes - 1, 1}, {rawShardBytes, 1}, {3 * rawShardBytes, 3},
		{DefaultCacheBytes, DefaultCacheBytes / rawShardBytes}, {1 << 40, rawShardLimit},
	}
	for _, tc := range tests {
		if got := len(newRawCache(tc.limit).shards); got != tc.want {
			t.Errorf("a limit of %d gives %d shards, want %d", tc.limit, got, tc.want)
		}
	}
}

func TestRawCacheSpreadsEntriesAcrossShardsAndSumsThem(t *testing.T) {
	cache := newRawCache(4 * rawShardBytes)
	for prefix := range byte(40) {
		cache.put(idFrom(t, prefix), object.TypeBlob, []byte("12345"))
	}

	used := 0
	for _, shard := range cache.shards {
		if shard.len() > 0 {
			used++
		}
	}
	if used != len(cache.shards) || cache.len() != 40 || cache.bytes() != 200 {
		t.Fatalf("%d shards hold %d entries and %d bytes, want 40 entries and 200 bytes in every shard", used, cache.len(), cache.bytes())
	}
	cache.purge()
	if cache.len() != 0 || cache.bytes() != 0 {
		t.Fatalf("cache holds %d entries and %d bytes after purge", cache.len(), cache.bytes())
	}
}

func TestRawCacheEvictsOnlyWithinTheShardThatIsFull(t *testing.T) {
	cache := newRawCache(2 * rawShardBytes)
	half := make([]byte, rawShardBytes*3/4)
	first, second := idFrom(t, 2), idFrom(t, 4)
	other := idFrom(t, 3)
	cache.put(first, object.TypeBlob, half)
	cache.put(other, object.TypeBlob, half)
	cache.put(second, object.TypeBlob, half)

	if _, _, ok := cache.get(first); ok {
		t.Fatal("the oldest entry of the full shard survived")
	}
	if _, _, ok := cache.get(second); !ok {
		t.Fatal("the newest entry of the full shard was lost")
	}
	if _, _, ok := cache.get(other); !ok {
		t.Fatal("an entry of another shard was evicted")
	}
}

func TestRawCacheRefusesAnEntryLargerThanItsShard(t *testing.T) {
	cache := newRawCache(4 * rawShardBytes)

	cache.put(idFrom(t, 1), object.TypeBlob, make([]byte, rawShardBytes+1))

	if cache.len() != 0 {
		t.Fatalf("cache holds %d entries, want the oversized one refused", cache.len())
	}
}
