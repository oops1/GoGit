package ops

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

func cleanRepoPath(p string) (string, error) {
	if p == "" {
		return "", fmt.Errorf("%w: empty path", ErrInvalidPath)
	}
	clean := path.Clean(p)
	if clean == "." || clean == "/" || strings.HasPrefix(clean, "../") || clean == ".." || strings.HasPrefix(clean, "/") {
		return "", fmt.Errorf("%w: %q", ErrInvalidPath, p)
	}
	return clean, nil
}

func joinRel(dir, name string) string {
	if dir == "" {
		return name
	}
	return dir + "/" + name
}

func readDirRoot(root *os.Root, rel string) ([]fs.DirEntry, error) {
	name := "."
	if rel != "" {
		name = filepath.FromSlash(rel)
	}
	file, err := fsRootOpen(root, name)
	if err != nil {
		return nil, err
	}
	entries, err := file.ReadDir(-1)
	closeErr := fsFileClose(file)
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}
	return entries, nil
}
