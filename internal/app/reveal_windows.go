//go:build windows

package app

import "os/exec"

var revealCommand = func(path string) *exec.Cmd {
	return exec.Command("explorer", "/select,"+path)
}

var terminalCommand = func(path string) *exec.Cmd {
	return exec.Command("cmd", "/c", "start", "", "cmd", "/k", "cd", "/d", path)
}
