package pack

import (
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

func mapFileView(file *os.File, size int64) ([]byte, func() error, error) {
	mapping, err := windows.CreateFileMapping(windows.Handle(file.Fd()), nil, windows.PAGE_READONLY, 0, 0, nil)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = windows.CloseHandle(mapping) }()
	address, err := windows.MapViewOfFile(mapping, windows.FILE_MAP_READ, 0, 0, 0)
	if err != nil {
		return nil, nil, err
	}
	data := unsafe.Slice((*byte)(unsafe.Pointer(address)), int(size))
	return data, func() error { return windows.UnmapViewOfFile(address) }, nil
}
