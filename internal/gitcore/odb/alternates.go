package odb

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/pack"
)

const (
	alternatesFile    = "info/alternates"
	alternatesComment = '#'
)

func parseAlternates(dir string, data []byte) []string {
	var paths []string
	for _, line := range strings.Split(string(data), "\n") {
		entry := strings.TrimSuffix(line, "\r")
		if entry == "" || entry[0] == alternatesComment {
			continue
		}
		paths = append(paths, resolveAlternate(dir, entry))
	}
	return paths
}

func resolveAlternate(dir, entry string) string {
	path := filepath.FromSlash(entry)
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(dir, path)
}

func (d *DB) alternatePaths(extra []string) ([]string, error) {
	paths := make([]string, 0, len(extra))
	for _, entry := range extra {
		paths = append(paths, resolveAlternate(d.dir, entry))
	}
	data, err := d.root.ReadFile(filepath.FromSlash(alternatesFile))
	if errors.Is(err, fs.ErrNotExist) {
		return paths, nil
	}
	if err != nil {
		return nil, fmt.Errorf("odb: read %s in %s: %w", alternatesFile, d.dir, err)
	}
	return append(paths, parseAlternates(d.dir, data)...), nil
}

type opener struct {
	opts  Options
	cache *objectCache
	packs *pack.Cache
	seen  map[string]struct{}
	stack map[string]struct{}
}

func newOpener(opts Options) *opener {
	return &opener{
		opts:  opts,
		cache: newObjectCache(opts.CacheBytes),
		packs: pack.NewCache(opts.PackBytes),
		seen:  make(map[string]struct{}),
		stack: make(map[string]struct{}),
	}
}

func (o *opener) open(dir string, extra []string, depth int) (*DB, error) {
	clean := filepath.Clean(dir)
	if _, ok := o.stack[clean]; ok {
		return nil, fmt.Errorf("%w: %s repeats in the chain", ErrAlternatesLoop, clean)
	}
	if depth > MaxAlternatesDepth {
		return nil, fmt.Errorf("%w: %s sits %d levels deep", ErrAlternatesLoop, clean, depth)
	}
	if _, ok := o.seen[clean]; ok {
		return nil, nil
	}
	o.seen[clean] = struct{}{}
	o.stack[clean] = struct{}{}
	defer delete(o.stack, clean)
	db, err := o.newDB(clean)
	if err != nil {
		return nil, err
	}
	if err := o.linkAlternates(db, extra, depth); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func (o *opener) newDB(dir string) (*DB, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("odb: open %s: %w", dir, err)
	}
	db := &DB{dir: dir, root: root, opts: o.opts, cache: o.cache, packCache: o.packs}
	if _, err := db.reloadPacks(); err != nil {
		_ = root.Close()
		return nil, err
	}
	return db, nil
}

func (o *opener) linkAlternates(db *DB, extra []string, depth int) error {
	paths, err := db.alternatePaths(extra)
	if err != nil {
		return err
	}
	for _, path := range paths {
		child, err := o.open(path, nil, depth+1)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if child != nil {
			db.alternates = append(db.alternates, child)
		}
	}
	return nil
}
