//go:build windows

package transport

import (
	"errors"
	"fmt"
	"io"
	"os"
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

func createPipeInstance(t *testing.T, name string) windows.Handle {
	t.Helper()
	path, err := windows.UTF16PtrFromString(name)
	if err != nil {
		t.Fatalf("UTF16PtrFromString returned error %v", err)
	}
	handle, err := windows.CreateNamedPipe(
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
	return handle
}

func acceptPipeClient(handle windows.Handle) error {
	err := windows.ConnectNamedPipe(handle, nil)
	if err != nil && !errors.Is(err, windows.ERROR_PIPE_CONNECTED) {
		return err
	}
	return nil
}

func startFakeWindowsPipeServer(t *testing.T, name string, handle func(t *testing.T, conn io.ReadWriteCloser)) {
	t.Helper()
	handleValue := createPipeInstance(t, name)
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := acceptPipeClient(handleValue); err != nil {
			_ = windows.CloseHandle(handleValue)
			if !errors.Is(err, windows.ERROR_NO_DATA) {
				t.Errorf("ConnectNamedPipe returned error %v", err)
			}
			return
		}
		conn := os.NewFile(uintptr(handleValue), name)
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

func useAgentPipe(t *testing.T, name string) {
	t.Helper()
	restore := windowsAgentPipe
	windowsAgentPipe = name
	t.Cleanup(func() { windowsAgentPipe = restore })
}

func TestDialAgentUsesDefaultPipeNameWhenEnvUnset(t *testing.T) {
	useAgentPipe(t, uniquePipeName(t))
	t.Setenv("SSH_AUTH_SOCK", "")
	_, err := dialAgent()
	if !errors.Is(err, ErrNoAgent) {
		t.Fatalf("dialAgent returned %v, want ErrNoAgent since no agent is listening", err)
	}
}

func TestDialAgentConnectsToTheDefaultPipeWhenEnvUnset(t *testing.T) {
	name := uniquePipeName(t)
	useAgentPipe(t, name)
	startFakeWindowsPipeServer(t, name, func(t *testing.T, conn io.ReadWriteCloser) {
		buf := make([]byte, 2)
		if _, err := io.ReadFull(conn, buf); err != nil {
			t.Errorf("ReadFull returned error %v", err)
		}
	})
	t.Setenv("SSH_AUTH_SOCK", "")
	conn, err := dialAgent()
	if err != nil {
		t.Fatalf("dialAgent returned error %v", err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.Write([]byte("ok")); err != nil {
		t.Fatalf("Write returned error %v", err)
	}
}

func TestDialAgentFallsBackToTheOpenSSHPipeForAnMSYSSocketPath(t *testing.T) {
	name := uniquePipeName(t)
	useAgentPipe(t, name)
	startFakeWindowsPipeServer(t, name, func(t *testing.T, conn io.ReadWriteCloser) {
		buf := make([]byte, 3)
		if _, err := io.ReadFull(conn, buf); err != nil {
			t.Errorf("ReadFull returned error %v", err)
		}
	})
	t.Setenv("SSH_AUTH_SOCK", "/tmp/ssh-XXXXabcd/agent.1234")
	conn, err := dialAgent()
	if err != nil {
		t.Fatalf("dialAgent returned error %v, want the fallback pipe", err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.Write([]byte("abc")); err != nil {
		t.Fatalf("Write returned error %v", err)
	}
}

func TestDialAgentReportsTheSocketWhenNeitherItNorTheFallbackOpens(t *testing.T) {
	useAgentPipe(t, uniquePipeName(t))
	t.Setenv("SSH_AUTH_SOCK", "/tmp/ssh-XXXXabcd/agent.1234")
	_, err := dialAgent()
	if !errors.Is(err, ErrNoAgent) {
		t.Fatalf("dialAgent returned %v, want ErrNoAgent", err)
	}
	if !errors.Is(err, windows.ERROR_PATH_NOT_FOUND) && !errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
		t.Fatalf("dialAgent returned %v, want the error of the socket path", err)
	}
}

func TestDialWindowsPipeFailsWhenNameContainsEmbeddedNUL(t *testing.T) {
	_, err := dialWindowsPipe("bad\x00name")
	if err == nil {
		t.Fatalf("dialWindowsPipe succeeded, want an error for an embedded NUL byte")
	}
}

func dialOccupiedPipe(t *testing.T) (string, windows.Handle, io.ReadWriteCloser) {
	t.Helper()
	name := uniquePipeName(t)
	server := createPipeInstance(t, name)
	accepted := make(chan error, 1)
	go func() { accepted <- acceptPipeClient(server) }()
	first, err := dialWindowsPipe(name)
	if err != nil {
		t.Fatalf("dialWindowsPipe returned error %v", err)
	}
	if err := <-accepted; err != nil {
		t.Fatalf("ConnectNamedPipe returned error %v", err)
	}
	return name, server, first
}

func TestDialWindowsPipeWaitsUntilABusyPipeIsFree(t *testing.T) {
	name, server, first := dialOccupiedPipe(t)
	defer func() { _ = windows.CloseHandle(server) }()

	released := make(chan error, 1)
	go func() {
		time.Sleep(100 * time.Millisecond)
		_ = first.Close()
		if err := windows.DisconnectNamedPipe(server); err != nil {
			released <- err
			return
		}
		released <- acceptPipeClient(server)
	}()

	second, err := dialWindowsPipe(name)
	if err != nil {
		t.Fatalf("dialWindowsPipe returned error %v, want it to wait for the busy pipe", err)
	}
	defer func() { _ = second.Close() }()
	if err := <-released; err != nil {
		t.Fatalf("the server could not take the second client: %v", err)
	}
}

func TestDialWindowsPipeFailsWhenTheBusyPipeStaysBusy(t *testing.T) {
	name, server, first := dialOccupiedPipe(t)
	defer func() { _ = windows.CloseHandle(server) }()
	defer func() { _ = first.Close() }()
	restore := pipeBusyWaitMillis
	pipeBusyWaitMillis = 50
	t.Cleanup(func() { pipeBusyWaitMillis = restore })

	if _, err := dialWindowsPipe(name); err == nil {
		t.Fatalf("dialWindowsPipe succeeded, want a timeout while the only instance is busy")
	}
}

func TestDialWindowsPipeGivesUpAfterRepeatedBusyReplies(t *testing.T) {
	name, server, first := dialOccupiedPipe(t)
	defer func() { _ = windows.CloseHandle(server) }()
	defer func() { _ = first.Close() }()
	waits := 0
	restore := waitForNamedPipe
	waitForNamedPipe = func(*uint16, uint32) error {
		waits++
		return nil
	}
	t.Cleanup(func() { waitForNamedPipe = restore })

	_, err := dialWindowsPipe(name)
	if !errors.Is(err, windows.ERROR_PIPE_BUSY) {
		t.Fatalf("dialWindowsPipe returned %v, want ERROR_PIPE_BUSY", err)
	}
	if waits != pipeBusyAttempts {
		t.Fatalf("waited %d times, want %d", waits, pipeBusyAttempts)
	}
}

func TestClosingTheAgentPipeUnblocksAPendingRead(t *testing.T) {
	name := uniquePipeName(t)
	release := make(chan struct{})
	startFakeWindowsPipeServer(t, name, func(t *testing.T, conn io.ReadWriteCloser) {
		<-release
	})
	defer close(release)
	conn, err := dialWindowsPipe(name)
	if err != nil {
		t.Fatalf("dialWindowsPipe returned error %v", err)
	}
	readDone := make(chan error, 1)
	go func() {
		_, err := conn.Read(make([]byte, 4))
		readDone <- err
	}()
	time.Sleep(50 * time.Millisecond)
	if err := conn.Close(); err != nil {
		t.Fatalf("Close returned error %v", err)
	}
	select {
	case err := <-readDone:
		if err == nil {
			t.Fatalf("Read succeeded after Close, want an error")
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("Read stayed blocked after the pipe was closed")
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

func TestWindowsPipeConnWriteFailsAfterClose(t *testing.T) {
	name := uniquePipeName(t)
	startFakeWindowsPipeServer(t, name, func(t *testing.T, conn io.ReadWriteCloser) {
		time.Sleep(200 * time.Millisecond)
	})
	conn, err := dialWindowsPipe(name)
	if err != nil {
		t.Fatalf("dialWindowsPipe returned error %v", err)
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("Close returned error %v", err)
	}
	if _, err := conn.Write([]byte("x")); err == nil {
		t.Fatalf("Write succeeded on a closed pipe, want an error")
	}
}

func TestDialAgentUsesSSHAuthSockWhenSet(t *testing.T) {
	name := uniquePipeName(t)
	useAgentPipe(t, uniquePipeName(t))
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
