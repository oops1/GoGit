package odb

import (
	"container/list"
	"runtime"
	"sync"
	"weak"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
)

type rawEntry struct {
	id   hash.ObjectID
	kind object.Type
	data []byte
}

type rawCache struct {
	mu    sync.Mutex
	limit int64
	used  int64
	order *list.List
	items map[hash.ObjectID]*list.Element
}

func newRawCache(limit int64) *rawCache {
	return &rawCache{
		limit: limit,
		order: list.New(),
		items: make(map[hash.ObjectID]*list.Element),
	}
}

func (c *rawCache) get(id hash.ObjectID) (object.Type, []byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	element, ok := c.items[id]
	if !ok {
		return 0, nil, false
	}
	c.order.MoveToFront(element)
	entry := element.Value.(*rawEntry)
	return entry.kind, entry.data, true
}

func (c *rawCache) put(id hash.ObjectID, kind object.Type, data []byte) {
	size := int64(len(data))
	if size > c.limit {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if element, ok := c.items[id]; ok {
		c.order.MoveToFront(element)
		return
	}
	c.items[id] = c.order.PushFront(&rawEntry{id: id, kind: kind, data: data})
	c.used += size
	for c.used > c.limit {
		c.evict(c.order.Back())
	}
}

func (c *rawCache) evict(element *list.Element) {
	entry := element.Value.(*rawEntry)
	c.order.Remove(element)
	delete(c.items, entry.id)
	c.used -= int64(len(entry.data))
}

func (c *rawCache) bytes() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.used
}

func (c *rawCache) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.items)
}

func (c *rawCache) purge() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.order.Init()
	clear(c.items)
	c.used = 0
}

type weakCache[T any] struct {
	mu    sync.Mutex
	items map[hash.ObjectID]weak.Pointer[T]
}

func newWeakCache[T any]() *weakCache[T] {
	return &weakCache[T]{items: make(map[hash.ObjectID]weak.Pointer[T])}
}

func (c *weakCache[T]) get(id hash.ObjectID) *T {
	c.mu.Lock()
	defer c.mu.Unlock()
	pointer, ok := c.items[id]
	if !ok {
		return nil
	}
	value := pointer.Value()
	if value == nil {
		delete(c.items, id)
	}
	return value
}

func (c *weakCache[T]) put(id hash.ObjectID, value *T) {
	c.mu.Lock()
	c.items[id] = weak.Make(value)
	c.mu.Unlock()
	runtime.AddCleanup(value, c.drop, id)
}

func (c *weakCache[T]) drop(id hash.ObjectID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if pointer, ok := c.items[id]; ok && pointer.Value() == nil {
		delete(c.items, id)
	}
}

func (c *weakCache[T]) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.items)
}

func (c *weakCache[T]) purge() {
	c.mu.Lock()
	defer c.mu.Unlock()
	clear(c.items)
}

type objectCache struct {
	raw     *rawCache
	commits *weakCache[object.Commit]
	trees   *weakCache[object.Tree]
}

func newObjectCache(limit int64) *objectCache {
	return &objectCache{
		raw:     newRawCache(limit),
		commits: newWeakCache[object.Commit](),
		trees:   newWeakCache[object.Tree](),
	}
}

func (c *objectCache) purge() {
	c.raw.purge()
	c.commits.purge()
	c.trees.purge()
}
