package safefile

import (
	"path/filepath"
	"testing"
)

func TestSyncDirFailsForMissingDirectory(t *testing.T) {
	if err := syncDir(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("expected an error for a missing directory")
	}
}

func TestSyncDirSucceedsForExistingDirectory(t *testing.T) {
	if err := syncDir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
}
