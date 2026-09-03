package pack

import (
	"bytes"
	"compress/zlib"
	"errors"
	"fmt"
	"io"
	"sync"
)

type inflater interface {
	io.ReadCloser
	zlib.Resetter
}

var emptyStream = []byte{0x78, 0x01, 0x03, 0x00, 0x00, 0x00, 0x00, 0x01}

var inflaters = sync.Pool{New: func() any {
	reader, _ := zlib.NewReader(bytes.NewReader(emptyStream))
	return reader.(inflater)
}}

func acquireInflater(source io.Reader) (inflater, error) {
	reader := inflaters.Get().(inflater)
	if err := reader.Reset(source, nil); err != nil {
		inflaters.Put(reader)
		return nil, fmt.Errorf("%w: %v", ErrDecompress, err)
	}
	return reader, nil
}

func releaseInflater(reader inflater) {
	inflaters.Put(reader)
}

func inflateExact(source io.Reader, into []byte) error {
	reader, err := acquireInflater(source)
	if err != nil {
		return err
	}
	defer releaseInflater(reader)
	if _, err := io.ReadFull(reader, into); err != nil {
		return fmt.Errorf("%w: %v", ErrDecompress, err)
	}
	return checkStreamEnd(reader, int64(len(into)))
}

func inflateBuffered(source io.Reader, into *bytes.Buffer, size int64) error {
	reader, err := acquireInflater(source)
	if err != nil {
		return err
	}
	defer releaseInflater(reader)
	into.Grow(int(min(size, maxPrealloc)))
	if _, err := io.CopyN(into, reader, size); err != nil {
		return fmt.Errorf("%w: %v", ErrDecompress, err)
	}
	return checkStreamEnd(reader, size)
}

func checkStreamEnd(reader io.Reader, size int64) error {
	var extra [1]byte
	read, err := reader.Read(extra[:])
	if read > 0 {
		return fmt.Errorf("%w: the stream holds more than %d bytes", ErrSizeMismatch, size)
	}
	if !errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: %v", ErrDecompress, err)
	}
	return nil
}
