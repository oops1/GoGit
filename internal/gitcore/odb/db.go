package odb

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/pack"
)

const packDirName = "pack"

type DB struct {
	dir        string
	root       *os.Root
	opts       Options
	cache      *objectCache
	packCache  *pack.Cache
	alternates []*DB

	mu     sync.RWMutex
	packs  *pack.Store
	closed bool
}

func Open(objectsDir string, opts Options) (*DB, error) {
	normalized, err := opts.normalized()
	if err != nil {
		return nil, err
	}
	return newOpener(normalized).open(objectsDir, normalized.Alternates, 0)
}

func (d *DB) Dir() string { return d.dir }

func (d *DB) PackDir() string { return filepath.Join(d.dir, packDirName) }

func (d *DB) Format() hash.Format { return d.opts.Format }

func (d *DB) Alternates() []*DB { return d.alternates }

func (d *DB) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return nil
	}
	d.closed = true
	failures := make([]error, 0, len(d.alternates)+2)
	for _, alternate := range d.alternates {
		failures = append(failures, alternate.Close())
	}
	if d.packs != nil {
		failures = append(failures, d.packs.Close())
		d.packs = nil
	}
	failures = append(failures, d.root.Close())
	d.cache.purge()
	return errors.Join(failures...)
}

func (d *DB) Reload() (bool, error) {
	d.mu.Lock()
	changed, err := d.reloadPacks()
	d.mu.Unlock()
	if err != nil {
		return false, err
	}
	for _, alternate := range d.alternates {
		altered, err := alternate.Reload()
		if err != nil {
			return false, err
		}
		changed = changed || altered
	}
	return changed, nil
}

func (d *DB) reloadPacks() (bool, error) {
	if d.packs == nil {
		store, err := pack.Open(d.PackDir(), d.packOptions()...)
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		d.packs = store
		return true, nil
	}
	changed, err := d.packs.Reload()
	if err == nil {
		return changed, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return false, err
	}
	_ = d.packs.Close()
	d.packs = nil
	return true, nil
}

func (d *DB) packOptions() []pack.Option {
	return []pack.Option{
		pack.WithCache(d.packCache),
		pack.WithBaseResolver(d),
		pack.WithMaxDeltaDepth(d.opts.MaxDepth),
	}
}

func (d *DB) store() *pack.Store {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.packs
}

func (d *DB) Get(id hash.ObjectID) (object.Type, []byte, error) {
	if kind, data, ok := d.cache.raw.get(id); ok {
		return kind, data, nil
	}
	kind, data, ok, err := d.lookup(id)
	if err != nil {
		return 0, nil, err
	}
	if !ok {
		return 0, nil, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	d.cache.raw.put(id, kind, data)
	return kind, data, nil
}

func (d *DB) lookup(id hash.ObjectID) (object.Type, []byte, bool, error) {
	kind, data, ok, err := d.looseRead(id)
	if ok || err != nil {
		return kind, data, ok, err
	}
	if store := d.store(); store != nil {
		kind, data, ok, err = store.Get(id)
		if ok || err != nil {
			return kind, data, ok, err
		}
	}
	for _, alternate := range d.alternates {
		kind, data, ok, err = alternate.lookup(id)
		if ok || err != nil {
			return kind, data, ok, err
		}
	}
	return 0, nil, false, nil
}

func (d *DB) Has(id hash.ObjectID) (bool, error) {
	if _, _, ok := d.cache.raw.get(id); ok {
		return true, nil
	}
	return d.has(id)
}

func (d *DB) has(id hash.ObjectID) (bool, error) {
	ok, err := d.looseHas(id)
	if ok || err != nil {
		return ok, err
	}
	if store := d.store(); store != nil {
		ok, err = store.Contains(id)
		if ok || err != nil {
			return ok, err
		}
	}
	for _, alternate := range d.alternates {
		ok, err = alternate.has(id)
		if ok || err != nil {
			return ok, err
		}
	}
	return false, nil
}

func (d *DB) Type(id hash.ObjectID) (object.Type, error) {
	kind, _, err := d.Info(id)
	return kind, err
}

func (d *DB) Size(id hash.ObjectID) (int64, error) {
	_, size, err := d.Info(id)
	return size, err
}

func (d *DB) Info(id hash.ObjectID) (object.Type, int64, error) {
	if kind, data, ok := d.cache.raw.get(id); ok {
		return kind, int64(len(data)), nil
	}
	kind, size, ok, err := d.header(id)
	if err != nil {
		return 0, 0, err
	}
	if !ok {
		return 0, 0, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	return kind, size, nil
}

func (d *DB) header(id hash.ObjectID) (object.Type, int64, bool, error) {
	kind, size, ok, err := d.looseHeader(id)
	if ok || err != nil {
		return kind, size, ok, err
	}
	kind, size, ok, err = d.packHeader(id)
	if ok || err != nil {
		return kind, size, ok, err
	}
	for _, alternate := range d.alternates {
		kind, size, ok, err = alternate.header(id)
		if ok || err != nil {
			return kind, size, ok, err
		}
	}
	return 0, 0, false, nil
}

func (d *DB) packHeader(id hash.ObjectID) (object.Type, int64, bool, error) {
	store := d.store()
	if store == nil {
		return 0, 0, false, nil
	}
	for _, file := range store.Files() {
		offset, ok, err := file.Index.Lookup(id)
		if err != nil {
			return 0, 0, false, err
		}
		if !ok {
			continue
		}
		head, err := file.Pack.HeaderAt(offset)
		if err != nil {
			return 0, 0, false, err
		}
		if !head.Kind.IsDelta() {
			return head.Kind.Type(), head.Size, true, nil
		}
		kind, data, err := file.Pack.ObjectAt(offset)
		if err != nil {
			return 0, 0, false, err
		}
		return kind, int64(len(data)), true, nil
	}
	return 0, 0, false, nil
}

func (d *DB) ResolveBase(id hash.ObjectID, depth int) (object.Type, []byte, error) {
	if depth > d.opts.MaxDepth {
		return 0, nil, fmt.Errorf("%w: %d links reached at %s", pack.ErrDeltaChainTooDeep, depth, id)
	}
	return d.Get(id)
}

func (d *DB) raw(id hash.ObjectID, want object.Type) ([]byte, error) {
	kind, data, err := d.Get(id)
	if err != nil {
		return nil, err
	}
	if kind != want {
		return nil, fmt.Errorf("%w: %s is a %s, not a %s", ErrWrongType, id, kind, want)
	}
	return data, nil
}

func (d *DB) Commit(id hash.ObjectID) (*object.Commit, error) {
	if cached := d.cache.commits.get(id); cached != nil {
		return cached, nil
	}
	data, err := d.raw(id, object.TypeCommit)
	if err != nil {
		return nil, err
	}
	commit, err := object.ParseCommit(data)
	if err != nil {
		return nil, err
	}
	d.cache.commits.put(id, commit)
	return commit, nil
}

func (d *DB) Tree(id hash.ObjectID) (*object.Tree, error) {
	if cached := d.cache.trees.get(id); cached != nil {
		return cached, nil
	}
	data, err := d.raw(id, object.TypeTree)
	if err != nil {
		return nil, err
	}
	tree, err := object.ParseTree(data)
	if err != nil {
		return nil, err
	}
	d.cache.trees.put(id, tree)
	return tree, nil
}

func (d *DB) Blob(id hash.ObjectID) (*object.Blob, error) {
	data, err := d.raw(id, object.TypeBlob)
	if err != nil {
		return nil, err
	}
	return object.ParseBlob(data)
}

func (d *DB) Tag(id hash.ObjectID) (*object.Tag, error) {
	data, err := d.raw(id, object.TypeTag)
	if err != nil {
		return nil, err
	}
	return object.ParseTag(data)
}

func (d *DB) Peel(id hash.ObjectID) (object.Type, hash.ObjectID, error) {
	current := id
	for range MaxTagChain {
		kind, err := d.Type(current)
		if err != nil {
			return 0, hash.Zero, err
		}
		if kind != object.TypeTag {
			return kind, current, nil
		}
		tag, err := d.Tag(current)
		if err != nil {
			return 0, hash.Zero, err
		}
		current = tag.Object
	}
	return 0, hash.Zero, fmt.Errorf("%w: %s", ErrTagChainTooDeep, id)
}

func (d *DB) PeelTag(id hash.ObjectID) (hash.ObjectID, bool, error) {
	kind, err := d.Type(id)
	if err != nil {
		return hash.Zero, false, err
	}
	if kind != object.TypeTag {
		return hash.Zero, false, nil
	}
	_, target, err := d.Peel(id)
	if err != nil {
		return hash.Zero, false, err
	}
	return target, true, nil
}
