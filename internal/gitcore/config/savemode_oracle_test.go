//go:build oracle

package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func (o *oracle) emptyRepo(name string) string {
	o.t.Helper()
	o.run(o.dir, "init", "-q", name)
	return filepath.Join(o.dir, name)
}

func ourConfigWrite(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile returned error %v", err)
	}
	f := mustParse(t, string(data))
	if err := f.Set("our.probe", "1"); err != nil {
		t.Fatalf("Set returned error %v", err)
	}
	if err := f.Save(path); err != nil {
		t.Fatalf("Save returned error %v", err)
	}
}

func TestOracleSavedConfigHasTheSamePermissionsAsGitGivesIt(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows keeps only the read-only bit, so group and other bits cannot be compared")
	}
	o := newOracle(t)
	theirs := o.emptyRepo("theirs")
	ours := o.emptyRepo("ours")
	theirPath := filepath.Join(theirs, ".git", "config")
	ourPath := filepath.Join(ours, ".git", "config")
	for _, mode := range []os.FileMode{0o600, 0o644, 0o664} {
		if err := os.Chmod(theirPath, mode); err != nil {
			t.Fatalf("Chmod returned error %v", err)
		}
		if err := os.Chmod(ourPath, mode); err != nil {
			t.Fatalf("Chmod returned error %v", err)
		}
		o.run(theirs, "config", "their.probe", "1")
		ourConfigWrite(t, ourPath)
		if got, want := modeOf(t, ourPath), modeOf(t, theirPath); got != want {
			t.Fatalf("after starting from %v our config has mode %v, git leaves %v", mode, got, want)
		}
	}
}

func TestOracleConfigCreatedFromNothingHasTheSamePermissionsAsGitGivesIt(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows keeps only the read-only bit, so group and other bits cannot be compared")
	}
	o := newOracle(t)
	theirs := o.emptyRepo("theirs")
	ours := o.emptyRepo("ours")
	theirPath := filepath.Join(theirs, ".git", "config")
	ourPath := filepath.Join(ours, ".git", "config")
	if err := os.Remove(theirPath); err != nil {
		t.Fatalf("Remove returned error %v", err)
	}
	if err := os.Remove(ourPath); err != nil {
		t.Fatalf("Remove returned error %v", err)
	}
	o.run(theirs, "config", "their.probe", "1")
	if err := mustParse(t, "[our]\n\tprobe = 1\n").Save(ourPath); err != nil {
		t.Fatalf("Save returned error %v", err)
	}
	if got, want := modeOf(t, ourPath), modeOf(t, theirPath); got != want {
		t.Fatalf("our fresh config has mode %v, git creates %v", got, want)
	}
}
