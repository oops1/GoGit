package index

import (
	"io/fs"
	"os"
)

const (
	lockSuffix = ".lock"
	filePerm   = 0o666
)

var (
	fsOpen     = os.Open
	fsStat     = func(file *os.File) (fs.FileInfo, error) { return file.Stat() }
	fsOpenRoot = os.OpenRoot
	fsCreate   = func(root *os.Root, name string, flag int) (*os.File, error) {
		return root.OpenFile(name, flag, filePerm)
	}
	fsWrite  = func(file *os.File, data []byte) (int, error) { return file.Write(data) }
	fsClose  = func(file *os.File) error { return file.Close() }
	fsRename = func(root *os.Root, from, to string) error { return root.Rename(from, to) }
	fsRemove = func(root *os.Root, name string) error { return root.Remove(name) }
)
