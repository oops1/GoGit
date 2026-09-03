package repo

import (
	"path/filepath"
	"testing"
)

func TestFilesystemIDIsSharedByDirectoriesOfOneMount(t *testing.T) {
	base := tempDir(t)
	nested := makeDir(t, filepath.Join(base, "nested"))
	baseID, err := filesystemID(base)
	if err != nil {
		t.Fatalf("filesystemID returned error %v", err)
	}
	nestedID, err := filesystemID(nested)
	if err != nil {
		t.Fatalf("filesystemID returned error %v", err)
	}
	if baseID != nestedID {
		t.Fatalf("filesystemID returned %q for %q and %q for %q", baseID, base, nestedID, nested)
	}
}

func TestFilesystemIDReportsAMissingPath(t *testing.T) {
	if _, err := filesystemID(filepath.Join(tempDir(t), "absent")); err == nil {
		t.Fatal("filesystemID returned no error for a missing path")
	}
}

func TestFilesystemBoundaryNeedsASecondMountPoint(t *testing.T) {
	t.Skip("a real boundary needs a second mount point or volume, which tests cannot create; " +
		"the branch itself is covered through a stubbed filesystem identity in TestDiscoverStopsAtFilesystemBoundary")
}
