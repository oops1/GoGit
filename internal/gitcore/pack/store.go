package pack

import (
	"fmt"
	"iter"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
)

type PackFile struct {
	Name    string
	ModTime time.Time
	Index   *Index
	Pack    *Pack
}

type Store struct {
	dir      string
	settings settings
	mu       sync.RWMutex
	files    []*PackFile
}

type candidate struct {
	name    string
	modTime time.Time
}

func Open(dir string, opts ...Option) (*Store, error) {
	store := &Store{dir: dir, settings: newSettings(opts)}
	if _, err := store.Reload(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *Store) Dir() string {
	return s.dir
}

func (s *Store) Files() []*PackFile {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return slices.Clone(s.files)
}

func (s *Store) Count() int {
	total := 0
	for _, file := range s.snapshot() {
		total += file.Index.Count()
	}
	return total
}

func (s *Store) Objects() iter.Seq[hash.ObjectID] {
	return func(yield func(hash.ObjectID) bool) {
		seen := make(map[hash.ObjectID]struct{})
		for _, file := range s.snapshot() {
			for id := range file.Index.Objects() {
				if _, ok := seen[id]; ok {
					continue
				}
				seen[id] = struct{}{}
				if !yield(id) {
					return
				}
			}
		}
	}
}

func (s *Store) Contains(id hash.ObjectID) (bool, error) {
	for _, file := range s.snapshot() {
		_, ok, err := file.Index.Position(id)
		if err != nil {
			return false, err
		}
		if ok {
			return true, nil
		}
	}
	return false, nil
}

func (s *Store) Get(id hash.ObjectID) (object.Type, []byte, bool, error) {
	return s.get(id, 0)
}

func (s *Store) ResolveBase(id hash.ObjectID, depth int) (object.Type, []byte, error) {
	if depth > s.settings.maxDepth {
		return 0, nil, fmt.Errorf("%w: %d links reached at %s", ErrDeltaChainTooDeep, depth, id)
	}
	kind, data, ok, err := s.get(id, depth)
	if err != nil {
		return 0, nil, err
	}
	if ok {
		return kind, data, nil
	}
	if s.settings.bases != nil {
		return s.settings.bases.ResolveBase(id, depth)
	}
	return 0, nil, fmt.Errorf("%w: %s", ErrBaseNotFound, id)
}

func (s *Store) Verify() error {
	for _, file := range s.snapshot() {
		if err := file.Index.Verify(); err != nil {
			return err
		}
		if err := file.Pack.Verify(); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) Reload() (bool, error) {
	found, err := s.scan()
	if err != nil {
		return false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := make(map[string]*PackFile, len(s.files))
	for _, file := range s.files {
		kept[file.Name] = file
	}
	files := make([]*PackFile, 0, len(found))
	var opened []*PackFile
	changed := false
	for _, wanted := range found {
		if file, ok := kept[wanted.name]; ok && file.ModTime.Equal(wanted.modTime) {
			delete(kept, wanted.name)
			files = append(files, file)
			continue
		}
		file, err := s.openPair(wanted)
		if err != nil {
			for _, stale := range opened {
				s.release(stale)
			}
			return false, err
		}
		opened = append(opened, file)
		files = append(files, file)
		changed = true
	}
	for _, stale := range kept {
		s.release(stale)
		changed = true
	}
	s.files = files
	return changed, nil
}

func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var failure error
	for _, file := range s.files {
		s.settings.cache.dropPack(file.Pack.Checksum())
		if err := file.Index.Close(); err != nil && failure == nil {
			failure = err
		}
		if err := file.Pack.Close(); err != nil && failure == nil {
			failure = err
		}
	}
	s.files = nil
	return failure
}

func (s *Store) get(id hash.ObjectID, depth int) (object.Type, []byte, bool, error) {
	for _, file := range s.snapshot() {
		offset, ok, err := file.Index.Lookup(id)
		if err != nil {
			return 0, nil, false, err
		}
		if !ok {
			continue
		}
		kind, data, err := file.Pack.objectAt(offset, depth)
		if err != nil {
			return 0, nil, false, err
		}
		return kind, data, true, nil
	}
	return 0, nil, false, nil
}

func (s *Store) snapshot() []*PackFile {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.files
}

func (s *Store) release(file *PackFile) {
	s.settings.cache.dropPack(file.Pack.Checksum())
	_ = file.Index.Close()
	_ = file.Pack.Close()
}

func (s *Store) openPair(wanted candidate) (*PackFile, error) {
	index, err := OpenIndex(filepath.Join(s.dir, wanted.name+indexSuffix))
	if err != nil {
		return nil, err
	}
	packfile, err := OpenPack(filepath.Join(s.dir, wanted.name+packSuffix),
		WithCache(s.settings.cache),
		WithIndex(index),
		WithBaseResolver(s),
		WithMaxDeltaDepth(s.settings.maxDepth))
	if err != nil {
		_ = index.Close()
		return nil, err
	}
	if index.PackHash() != packfile.Checksum() {
		_ = index.Close()
		_ = packfile.Close()
		return nil, fmt.Errorf("%w: %s declares %s, packfile holds %s",
			ErrPackMismatch, wanted.name, index.PackHash(), packfile.Checksum())
	}
	return &PackFile{Name: wanted.name, ModTime: wanted.modTime, Index: index, Pack: packfile}, nil
}

func (s *Store) scan() ([]candidate, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, fmt.Errorf("pack: read %s: %w", s.dir, err)
	}
	var found []candidate
	for _, entry := range entries {
		name, ok := strings.CutSuffix(entry.Name(), packSuffix)
		if !ok {
			continue
		}
		info, err := os.Stat(filepath.Join(s.dir, name+indexSuffix))
		if err != nil {
			continue
		}
		found = append(found, candidate{name: name, modTime: info.ModTime()})
	}
	slices.SortFunc(found, func(a, b candidate) int {
		if order := b.modTime.Compare(a.modTime); order != 0 {
			return order
		}
		return strings.Compare(a.name, b.name)
	})
	return found, nil
}
