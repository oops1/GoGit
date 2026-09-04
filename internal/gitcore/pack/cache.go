package pack

import (
	"container/list"
	"sync"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
)

type cacheKey struct {
	pack   hash.ObjectID
	offset int64
}

type cacheItem struct {
	key  cacheKey
	kind object.Type
	data []byte
}

type Cache struct {
	mu    sync.Mutex
	limit int64
	used  int64
	order *list.List
	items map[cacheKey]*list.Element
}

func NewCache(limit int64) *Cache {
	if limit <= 0 {
		limit = DefaultCacheBytes
	}
	return &Cache{
		limit: limit,
		order: list.New(),
		items: make(map[cacheKey]*list.Element),
	}
}

func (c *Cache) Limit() int64 {
	return c.limit
}

func (c *Cache) Bytes() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.used
}

func (c *Cache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.items)
}

func (c *Cache) Purge() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.order.Init()
	clear(c.items)
	c.used = 0
}

func (c *Cache) get(key cacheKey) (object.Type, []byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	element, ok := c.items[key]
	if !ok {
		return 0, nil, false
	}
	c.order.MoveToFront(element)
	item := element.Value.(*cacheItem)
	return item.kind, item.data, true
}

func (c *Cache) put(key cacheKey, kind object.Type, data []byte) {
	size := int64(len(data))
	if size > c.limit {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if element, ok := c.items[key]; ok {
		c.order.MoveToFront(element)
		return
	}
	c.items[key] = c.order.PushFront(&cacheItem{key: key, kind: kind, data: data})
	c.used += size
	for c.used > c.limit {
		c.drop(c.order.Back())
	}
}

func (c *Cache) dropPack(id hash.ObjectID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for key, element := range c.items {
		if key.pack == id {
			c.drop(element)
		}
	}
}

func (c *Cache) drop(element *list.Element) {
	item := element.Value.(*cacheItem)
	c.order.Remove(element)
	delete(c.items, item.key)
	c.used -= int64(len(item.data))
}
