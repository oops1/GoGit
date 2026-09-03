package pack

import (
	"bytes"
	"sync"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
)

func TestNewCacheFallsBackToTheDefaultLimit(t *testing.T) {
	if got := NewCache(0).Limit(); got != DefaultCacheBytes {
		t.Fatalf("Limit = %d, want %d", got, DefaultCacheBytes)
	}
	if got := NewCache(64).Limit(); got != 64 {
		t.Fatalf("Limit = %d, want 64", got)
	}
}

func TestCacheKeepsObjectsUntilTheLimitIsReached(t *testing.T) {
	cache := NewCache(20)
	first := cacheKey{pack: idOfByte(1), offset: 1}
	second := cacheKey{pack: idOfByte(1), offset: 2}
	cache.put(first, object.TypeBlob, bytes.Repeat([]byte{'a'}, 10))
	cache.put(second, object.TypeTree, bytes.Repeat([]byte{'b'}, 10))
	if cache.Len() != 2 || cache.Bytes() != 20 {
		t.Fatalf("the cache holds %d objects of %d bytes, want 2 of 20", cache.Len(), cache.Bytes())
	}
	kind, data, ok := cache.get(first)
	if !ok || kind != object.TypeBlob || len(data) != 10 {
		t.Fatalf("get returned (%s, %d bytes, %v)", kind, len(data), ok)
	}
	third := cacheKey{pack: idOfByte(1), offset: 3}
	cache.put(third, object.TypeBlob, bytes.Repeat([]byte{'c'}, 10))
	if _, _, ok := cache.get(second); ok {
		t.Fatal("the cache evicted the object used last instead of the oldest one")
	}
	if _, _, ok := cache.get(first); !ok {
		t.Fatal("the cache dropped the object used last")
	}
	if cache.Bytes() != 20 {
		t.Fatalf("the cache holds %d bytes, want 20", cache.Bytes())
	}
}

func TestCacheSkipsObjectsLargerThanTheLimit(t *testing.T) {
	cache := NewCache(8)
	key := cacheKey{pack: idOfByte(2), offset: 1}
	cache.put(key, object.TypeBlob, bytes.Repeat([]byte{'a'}, 9))
	if _, _, ok := cache.get(key); ok {
		t.Fatal("the cache kept an object larger than its limit")
	}
	if cache.Len() != 0 {
		t.Fatalf("the cache holds %d objects, want none", cache.Len())
	}
}

func TestCacheIgnoresRepeatedPuts(t *testing.T) {
	cache := NewCache(64)
	key := cacheKey{pack: idOfByte(3), offset: 1}
	cache.put(key, object.TypeBlob, []byte("first"))
	cache.put(key, object.TypeTree, []byte("second"))
	kind, data, ok := cache.get(key)
	if !ok || kind != object.TypeBlob || string(data) != "first" {
		t.Fatalf("get returned (%s, %q, %v)", kind, data, ok)
	}
	if cache.Bytes() != 5 {
		t.Fatalf("the cache holds %d bytes, want 5", cache.Bytes())
	}
}

func TestCacheMissesUnknownKeys(t *testing.T) {
	cache := NewCache(64)
	if _, _, ok := cache.get(cacheKey{pack: idOfByte(4), offset: 9}); ok {
		t.Fatal("get found an object that was never stored")
	}
}

func TestPurgeEmptiesTheCache(t *testing.T) {
	cache := NewCache(64)
	cache.put(cacheKey{pack: idOfByte(5), offset: 1}, object.TypeBlob, []byte("data"))
	cache.Purge()
	if cache.Len() != 0 || cache.Bytes() != 0 {
		t.Fatalf("the cache holds %d objects of %d bytes after Purge", cache.Len(), cache.Bytes())
	}
}

func TestDropPackRemovesOnlyOnePackfile(t *testing.T) {
	cache := NewCache(64)
	kept := cacheKey{pack: idOfByte(6), offset: 1}
	cache.put(kept, object.TypeBlob, []byte("kept"))
	cache.put(cacheKey{pack: idOfByte(7), offset: 1}, object.TypeBlob, []byte("dropped"))
	cache.dropPack(idOfByte(7))
	if cache.Len() != 1 || cache.Bytes() != 4 {
		t.Fatalf("the cache holds %d objects of %d bytes, want 1 of 4", cache.Len(), cache.Bytes())
	}
	if _, _, ok := cache.get(kept); !ok {
		t.Fatal("dropPack removed an object of another packfile")
	}
}

func TestCacheServesManyGoroutines(t *testing.T) {
	cache := NewCache(1 << 12)
	var workers sync.WaitGroup
	for worker := range 8 {
		workers.Go(func() {
			for step := range 200 {
				key := cacheKey{pack: idOfByte(byte(worker)), offset: int64(step % 32)}
				cache.put(key, object.TypeBlob, bytes.Repeat([]byte{byte(worker)}, 64))
				cache.get(key)
			}
		})
	}
	workers.Wait()
	if cache.Bytes() > cache.Limit() {
		t.Fatalf("the cache holds %d bytes, more than its limit of %d", cache.Bytes(), cache.Limit())
	}
}

func TestCacheKeysSeparatePackfiles(t *testing.T) {
	cache := NewCache(64)
	var first, second hash.ObjectID
	first[0] = 1
	second[0] = 2
	cache.put(cacheKey{pack: first, offset: 8}, object.TypeBlob, []byte("one"))
	cache.put(cacheKey{pack: second, offset: 8}, object.TypeBlob, []byte("two"))
	_, data, ok := cache.get(cacheKey{pack: second, offset: 8})
	if !ok || string(data) != "two" {
		t.Fatalf("get returned (%q, %v)", data, ok)
	}
}
