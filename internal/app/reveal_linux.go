//go:build linux

package app

import (
	"os/exec"
	"path/filepath"
)

var revealCommand = func(path string) *exec.Cmd {
	return exec.Command("xdg-open", filepath.Dir(path))
}

var terminalCommand = func(path string) *exec.Cmd {
	return exec.Command("x-terminal-emulator", "--working-directory="+path)
}
