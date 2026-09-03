package app

import (
	"os"
	"path/filepath"
)

func writeFile(dir, name, content string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600)
}
