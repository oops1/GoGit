package odb

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/pack"
)

func (d *DB) PackReuse() *pack.Reuse {
	return pack.NewReuse(d.packStores()...)
}

func (d *DB) packStores() []*pack.Store {
	var stores []*pack.Store
	if store := d.store(); store != nil {
		stores = append(stores, store)
	}
	for _, alternate := range d.alternates {
		stores = append(stores, alternate.packStores()...)
	}
	return stores
}

func (d *DB) ForgetPacks(names ...string) error {
	store := d.store()
	if store == nil {
		return nil
	}
	return store.Forget(names...)
}

func (d *DB) Stream(id hash.ObjectID) (object.Type, int64, io.ReadCloser, error) {
	if kind, data, ok := d.cache.raw.get(id); ok {
		return kind, int64(len(data)), io.NopCloser(bytes.NewReader(data)), nil
	}
	kind, size, reader, ok, err := d.stream(id)
	if !ok && err == nil {
		grown, reloadErr := d.Reload()
		if reloadErr != nil {
			return 0, 0, nil, reloadErr
		}
		if grown {
			kind, size, reader, ok, err = d.stream(id)
		}
	}
	if err != nil {
		return 0, 0, nil, err
	}
	if !ok {
		return 0, 0, nil, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	return kind, size, reader, nil
}

func (d *DB) stream(id hash.ObjectID) (object.Type, int64, io.ReadCloser, bool, error) {
	var packErr error
	if store := d.store(); store != nil {
		kind, size, reader, ok, err := store.Stream(id)
		if ok {
			return kind, size, reader, true, nil
		}
		packErr = err
	}
	kind, size, reader, ok, err := d.looseStream(id)
	if ok || err != nil {
		return kind, size, reader, ok, err
	}
	for _, alternate := range d.alternates {
		kind, size, reader, ok, err = alternate.stream(id)
		if ok || err != nil {
			return kind, size, reader, ok, err
		}
	}
	return 0, 0, nil, false, packErr
}

func (d *DB) looseStream(id hash.ObjectID) (object.Type, int64, io.ReadCloser, bool, error) {
	file, ok, err := d.looseFile(id)
	if !ok {
		return 0, 0, nil, false, err
	}
	decompressor, err := zlib.NewReader(file)
	if err != nil {
		_ = file.Close()
		return 0, 0, nil, false, fmt.Errorf("odb: read %s in %s: %w: %w", id, d.dir, object.ErrMalformed, err)
	}
	source := bufio.NewReader(decompressor)
	kind, size, err := object.ReadLooseHeader(source)
	if err != nil {
		_ = decompressor.Close()
		_ = file.Close()
		return 0, 0, nil, false, fmt.Errorf("odb: read %s in %s: %w", id, d.dir, err)
	}
	hasher, _ := hash.NewHasher(d.opts.Format, kind.String(), size)
	stream := &looseStream{id: id, dir: d.dir, file: file, decompressor: decompressor, source: source, hasher: hasher, remaining: size}
	return kind, size, stream, true, nil
}

type looseStream struct {
	id           hash.ObjectID
	dir          string
	file         *os.File
	decompressor io.ReadCloser
	source       *bufio.Reader
	hasher       *hash.Hasher
	remaining    int64
	finished     bool
	err          error
}

func (s *looseStream) Read(into []byte) (int, error) {
	if s.remaining == 0 {
		return 0, s.finish()
	}
	if int64(len(into)) > s.remaining {
		into = into[:s.remaining]
	}
	read, err := s.source.Read(into)
	_, _ = s.hasher.Write(into[:read])
	s.remaining -= int64(read)
	if err == nil || errors.Is(err, io.EOF) && s.remaining == 0 {
		return read, nil
	}
	if errors.Is(err, io.EOF) {
		err = io.ErrUnexpectedEOF
	}
	return read, fmt.Errorf("odb: read %s in %s: %w", s.id, s.dir, err)
}

func (s *looseStream) finish() error {
	if s.finished {
		return s.err
	}
	s.finished = true
	s.err = io.EOF
	if _, err := s.source.ReadByte(); !errors.Is(err, io.EOF) {
		s.err = fmt.Errorf("%w: %s in %s does not end after its declared size: %v", ErrCorrupt, s.id, s.dir, err)
		return s.err
	}
	if got, _ := s.hasher.Sum(); got != s.id {
		s.err = fmt.Errorf("%w: %s in %s holds %s", ErrCorrupt, s.id, s.dir, got)
	}
	return s.err
}

func (s *looseStream) Close() error {
	return errors.Join(s.decompressor.Close(), s.file.Close())
}
