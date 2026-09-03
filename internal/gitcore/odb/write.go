package odb

import (
	"bytes"
	"compress/zlib"
	"crypto/rand"
	"fmt"
	"os"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
)

type rootFile struct {
	file *os.File
}

func (f rootFile) Write(chunk []byte) (int, error) {
	return fileWrite(f.file, chunk)
}

type looseTemp struct {
	db   *DB
	name string
	file *os.File
}

func (d *DB) createTemp() (*looseTemp, error) {
	name := tempPrefix + rand.Text()
	file, err := rootCreate(d.root, name, looseTempMode)
	if err != nil {
		return nil, fmt.Errorf("odb: create %s in %s: %w", name, d.dir, err)
	}
	return &looseTemp{db: d, name: name, file: file}, nil
}

func (t *looseTemp) abort() {
	_ = t.file.Close()
	_ = rootRemove(t.db.root, t.name)
}

func (t *looseTemp) commit(id hash.ObjectID) error {
	err := t.file.Close()
	if err == nil {
		err = rootChmod(t.db.root, t.name, looseFileMode)
	}
	if err == nil {
		err = rootMkdirAll(t.db.root, fanoutOf(id), looseDirMode)
	}
	if err == nil {
		err = rootRename(t.db.root, t.name, looseName(id))
	}
	if err != nil {
		_ = rootRemove(t.db.root, t.name)
		return fmt.Errorf("odb: store %s in %s: %w", id, t.db.dir, err)
	}
	return nil
}

func compressLoose(kind object.Type, data []byte) []byte {
	var buffer bytes.Buffer
	compressor := zlib.NewWriter(&buffer)
	_, _ = compressor.Write(object.EncodeLooseRaw(kind, data))
	_ = compressor.Close()
	return buffer.Bytes()
}

func (d *DB) Put(kind object.Type, data []byte) (hash.ObjectID, error) {
	if !kind.Valid() {
		return hash.Zero, fmt.Errorf("%w: %d", object.ErrUnknownType, uint8(kind))
	}
	id := hash.SumSHA1(kind.String(), data)
	known, err := d.Has(id)
	if err != nil {
		return hash.Zero, err
	}
	if known {
		return id, nil
	}
	if err := d.writeLoose(id, kind, data); err != nil {
		return hash.Zero, err
	}
	return id, nil
}

func (d *DB) PutObject(obj object.Object) (hash.ObjectID, error) {
	return d.Put(obj.Type(), obj.Encode())
}

func (d *DB) writeLoose(id hash.ObjectID, kind object.Type, data []byte) error {
	temp, err := d.createTemp()
	if err != nil {
		return err
	}
	if _, err := fileWrite(temp.file, compressLoose(kind, data)); err != nil {
		temp.abort()
		return fmt.Errorf("odb: store %s in %s: %w", id, d.dir, err)
	}
	return temp.commit(id)
}

type ObjectWriter struct {
	db         *DB
	temp       *looseTemp
	compressor *zlib.Writer
	hasher     *hash.Hasher
	id         hash.ObjectID
	done       bool
}

func (d *DB) Writer(kind object.Type, size int64) (*ObjectWriter, error) {
	if !kind.Valid() {
		return nil, fmt.Errorf("%w: %d", object.ErrUnknownType, uint8(kind))
	}
	hasher, err := hash.NewHasher(d.opts.Format, kind.String(), size)
	if err != nil {
		return nil, err
	}
	temp, err := d.createTemp()
	if err != nil {
		return nil, err
	}
	compressor := zlib.NewWriter(rootFile{file: temp.file})
	_, _ = compressor.Write(hash.Header(kind.String(), size))
	return &ObjectWriter{db: d, temp: temp, compressor: compressor, hasher: hasher}, nil
}

func (w *ObjectWriter) ID() hash.ObjectID {
	return w.id
}

func (w *ObjectWriter) Write(chunk []byte) (int, error) {
	if w.done {
		return 0, ErrWriterClosed
	}
	written, err := w.hasher.Write(chunk)
	if err != nil {
		return 0, err
	}
	if _, err := w.compressor.Write(chunk[:written]); err != nil {
		return 0, fmt.Errorf("odb: store object in %s: %w", w.db.dir, err)
	}
	return written, nil
}

func (w *ObjectWriter) Close() error {
	if w.done {
		return nil
	}
	w.done = true
	id, err := w.hasher.Sum()
	if err == nil {
		err = w.compressor.Close()
	}
	if err != nil {
		w.temp.abort()
		return fmt.Errorf("odb: store object in %s: %w", w.db.dir, err)
	}
	w.id = id
	known, err := w.db.Has(id)
	if err != nil {
		w.temp.abort()
		return err
	}
	if known {
		w.temp.abort()
		return nil
	}
	return w.temp.commit(id)
}
