package attributes

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

type Loader interface {
	ReadFile(path string) ([]byte, error)
}

type LoaderFunc func(path string) ([]byte, error)

func (f LoaderFunc) ReadFile(path string) ([]byte, error) { return f(path) }

func RootLoader(root *os.Root) Loader {
	return LoaderFunc(func(path string) ([]byte, error) {
		return root.ReadFile(filepath.FromSlash(path))
	})
}

func OSLoader(base string) Loader {
	return LoaderFunc(func(path string) ([]byte, error) {
		name := filepath.FromSlash(path)
		if base != "" && !filepath.IsAbs(name) {
			name = filepath.Join(base, name)
		}
		return os.ReadFile(name)
	})
}

func MemoryLoader(files map[string]string) Loader {
	return LoaderFunc(func(path string) ([]byte, error) {
		text, ok := files[path]
		if !ok {
			return nil, fs.ErrNotExist
		}
		return []byte(text), nil
	})
}

type cachedFile[T any] struct {
	rules []T
	err   error
}

type fileCache[T any] struct {
	loader Loader
	parse  func(source string, data []byte) []T
	cache  map[string]cachedFile[T]
}

func newFileCache[T any](loader Loader, parse func(string, []byte) []T) *fileCache[T] {
	return &fileCache[T]{loader: loader, parse: parse, cache: map[string]cachedFile[T]{}}
}

func (c *fileCache[T]) get(path string) ([]T, error) {
	if entry, ok := c.cache[path]; ok {
		return entry.rules, entry.err
	}
	var entry cachedFile[T]
	switch data, err := c.read(path); {
	case err == nil:
		entry.rules = c.parse(path, data)
	case errors.Is(err, fs.ErrNotExist):
	default:
		entry.err = err
	}
	c.cache[path] = entry
	return entry.rules, entry.err
}

func (c *fileCache[T]) read(path string) ([]byte, error) {
	if c.loader == nil || path == "" {
		return nil, fs.ErrNotExist
	}
	return c.loader.ReadFile(path)
}
