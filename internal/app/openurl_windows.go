//go:build windows

package app

import "os/exec"

var openURLCommand = func(url string) *exec.Cmd {
	return exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
}
