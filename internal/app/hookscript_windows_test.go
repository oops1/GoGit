package app

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func writeAppHook(t *testing.T, repoDir, name, line string, exit int) {
	t.Helper()
	dir := filepath.Join(repoDir, ".git", "hooks")
	if err := os.MkdirAll(dir, 0o777); err != nil {
		t.Fatal(err)
	}
	script := "@echo off\r\necho " + line + "\r\nexit /b " + strconv.Itoa(exit) + "\r\n"
	if err := os.WriteFile(filepath.Join(dir, name+".cmd"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}
