//go:build windows

package transport

import (
	"path/filepath"
	"testing"
)

func TestSystemSSHConfigPathLivesUnderProgramData(t *testing.T) {
	t.Setenv("ProgramData", `C:\ProgramData`)
	if got, want := systemSSHConfigPath(), filepath.Join(`C:\ProgramData`, "ssh", "ssh_config"); got != want {
		t.Fatalf("systemSSHConfigPath = %q, want %q", got, want)
	}
	t.Setenv("ProgramData", "")
	if got := systemSSHConfigPath(); got != "" {
		t.Fatalf("systemSSHConfigPath = %q without ProgramData, want empty", got)
	}
}
