package safefile

import (
	"os"

	"golang.org/x/sys/windows"
)

func tryLock(file *os.File) bool {
	var overlapped windows.Overlapped
	flags := uint32(windows.LOCKFILE_EXCLUSIVE_LOCK | windows.LOCKFILE_FAIL_IMMEDIATELY)
	return windows.LockFileEx(windows.Handle(file.Fd()), flags, 0, 1, 0, &overlapped) == nil
}
