package object

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

const (
	looseFileMode     = 0o444
	looseTempMode     = 0o644
	looseDirMode      = 0o755
	looseTempPrefix   = "tmp_obj_"
	looseHeaderLimit  = 64
	looseBodyPrealloc = 1 << 20
	fanoutLength      = 2
)

var ErrHeaderTooLong = errors.New("object: loose object header is too long")

func EncodeLooseRaw(typ Type, data []byte) []byte {
	head := hash.Header(typ.String(), int64(len(data)))
	raw := make([]byte, 0, len(head)+len(data))
	raw = append(raw, head...)
	return append(raw, data...)
}

func EncodeLoose(obj Object) []byte {
	return EncodeLooseRaw(obj.Type(), obj.Encode())
}

func ReadLooseHeader(r io.ByteReader) (Type, int64, error) {
	head, err := readLooseHeaderBytes(r)
	if err != nil {
		return 0, 0, err
	}
	typeText, sizeText, found := bytes.Cut(head, []byte(" "))
	if !found {
		return 0, 0, fmt.Errorf("%w: no space in header %q", ErrInvalidHeader, head)
	}
	objectType, err := ParseType(string(typeText))
	if err != nil {
		return 0, 0, err
	}
	size, err := strconv.ParseInt(string(sizeText), 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("%w: %w", ErrInvalidHeader, err)
	}
	if size < 0 {
		return 0, 0, fmt.Errorf("%w: negative size %d", ErrSizeMismatch, size)
	}
	return objectType, size, nil
}

func readLooseHeaderBytes(r io.ByteReader) ([]byte, error) {
	head := make([]byte, 0, looseHeaderLimit)
	for range looseHeaderLimit {
		current, err := r.ReadByte()
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrInvalidHeader, err)
		}
		if current == 0 {
			return head, nil
		}
		head = append(head, current)
	}
	return nil, fmt.Errorf("%w: %d bytes without a terminator", ErrHeaderTooLong, looseHeaderLimit)
}

func DecodeRaw(raw []byte) (Object, error) {
	reader := bytes.NewReader(raw)
	objectType, size, err := ReadLooseHeader(reader)
	if err != nil {
		return nil, err
	}
	body := raw[len(raw)-reader.Len():]
	if size != int64(len(body)) {
		return nil, fmt.Errorf("%w: header declares %d, content has %d", ErrSizeMismatch, size, len(body))
	}
	return Parse(objectType, body)
}

func DecodeRawStream(r io.Reader, want hash.ObjectID) (Type, []byte, error) {
	decompressor, err := zlib.NewReader(r)
	if err != nil {
		return 0, nil, fmt.Errorf("%w: %w", ErrMalformed, err)
	}
	defer func() { _ = decompressor.Close() }()
	buffered := bufio.NewReader(decompressor)
	objectType, size, err := ReadLooseHeader(buffered)
	if err != nil {
		return 0, nil, err
	}
	body, err := readLooseBody(buffered, size)
	if err != nil {
		return 0, nil, err
	}
	if want != hash.Zero {
		if got := hash.SumSHA1(objectType.String(), body); got != want {
			return 0, nil, fmt.Errorf("%w: holds %s", ErrCorrupt, got)
		}
	}
	return objectType, body, nil
}

func readLooseBody(source io.Reader, size int64) ([]byte, error) {
	var body bytes.Buffer
	body.Grow(int(min(size, looseBodyPrealloc)))
	read, err := io.Copy(&body, io.LimitReader(source, size+1))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrMalformed, err)
	}
	if read != size {
		return nil, fmt.Errorf("%w: header declares %d, content holds %d", ErrSizeMismatch, size, read)
	}
	return body.Bytes(), nil
}

func DecodeLoose(r io.Reader) (Object, error) {
	objectType, body, err := DecodeRawStream(r, hash.Zero)
	if err != nil {
		return nil, err
	}
	return Parse(objectType, body)
}

func ReadLoose(path string) (Object, error) {
	want, err := IDFromLoosePath(path)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("object: open %s: %w", path, err)
	}
	defer func() { _ = file.Close() }()
	objectType, body, err := DecodeRawStream(file, want)
	if err != nil {
		return nil, fmt.Errorf("object: read %s: %w", path, err)
	}
	return Parse(objectType, body)
}

func WriteLoose(dir string, obj Object) (hash.ObjectID, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return hash.Zero, fmt.Errorf("object: open %s: %w", dir, err)
	}
	defer func() { _ = root.Close() }()
	return WriteLooseRaw(root, obj.Type(), obj.Encode())
}

func WriteLooseRaw(root *os.Root, typ Type, data []byte) (hash.ObjectID, error) {
	id := hash.SumSHA1(typ.String(), data)
	name := id.String()
	fanout := name[:fanoutLength]
	final := filepath.Join(fanout, name[fanoutLength:])
	if info, err := root.Stat(final); err == nil && info.Mode().IsRegular() {
		return id, nil
	}
	tempName := looseTempPrefix + rand.Text()
	temp, err := root.OpenFile(tempName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, looseTempMode)
	if err != nil {
		return hash.Zero, fmt.Errorf("object: create %s: %w", tempName, err)
	}
	if err := writeLooseTemp(root, temp, tempName, fanout, final, compressLooseRaw(typ, data)); err != nil {
		return hash.Zero, fmt.Errorf("object: write %s: %w", final, err)
	}
	return id, nil
}

func writeLooseTemp(root *os.Root, temp *os.File, tempName, fanout, final string, payload []byte) error {
	_, err := temp.Write(payload)
	if closeErr := temp.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = root.Chmod(tempName, looseFileMode)
	}
	if err == nil {
		err = root.MkdirAll(fanout, looseDirMode)
	}
	if err == nil {
		err = root.Rename(tempName, final)
	}
	if err != nil {
		_ = root.Remove(tempName)
		return err
	}
	return nil
}

func compressLooseRaw(typ Type, data []byte) []byte {
	var buf bytes.Buffer
	compressor := zlib.NewWriter(&buf)
	_, _ = compressor.Write(EncodeLooseRaw(typ, data))
	_ = compressor.Close()
	return buf.Bytes()
}

func IDFromLoosePath(path string) (hash.ObjectID, error) {
	name := filepath.Base(path)
	fanout := filepath.Base(filepath.Dir(path))
	if len(fanout) != fanoutLength || len(name) != hash.HexSize-fanoutLength {
		return hash.Zero, fmt.Errorf("%w: %s", ErrInvalidPath, path)
	}
	id, err := hash.Parse(fanout + name)
	if err != nil {
		return hash.Zero, fmt.Errorf("%w: %s: %w", ErrInvalidPath, path, err)
	}
	return id, nil
}
