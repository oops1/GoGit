//go:build !windows

package safefile

import (
	"os"

	"golang.org/x/sys/unix"
)

func tryLock(file *os.File) bool {
	return unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB) == nil
}
