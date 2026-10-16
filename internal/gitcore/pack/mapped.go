package pack

import (
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"sync"
)

var mapFile = mapFileView

type mappedFile struct {
	mu      sync.RWMutex
	file    *os.File
	data    []byte
	release func() error
}

func newMappedFile(file *os.File, size int64) (*mappedFile, error) {
	data, release, err := mapFile(file, size)
	if err != nil {
		return nil, err
	}
	return &mappedFile{file: file, data: data, release: release}, nil
}

func sourceOf(file *os.File, size int64) (io.ReaderAt, io.Closer) {
	if mapped, err := newMappedFile(file, size); err == nil {
		return mapped, mapped
	}
	return file, file
}

func (m *mappedFile) ReadAt(p []byte, offset int64) (read int, err error) {
	if offset < 0 {
		return 0, fmt.Errorf("pack: negative offset %d", offset)
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.data == nil {
		return 0, os.ErrClosed
	}
	if offset >= int64(len(m.data)) {
		if len(p) == 0 {
			return 0, nil
		}
		return 0, io.EOF
	}
	previous := debug.SetPanicOnFault(true)
	defer func() {
		debug.SetPanicOnFault(previous)
		if recovered := recover(); recovered != nil {
			read, err = 0, fmt.Errorf("pack: the mapped file changed under the reader: %v", recovered)
		}
	}()
	read = copy(p, m.data[offset:])
	if read < len(p) {
		return read, io.EOF
	}
	return read, nil
}

func (m *mappedFile) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.data == nil {
		return &os.PathError{Op: "close", Path: m.file.Name(), Err: os.ErrClosed}
	}
	m.data = nil
	unmapErr := m.release()
	if err := m.file.Close(); err != nil {
		return err
	}
	return unmapErr
}
