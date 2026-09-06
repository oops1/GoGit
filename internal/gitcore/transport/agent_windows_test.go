//go:build windows

package transport

import (
	"errors"
	"fmt"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

var pipeNameCounter atomic.Int64

func uniquePipeName(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf(`\\.\pipe\gogit-test-%d-%d`, time.Now().UnixNano(), pipeNameCounter.Add(1))
}

func startFakeWindowsPipeServer(t *testing.T, name string, handle func(t *testing.T, conn io.ReadWriteCloser)) {
	t.Helper()
	path, err := windows.UTF16PtrFromString(name)
	if err != nil {
		t.Fatalf("UTF16PtrFromString returned error %v", err)
	}
	handleValue, err := windows.CreateNamedPipe(
		path,
		windows.PIPE_ACCESS_DUPLEX,
		windows.PIPE_TYPE_BYTE|windows.PIPE_READMODE_BYTE,
		1,
		4096,
		4096,
		0,
		nil,
	)
	if err != nil {
		t.Fatalf("CreateNamedPipe returned error %v", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		err := windows.ConnectNamedPipe(handleValue, nil)
		if err != nil && !errors.Is(err, windows.ERROR_PIPE_CONNECTED) {
			if errors.Is(err, windows.ERROR_NO_DATA) {
				_ = windows.CloseHandle(handleValue)
				return
			}
			t.Errorf("ConnectNamedPipe returned error %v", err)
			return
		}
		conn := &windowsPipeConn{handle: handleValue}
		defer func() { _ = conn.Close() }()
		handle(t, conn)
	}()
	t.Cleanup(func() {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Errorf("fake windows pipe server did not finish within 5s")
		}
	})
}

func TestDialWindowsPipeRoundTrip(t *testing.T) {
	name := uniquePipeName(t)
	startFakeWindowsPipeServer(t, name, func(t *testing.T, conn io.ReadWriteCloser) {
		buf := make([]byte, 5)
		if _, err := io.ReadFull(conn, buf); err != nil {
			t.Errorf("ReadFull returned error %v", err)
			return
		}
		if string(buf) != "hello" {
			t.Errorf("read %q, want %q", buf, "hello")
			return
		}
		if _, err := conn.Write([]byte("world")); err != nil {
			t.Errorf("Write returned error %v", err)
		}
	})

	conn, err := dialWindowsPipe(name)
	if err != nil {
		t.Fatalf("dialWindowsPipe returned error %v", err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.Write([]byte("hello")); err != nil {
		t.Fatalf("Write returned error %v", err)
	}
	buf := make([]byte, 5)
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("ReadFull returned error %v", err)
	}
	if string(buf) != "world" {
		t.Fatalf("read %q, want %q", buf, "world")
	}
}

func TestDialWindowsPipeFailsWhenPipeDoesNotExist(t *testing.T) {
	name := uniquePipeName(t)
	_, err := dialWindowsPipe(name)
	if err == nil {
		t.Fatalf("dialWindowsPipe succeeded, want an error")
	}
}

func TestDialAgentUsesDefaultPipeNameWhenEnvUnset(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")
	_, err := dialAgent()
	if err == nil {
		t.Fatalf("dialAgent succeeded, want an error since no agent is listening")
	}
}

func TestDialWindowsPipeFailsWhenNameContainsEmbeddedNUL(t *testing.T) {
	_, err := dialWindowsPipe("bad\x00name")
	if err == nil {
		t.Fatalf("dialWindowsPipe succeeded, want an error for an embedded NUL byte")
	}
}

func TestWindowsPipeConnReadWithEmptyBufferReturnsImmediately(t *testing.T) {
	name := uniquePipeName(t)
	startFakeWindowsPipeServer(t, name, func(t *testing.T, conn io.ReadWriteCloser) {
		time.Sleep(200 * time.Millisecond)
	})
	conn, err := dialWindowsPipe(name)
	if err != nil {
		t.Fatalf("dialWindowsPipe returned error %v", err)
	}
	defer func() { _ = conn.Close() }()
	n, err := conn.Read(nil)
	if n != 0 || err != nil {
		t.Fatalf("Read(nil) = (%d, %v), want (0, nil)", n, err)
	}
}

func TestWindowsPipeConnWriteWithEmptyBufferReturnsImmediately(t *testing.T) {
	name := uniquePipeName(t)
	startFakeWindowsPipeServer(t, name, func(t *testing.T, conn io.ReadWriteCloser) {})
	conn, err := dialWindowsPipe(name)
	if err != nil {
		t.Fatalf("dialWindowsPipe returned error %v", err)
	}
	defer func() { _ = conn.Close() }()
	n, err := conn.Write(nil)
	if n != 0 || err != nil {
		t.Fatalf("Write(nil) = (%d, %v), want (0, nil)", n, err)
	}
}

func TestWindowsPipeConnReadReturnsEOFWhenPeerClosesThePipe(t *testing.T) {
	name := uniquePipeName(t)
	startFakeWindowsPipeServer(t, name, func(t *testing.T, conn io.ReadWriteCloser) {
		_ = conn.Close()
	})
	conn, err := dialWindowsPipe(name)
	if err != nil {
		t.Fatalf("dialWindowsPipe returned error %v", err)
	}
	defer func() { _ = conn.Close() }()
	buf := make([]byte, 4)
	_, err = conn.Read(buf)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("Read returned %v, want io.EOF once the peer closed the pipe", err)
	}
}

func TestWindowsPipeConnReadFailsAfterHandleIsClosed(t *testing.T) {
	name := uniquePipeName(t)
	startFakeWindowsPipeServer(t, name, func(t *testing.T, conn io.ReadWriteCloser) {
		time.Sleep(200 * time.Millisecond)
	})
	conn, err := dialWindowsPipe(name)
	if err != nil {
		t.Fatalf("dialWindowsPipe returned error %v", err)
	}
	pipeConn := conn.(*windowsPipeConn)
	if err := windows.CloseHandle(pipeConn.handle); err != nil {
		t.Fatalf("CloseHandle returned error %v", err)
	}
	buf := make([]byte, 4)
	_, err = pipeConn.Read(buf)
	if err == nil {
		t.Fatalf("Read succeeded on a closed handle, want an error")
	}
	if errors.Is(err, io.EOF) {
		t.Fatalf("Read returned io.EOF for a closed handle, want a raw error")
	}
}

func TestWindowsPipeConnWriteFailsAfterHandleIsClosed(t *testing.T) {
	name := uniquePipeName(t)
	startFakeWindowsPipeServer(t, name, func(t *testing.T, conn io.ReadWriteCloser) {
		time.Sleep(200 * time.Millisecond)
	})
	conn, err := dialWindowsPipe(name)
	if err != nil {
		t.Fatalf("dialWindowsPipe returned error %v", err)
	}
	pipeConn := conn.(*windowsPipeConn)
	if err := windows.CloseHandle(pipeConn.handle); err != nil {
		t.Fatalf("CloseHandle returned error %v", err)
	}
	if _, err := pipeConn.Write([]byte("x")); err == nil {
		t.Fatalf("Write succeeded on a closed handle, want an error")
	}
}

func TestDialAgentUsesSSHAuthSockWhenSet(t *testing.T) {
	name := uniquePipeName(t)
	startFakeWindowsPipeServer(t, name, func(t *testing.T, conn io.ReadWriteCloser) {
		buf := make([]byte, 3)
		if _, err := io.ReadFull(conn, buf); err != nil {
			t.Errorf("ReadFull returned error %v", err)
		}
	})
	t.Setenv("SSH_AUTH_SOCK", name)
	conn, err := dialAgent()
	if err != nil {
		t.Fatalf("dialAgent returned error %v", err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.Write([]byte("abc")); err != nil {
		t.Fatalf("Write returned error %v", err)
	}
}
