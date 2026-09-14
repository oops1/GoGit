//go:build windows

package transport

import (
	"os"
	"path/filepath"
)

func systemSSHConfigPath() string {
	programData := os.Getenv("ProgramData")
	if programData == "" {
		return ""
	}
	return filepath.Join(programData, "ssh", "ssh_config")
}
