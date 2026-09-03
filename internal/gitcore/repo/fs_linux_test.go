package repo

import (
	"io/fs"
	"os"
	"testing"
	"time"
)

type sysLessFileInfo struct {
	fs.FileInfo
}

func (sysLessFileInfo) Name() string       { return "sysless" }
func (sysLessFileInfo) Size() int64        { return 0 }
func (sysLessFileInfo) Mode() fs.FileMode  { return fs.ModeDir }
func (sysLessFileInfo) ModTime() time.Time { return time.Time{} }
func (sysLessFileInfo) IsDir() bool        { return true }
func (sysLessFileInfo) Sys() any           { return nil }

func TestDeviceIDIsEmptyWithoutSystemInformation(t *testing.T) {
	if got := deviceID(sysLessFileInfo{}); got != "" {
		t.Fatalf("deviceID returned %q, want an empty identity", got)
	}
}

func TestDeviceIDReportsTheDeviceNumber(t *testing.T) {
	base := tempDir(t)
	info, err := os.Stat(base)
	if err != nil {
		t.Fatalf("Stat returned error %v", err)
	}
	if got := deviceID(info); got == "" {
		t.Fatal("deviceID returned an empty identity for a real directory")
	}
}

func TestSamePathIsCaseSensitiveOnLinux(t *testing.T) {
	if samePath("/repo/Work", "/repo/work") {
		t.Fatal("samePath equated two paths that differ in case")
	}
	if !samePath("/repo/work", "/repo/work") {
		t.Fatal("samePath distinguished one path from itself")
	}
}

func TestPlatformInitValuesDescribeLinuxFilesystems(t *testing.T) {
	if !defaultFileMode {
		t.Error("defaultFileMode is false although Linux keeps the executable bit")
	}
	if got := platformInitValues(); got != nil {
		t.Errorf("platformInitValues returned %v, want no extra values", got)
	}
}
