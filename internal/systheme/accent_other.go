//go:build !windows

package systheme

import "os"

func detectAccent() Accent {
	home, _ := os.UserHomeDir()
	return accentFromDesktopFiles(os.Getenv, home)
}
