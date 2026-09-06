package winconsole

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	kernel32                  = windows.NewLazySystemDLL("kernel32.dll")
	procGetConsoleWindow      = kernel32.NewProc("GetConsoleWindow")
	procGetConsoleProcessList = kernel32.NewProc("GetConsoleProcessList")
	procFreeConsole           = kernel32.NewProc("FreeConsole")
)

var (
	consoleWindow  = ownConsoleWindow
	consoleClients = ownConsoleClients
	releaseConsole = ownReleaseConsole
)

func Hide() bool {
	if consoleWindow() == 0 {
		return false
	}
	if consoleClients() != 1 {
		return false
	}
	return releaseConsole()
}

func ownConsoleWindow() uintptr {
	handle, _, _ := procGetConsoleWindow.Call()
	return handle
}

func ownConsoleClients() int {
	var pids [4]uint32
	count, _, _ := procGetConsoleProcessList.Call(uintptr(unsafe.Pointer(&pids[0])), uintptr(len(pids)))
	return int(count)
}

func ownReleaseConsole() bool {
	done, _, _ := procFreeConsole.Call()
	return done != 0
}
