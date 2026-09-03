//go:build !windows && !linux

package repo

import "os"

const defaultFileMode = true

func platformInitValues() []configValue {
	return nil
}

func samePath(a, b string) bool {
	return a == b
}

func filesystemID(path string) (string, error) {
	if _, err := os.Stat(path); err != nil {
		return "", err
	}
	return "", nil
}
