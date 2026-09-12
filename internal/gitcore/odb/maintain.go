package odb

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"iter"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/pack"
)

const (
	packIndexSuffix = ".idx"
	packTempPrefix  = "tmp_"
	incomingPrefix  = "incoming-"
	writableMode    = 0o644
	keepSuffix      = ".keep"
)

var packFileSuffixes = []string{".idx", ".pack", ".rev", ".bitmap", ".mtimes", ".promisor"}

var (
	rootChtimes = func(root *os.Root, name string, atime, mtime time.Time) error {
		return root.Chtimes(name, atime, mtime)
	}
	writePackFile = pack.WritePackFile
	pathChmod     = os.Chmod
	pathRemove    = os.Remove
)

type LooseObject struct {
	ID      hash.ObjectID
	Size    int64
	ModTime time.Time
}

type PackInfo struct {
	Name    string
	Objects int
	Size    int64
	ModTime time.Time
	Keep    bool
}

type TempFile struct {
	Path    string
	Size    int64
	ModTime time.Time
}

func (d *DB) LooseObjects() iter.Seq2[LooseObject, error] {
	return func(yield func(LooseObject, error) bool) {
		for id, err := range d.Loose() {
			if err != nil {
				if !yield(LooseObject{}, err) {
					return
				}
				continue
			}
			info, err := rootStat(d.root, looseName(id))
			if err != nil {
				if !yield(LooseObject{}, fmt.Errorf("odb: stat %s in %s: %w", id, d.dir, err)) {
					return
				}
				continue
			}
			if !yield(LooseObject{ID: id, Size: info.Size(), ModTime: info.ModTime()}, nil) {
				return
			}
		}
	}
}

func (d *DB) RemoveLoose(id hash.ObjectID) error {
	name := looseName(id)
	_ = rootChmod(d.root, name, writableMode)
	if err := rootRemove(d.root, name); err != nil {
		return fmt.Errorf("odb: remove %s from %s: %w", id, d.dir, err)
	}
	return nil
}

func (d *DB) RemoveEmptyFanouts() error {
	fanouts, err := d.readDir(".")
	if err != nil {
		return err
	}
	for _, fanout := range fanouts {
		if !fanout.IsDir() || len(fanout.Name()) != fanoutLength {
			continue
		}
		entries, err := d.readDir(fanout.Name())
		if err != nil {
			return err
		}
		if len(entries) > 0 {
			continue
		}
		if err := rootRemove(d.root, fanout.Name()); err != nil {
			return fmt.Errorf("odb: remove %s from %s: %w", fanout.Name(), d.dir, err)
		}
	}
	return nil
}

func (d *DB) TempFiles() ([]TempFile, error) {
	var out []TempFile
	for _, place := range []struct {
		dir      string
		prefixes []string
	}{
		{dir: ".", prefixes: []string{tempPrefix}},
		{dir: packDirName, prefixes: []string{packTempPrefix, incomingPrefix}},
	} {
		entries, err := d.readDir(place.dir)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if entry.IsDir() || !hasAnyPrefix(entry.Name(), place.prefixes) {
				continue
			}
			rel := path.Join(place.dir, entry.Name())
			info, err := rootStat(d.root, filepath.FromSlash(rel))
			if err != nil {
				return nil, fmt.Errorf("odb: stat %s in %s: %w", rel, d.dir, err)
			}
			out = append(out, TempFile{Path: rel, Size: info.Size(), ModTime: info.ModTime()})
		}
	}
	return out, nil
}

func (d *DB) RemoveTemp(rel string) error {
	name := filepath.FromSlash(rel)
	_ = rootChmod(d.root, name, writableMode)
	if err := rootRemove(d.root, name); err != nil {
		return fmt.Errorf("odb: remove %s from %s: %w", rel, d.dir, err)
	}
	return nil
}

func (d *DB) Packs() ([]PackInfo, error) {
	store := d.store()
	if store == nil {
		return nil, nil
	}
	var out []PackInfo
	for _, file := range store.Files() {
		rel := path.Join(packDirName, file.Name+packIndexSuffix)
		info, err := rootStat(d.root, filepath.FromSlash(rel))
		if err != nil {
			return nil, fmt.Errorf("odb: stat %s in %s: %w", rel, d.dir, err)
		}
		keep, err := d.hasFile(path.Join(packDirName, file.Name+keepSuffix))
		if err != nil {
			return nil, err
		}
		out = append(out, PackInfo{
			Name:    file.Name,
			Objects: file.Index.Count(),
			Size:    file.Pack.Size() + info.Size(),
			ModTime: file.ModTime,
			Keep:    keep,
		})
	}
	return out, nil
}

func (d *DB) Packed(id hash.ObjectID) (bool, error) {
	store := d.store()
	if store == nil {
		return false, nil
	}
	return store.Contains(id)
}

func (d *DB) PackedSince(since time.Time) iter.Seq[hash.ObjectID] {
	return func(yield func(hash.ObjectID) bool) {
		store := d.store()
		if store == nil {
			return
		}
		for _, file := range store.Files() {
			if file.ModTime.Before(since) {
				continue
			}
			for id := range file.Index.Objects() {
				if !yield(id) {
					return
				}
			}
		}
	}
}

func (d *DB) PackObjects(name string) iter.Seq[hash.ObjectID] {
	return func(yield func(hash.ObjectID) bool) {
		store := d.store()
		if store == nil {
			return
		}
		for _, file := range store.Files() {
			if file.Name != name {
				continue
			}
			for id := range file.Index.Objects() {
				if !yield(id) {
					return
				}
			}
			return
		}
	}
}

func (d *DB) PutLoose(kind object.Type, data []byte) (hash.ObjectID, error) {
	if !kind.Valid() {
		return hash.Zero, fmt.Errorf("%w: %d", object.ErrUnknownType, uint8(kind))
	}
	id := hash.SumSHA1(kind.String(), data)
	has, err := d.looseHas(id)
	if err != nil {
		return hash.Zero, err
	}
	if has {
		return id, nil
	}
	return id, d.writeLoose(id, kind, data)
}

func (d *DB) Touch(id hash.ObjectID, when time.Time) error {
	if err := rootChtimes(d.root, looseName(id), when, when); err != nil {
		return fmt.Errorf("odb: touch %s in %s: %w", id, d.dir, err)
	}
	return nil
}

func (d *DB) WritePack(ctx context.Context, ids []hash.ObjectID, opts pack.WriteOptions) (pack.IndexResult, error) {
	if err := rootMkdirAll(d.root, packDirName, looseDirMode); err != nil {
		return pack.IndexResult{}, fmt.Errorf("odb: create %s in %s: %w", packDirName, d.dir, err)
	}
	return writePackFile(ctx, d.PackDir(), d, ids, opts)
}

func RemovePackFiles(packDir, name string) error {
	var failures []error
	for _, suffix := range packFileSuffixes {
		file := filepath.Join(packDir, name+suffix)
		_ = pathChmod(file, writableMode)
		if err := pathRemove(file); err != nil && !errors.Is(err, fs.ErrNotExist) {
			failures = append(failures, fmt.Errorf("odb: remove %s: %w", file, err))
		}
	}
	return errors.Join(failures...)
}

func (d *DB) hasFile(rel string) (bool, error) {
	_, err := rootStat(d.root, filepath.FromSlash(rel))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("odb: stat %s in %s: %w", rel, d.dir, err)
	}
	return true, nil
}

func hasAnyPrefix(name string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}
