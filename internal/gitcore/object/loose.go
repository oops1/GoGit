package object

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

const (
	looseDirMode     = 0o755
	looseFileMode    = 0o444
	looseTempPattern = "tmp_obj_*"
	fanoutLength     = 2
)

func EncodeLoose(obj Object) []byte {
	body := obj.Encode()
	head := hash.Header(obj.Type().String(), int64(len(body)))
	raw := make([]byte, 0, len(head)+len(body))
	raw = append(raw, head...)
	return append(raw, body...)
}

func DecodeRaw(raw []byte) (Object, error) {
	head, body, found := bytes.Cut(raw, []byte{0})
	if !found {
		return nil, fmt.Errorf("%w: no NUL after the header", ErrInvalidHeader)
	}
	typeText, sizeText, found := bytes.Cut(head, []byte(" "))
	if !found {
		return nil, fmt.Errorf("%w: no space in header %q", ErrInvalidHeader, head)
	}
	objectType, err := ParseType(string(typeText))
	if err != nil {
		return nil, err
	}
	size, err := strconv.ParseInt(string(sizeText), 10, 64)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidHeader, err)
	}
	if size != int64(len(body)) {
		return nil, fmt.Errorf("%w: header declares %d, content has %d", ErrSizeMismatch, size, len(body))
	}
	return Parse(objectType, body)
}

func DecodeLoose(r io.Reader) (Object, error) {
	decompressor, err := zlib.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrMalformed, err)
	}
	raw, err := io.ReadAll(decompressor)
	closeErr := decompressor.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrMalformed, err)
	}
	return DecodeRaw(raw)
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
	obj, err := DecodeLoose(file)
	if err != nil {
		return nil, fmt.Errorf("object: read %s: %w", path, err)
	}
	if got := obj.ID(); got != want {
		return nil, fmt.Errorf("%w: %s holds %s", ErrCorrupt, path, got)
	}
	return obj, nil
}

func WriteLoose(dir string, obj Object) (hash.ObjectID, error) {
	id := obj.ID()
	name := id.String()
	fanout := filepath.Join(dir, name[:fanoutLength])
	final := filepath.Join(fanout, name[fanoutLength:])
	if isRegularFile(final) {
		return id, nil
	}
	temp, err := createLooseTemp(fanout)
	if err != nil {
		return hash.Zero, fmt.Errorf("object: prepare %s: %w", final, err)
	}
	if err := finishLooseTemp(temp, final, compressLoose(obj)); err != nil {
		return hash.Zero, err
	}
	return id, nil
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

func compressLoose(obj Object) []byte {
	var buf bytes.Buffer
	compressor := zlib.NewWriter(&buf)
	_, _ = compressor.Write(EncodeLoose(obj))
	_ = compressor.Close()
	return buf.Bytes()
}

func createLooseTemp(fanout string) (*os.File, error) {
	if err := os.MkdirAll(fanout, looseDirMode); err != nil {
		return nil, err
	}
	return os.CreateTemp(fanout, looseTempPattern)
}

func finishLooseTemp(temp *os.File, final string, payload []byte) error {
	name := temp.Name()
	_, err := temp.Write(payload)
	if closeErr := temp.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Chmod(name, looseFileMode)
	}
	if err == nil {
		err = os.Rename(name, final)
	}
	if err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("object: write %s: %w", final, err)
	}
	return nil
}

func isRegularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}
