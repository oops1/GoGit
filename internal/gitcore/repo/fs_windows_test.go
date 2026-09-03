package repo

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestVolumeIDLowerCasesTheVolumeName(t *testing.T) {
	base := tempDir(t)
	want := strings.ToLower(filepath.VolumeName(base))
	if got := volumeID(base); got != want {
		t.Fatalf("volumeID(%q) = %q, want %q", base, got, want)
	}
	if got := volumeID(strings.ToUpper(base)); got != want {
		t.Fatalf("volumeID of the upper cased path = %q, want %q", got, want)
	}
	if got := volumeID(`\\server\share\dir`); got != `\\server\share` {
		t.Fatalf("volumeID of a UNC path = %q, want %q", got, `\\server\share`)
	}
}

func TestVolumeIDSeparatesDifferentVolumes(t *testing.T) {
	if volumeID(`C:\dir`) == volumeID(`D:\dir`) {
		t.Fatal("volumeID reports one identity for two volumes")
	}
}

func TestSamePathIgnoresCaseOnWindows(t *testing.T) {
	if !samePath(`C:\Repo\Work`, `c:\repo\work`) {
		t.Fatal("samePath distinguished two spellings of one path")
	}
	if samePath(`C:\one`, `C:\two`) {
		t.Fatal("samePath equated two different paths")
	}
}

func TestPlatformInitValuesDescribeWindowsFilesystems(t *testing.T) {
	if defaultFileMode {
		t.Error("defaultFileMode is true although Windows has no executable bit")
	}
	want := []configValue{{"core.symlinks", "false"}, {"core.ignorecase", "true"}}
	got := platformInitValues()
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("platformInitValues returned %v, want %v", got, want)
	}
}
