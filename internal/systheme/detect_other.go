//go:build !windows

package systheme

import "os"

func detect() Scheme {
	home, _ := os.UserHomeDir()
	return detectFromDesktopFiles(os.Getenv, home)
}
