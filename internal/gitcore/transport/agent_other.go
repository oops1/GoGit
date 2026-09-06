//go:build !windows

package transport

import (
	"fmt"
	"io"
	"net"
	"os"
)

func dialAgent() (io.ReadWriteCloser, error) {
	sock := os.Getenv("SSH_AUTH_SOCK")
	if sock == "" {
		return nil, fmt.Errorf("%w: SSH_AUTH_SOCK is not set", ErrNoAgent)
	}
	conn, err := net.Dial("unix", sock)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrNoAgent, err)
	}
	return conn, nil
}
