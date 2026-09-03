package repo

import (
	"os"
	"path/filepath"
	"strings"
)

const defaultFileMode = false

func platformInitValues() []configValue {
	return []configValue{{"core.symlinks", "false"}, {"core.ignorecase", "true"}}
}

func samePath(a, b string) bool {
	return strings.EqualFold(a, b)
}

func volumeID(path string) string {
	return strings.ToLower(filepath.VolumeName(path))
}

func filesystemID(path string) (string, error) {
	if _, err := os.Stat(path); err != nil {
		return "", err
	}
	return volumeID(path), nil
}
