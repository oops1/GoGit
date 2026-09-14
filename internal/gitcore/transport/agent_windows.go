//go:build windows

package transport

import (
	"errors"
	"fmt"
	"io"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

const defaultWindowsAgentPipe = `\\.\pipe\openssh-ssh-agent`

const pipeBusyAttempts = 5

var (
	windowsAgentPipe   = defaultWindowsAgentPipe
	pipeBusyWaitMillis = uint32(2000)
	procWaitNamedPipe  = windows.NewLazySystemDLL("kernel32.dll").NewProc("WaitNamedPipeW")
	waitForNamedPipe   = waitNamedPipe
)

func dialAgent() (io.ReadWriteCloser, error) {
	name := os.Getenv("SSH_AUTH_SOCK")
	if name == "" || name == windowsAgentPipe {
		return dialAgentPipe(windowsAgentPipe)
	}
	conn, err := dialWindowsPipe(name)
	if err == nil {
		return conn, nil
	}
	fallback, fallbackErr := dialWindowsPipe(windowsAgentPipe)
	if fallbackErr != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrNoAgent, name, err)
	}
	return fallback, nil
}

func dialAgentPipe(name string) (io.ReadWriteCloser, error) {
	conn, err := dialWindowsPipe(name)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrNoAgent, name, err)
	}
	return conn, nil
}

func dialWindowsPipe(name string) (io.ReadWriteCloser, error) {
	path, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil, err
	}
	for range pipeBusyAttempts {
		handle, err := windows.CreateFile(
			path,
			windows.GENERIC_READ|windows.GENERIC_WRITE,
			0,
			nil,
			windows.OPEN_EXISTING,
			windows.FILE_FLAG_OVERLAPPED,
			0,
		)
		if err == nil {
			return os.NewFile(uintptr(handle), name), nil
		}
		if !errors.Is(err, windows.ERROR_PIPE_BUSY) {
			return nil, err
		}
		if err := waitForNamedPipe(path, pipeBusyWaitMillis); err != nil {
			return nil, err
		}
	}
	return nil, windows.ERROR_PIPE_BUSY
}

func waitNamedPipe(path *uint16, timeoutMillis uint32) error {
	ok, _, err := procWaitNamedPipe.Call(uintptr(unsafe.Pointer(path)), uintptr(timeoutMillis))
	if ok == 0 {
		return err
	}
	return nil
}
