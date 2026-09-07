//go:build linux

package app

import "os/exec"

var openURLCommand = func(url string) *exec.Cmd {
	return exec.Command("xdg-open", url)
}
