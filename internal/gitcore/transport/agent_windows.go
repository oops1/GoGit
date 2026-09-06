//go:build windows

package transport

import (
	"errors"
	"io"
	"os"

	"golang.org/x/sys/windows"
)

const defaultWindowsAgentPipe = `\\.\pipe\openssh-ssh-agent`

func dialAgent() (io.ReadWriteCloser, error) {
	name := os.Getenv("SSH_AUTH_SOCK")
	if name == "" {
		name = defaultWindowsAgentPipe
	}
	return dialWindowsPipe(name)
}

func dialWindowsPipe(name string) (io.ReadWriteCloser, error) {
	path, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateFile(
		path,
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		0,
		nil,
		windows.OPEN_EXISTING,
		0,
		0,
	)
	if err != nil {
		return nil, err
	}
	return &windowsPipeConn{handle: handle}, nil
}

type windowsPipeConn struct {
	handle windows.Handle
}

func (c *windowsPipeConn) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	var n uint32
	err := windows.ReadFile(c.handle, p, &n, nil)
	if err != nil {
		if errors.Is(err, windows.ERROR_BROKEN_PIPE) {
			return int(n), io.EOF
		}
		return int(n), err
	}
	if n == 0 {
		return 0, io.EOF
	}
	return int(n), nil
}

func (c *windowsPipeConn) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	var n uint32
	if err := windows.WriteFile(c.handle, p, &n, nil); err != nil {
		return int(n), err
	}
	return int(n), nil
}

func (c *windowsPipeConn) Close() error {
	return windows.CloseHandle(c.handle)
}
