//go:build !linux

package safefile

import "testing"

func TestSyncDirIsNoOpOutsideLinux(t *testing.T) {
	if err := syncDir("does-not-matter"); err != nil {
		t.Fatal(err)
	}
}
