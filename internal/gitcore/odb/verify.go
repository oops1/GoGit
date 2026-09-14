package odb

import (
	"context"
	"fmt"
	"iter"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/pack"
)

func (d *DB) VerifyLoose(id hash.ObjectID) error {
	file, ok, err := d.looseFile(id)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	defer func() { _ = file.Close() }()
	if _, _, err := object.DecodeRawStream(file, id); err != nil {
		return fmt.Errorf("odb: verify %s in %s: %w", id, d.dir, err)
	}
	return nil
}

func (d *DB) VerifyPack(ctx context.Context, name string) iter.Seq2[hash.ObjectID, error] {
	return func(yield func(hash.ObjectID, error) bool) {
		file := d.packFile(name)
		if file == nil {
			yield(hash.Zero, fmt.Errorf("%w: pack %s", ErrNotFound, name))
			return
		}
		if err := file.Index.Verify(); err != nil && !yield(hash.Zero, err) {
			return
		}
		if err := file.Pack.Verify(); err != nil && !yield(hash.Zero, err) {
			return
		}
		for id := range file.Index.Objects() {
			if ctx.Err() != nil {
				return
			}
			offset, _ := file.Index.Find(id)
			kind, data, err := file.Pack.ObjectAt(offset)
			if err == nil {
				if got := hash.SumSHA1(kind.String(), data); got != id {
					err = fmt.Errorf("%w: %s in %s holds %s", ErrCorrupt, id, name, got)
				}
			}
			if err != nil && !yield(id, err) {
				return
			}
		}
	}
}

func (d *DB) packFile(name string) *pack.PackFile {
	store := d.store()
	if store == nil {
		return nil
	}
	for _, file := range store.Files() {
		if file.Name == name {
			return file
		}
	}
	return nil
}
