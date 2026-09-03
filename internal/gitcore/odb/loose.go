package odb

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
)

const (
	fanoutLength     = 2
	nameLength       = hash.HexSize - fanoutLength
	looseHeaderLimit = 64
	loosePrealloc    = 1 << 20
	looseDirMode     = 0o755
	looseFileMode    = 0o444
	looseTempMode    = 0o644
	tempPrefix       = "tmp_obj_"
)

var ErrHeaderTooLong = errors.New("odb: loose object header is too long")

var (
	rootOpen     = func(root *os.Root, name string) (*os.File, error) { return root.Open(name) }
	rootStat     = func(root *os.Root, name string) (fs.FileInfo, error) { return root.Stat(name) }
	rootMkdirAll = func(root *os.Root, name string, perm fs.FileMode) error { return root.MkdirAll(name, perm) }
	rootCreate   = func(root *os.Root, name string, perm fs.FileMode) (*os.File, error) {
		return root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	}
	rootRename = func(root *os.Root, from, to string) error { return root.Rename(from, to) }
	rootChmod  = func(root *os.Root, name string, mode fs.FileMode) error { return root.Chmod(name, mode) }
	rootRemove = func(root *os.Root, name string) error { return root.Remove(name) }
	fileWrite  = func(file *os.File, data []byte) (int, error) { return file.Write(data) }
)

func fanoutOf(id hash.ObjectID) string {
	return id.String()[:fanoutLength]
}

func looseName(id hash.ObjectID) string {
	text := id.String()
	return filepath.Join(text[:fanoutLength], text[fanoutLength:])
}

func looseID(fanout, name string) (hash.ObjectID, bool) {
	if len(fanout) != fanoutLength || len(name) != nameLength {
		return hash.Zero, false
	}
	var id hash.ObjectID
	if _, err := hex.Decode(id[:], []byte(fanout+name)); err != nil {
		return hash.Zero, false
	}
	return id, true
}

func decodeLooseHeader(source io.ByteReader) (object.Type, int64, error) {
	head, err := readHeaderBytes(source)
	if err != nil {
		return 0, 0, err
	}
	typeText, sizeText, found := bytes.Cut(head, []byte(" "))
	if !found {
		return 0, 0, fmt.Errorf("%w: no space in header %q", object.ErrInvalidHeader, head)
	}
	kind, err := object.ParseType(string(typeText))
	if err != nil {
		return 0, 0, err
	}
	size, err := strconv.ParseInt(string(sizeText), 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("%w: %w", object.ErrInvalidHeader, err)
	}
	if size < 0 {
		return 0, 0, fmt.Errorf("%w: negative size %d", object.ErrInvalidHeader, size)
	}
	return kind, size, nil
}

func readHeaderBytes(source io.ByteReader) ([]byte, error) {
	head := make([]byte, 0, looseHeaderLimit)
	for range looseHeaderLimit {
		current, err := source.ReadByte()
		if err != nil {
			return nil, fmt.Errorf("%w: %w", object.ErrInvalidHeader, err)
		}
		if current == 0 {
			return head, nil
		}
		head = append(head, current)
	}
	return nil, fmt.Errorf("%w: %d bytes without a terminator", ErrHeaderTooLong, looseHeaderLimit)
}

func readLooseBody(source io.Reader, size int64) ([]byte, error) {
	var body bytes.Buffer
	body.Grow(int(min(size, loosePrealloc)))
	read, err := io.Copy(&body, io.LimitReader(source, size+1))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", object.ErrMalformed, err)
	}
	if read != size {
		return nil, fmt.Errorf("%w: header declares %d, content holds %d", object.ErrSizeMismatch, size, read)
	}
	return body.Bytes(), nil
}

func decodeLoose(source io.Reader) (object.Type, []byte, error) {
	decompressor, err := zlib.NewReader(source)
	if err != nil {
		return 0, nil, fmt.Errorf("%w: %w", object.ErrMalformed, err)
	}
	defer func() { _ = decompressor.Close() }()
	buffered := bufio.NewReader(decompressor)
	kind, size, err := decodeLooseHeader(buffered)
	if err != nil {
		return 0, nil, err
	}
	data, err := readLooseBody(buffered, size)
	if err != nil {
		return 0, nil, err
	}
	return kind, data, nil
}

func decodeLooseType(source io.Reader) (object.Type, int64, error) {
	decompressor, err := zlib.NewReader(source)
	if err != nil {
		return 0, 0, fmt.Errorf("%w: %w", object.ErrMalformed, err)
	}
	defer func() { _ = decompressor.Close() }()
	return decodeLooseHeader(bufio.NewReader(decompressor))
}

func (d *DB) looseFile(id hash.ObjectID) (*os.File, bool, error) {
	file, err := rootOpen(d.root, looseName(id))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("odb: open %s in %s: %w", id, d.dir, err)
	}
	return file, true, nil
}

func (d *DB) looseRead(id hash.ObjectID) (object.Type, []byte, bool, error) {
	file, ok, err := d.looseFile(id)
	if !ok {
		return 0, nil, false, err
	}
	defer func() { _ = file.Close() }()
	kind, data, err := decodeLoose(file)
	if err != nil {
		return 0, nil, false, fmt.Errorf("odb: read %s in %s: %w", id, d.dir, err)
	}
	if got := hash.SumSHA1(kind.String(), data); got != id {
		return 0, nil, false, fmt.Errorf("%w: %s in %s holds %s", ErrCorrupt, id, d.dir, got)
	}
	return kind, data, true, nil
}

func (d *DB) looseHeader(id hash.ObjectID) (object.Type, int64, bool, error) {
	file, ok, err := d.looseFile(id)
	if !ok {
		return 0, 0, false, err
	}
	defer func() { _ = file.Close() }()
	kind, size, err := decodeLooseType(file)
	if err != nil {
		return 0, 0, false, fmt.Errorf("odb: read %s in %s: %w", id, d.dir, err)
	}
	return kind, size, true, nil
}

func (d *DB) looseHas(id hash.ObjectID) (bool, error) {
	info, err := rootStat(d.root, looseName(id))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("odb: stat %s in %s: %w", id, d.dir, err)
	}
	return info.Mode().IsRegular(), nil
}

func (d *DB) readDir(name string) ([]fs.DirEntry, error) {
	file, err := rootOpen(d.root, name)
	if err != nil {
		return nil, fmt.Errorf("odb: open %s in %s: %w", name, d.dir, err)
	}
	entries, err := file.ReadDir(-1)
	_ = file.Close()
	if err != nil {
		return nil, fmt.Errorf("odb: read %s in %s: %w", name, d.dir, err)
	}
	return entries, nil
}
