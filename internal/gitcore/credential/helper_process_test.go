package credential

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if os.Getenv("GOGIT_CREDENTIAL_FAKE") == "1" {
		os.Exit(runFakeHelperProcess())
	}
	os.Exit(m.Run())
}

func runFakeHelperProcess() int {
	if argsFile := os.Getenv("GOGIT_CREDENTIAL_FAKE_ARGS_FILE"); argsFile != "" {
		if err := os.WriteFile(argsFile, []byte(strings.Join(os.Args[1:], "\x1f")), 0o600); err != nil {
			return 90
		}
	}
	stdin, err := io.ReadAll(os.Stdin)
	if err != nil {
		return 91
	}
	if stdinFile := os.Getenv("GOGIT_CREDENTIAL_FAKE_STDIN_FILE"); stdinFile != "" {
		if err := os.WriteFile(stdinFile, stdin, 0o600); err != nil {
			return 92
		}
	}
	if raw := os.Getenv("GOGIT_CREDENTIAL_FAKE_SLEEP"); raw != "" {
		if d, err := time.ParseDuration(raw); err == nil {
			time.Sleep(d)
		}
	}
	if out := os.Getenv("GOGIT_CREDENTIAL_FAKE_STDOUT"); out != "" {
		if _, err := os.Stdout.WriteString(out); err != nil {
			return 93
		}
	}
	code := 0
	if raw := os.Getenv("GOGIT_CREDENTIAL_FAKE_EXIT"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil {
			code = n
		}
	}
	return code
}

func installFakePathHelper(t *testing.T, dir, name string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		script := "@echo off\r\necho username=pathuser\r\necho password=pathpass\r\n"
		if err := os.WriteFile(filepath.Join(dir, name+".cmd"), []byte(script), 0o700); err != nil {
			t.Fatalf("writing fake helper script: %v", err)
		}
		return
	}
	script := "#!/bin/sh\necho \"username=pathuser\"\necho \"password=pathpass\"\n"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o700); err != nil {
		t.Fatalf("writing fake helper script: %v", err)
	}
}

func fakeHelper(t *testing.T, env map[string]string) *execHelper {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable() returned %v", err)
	}
	t.Setenv("GOGIT_CREDENTIAL_FAKE", "1")
	for k, v := range env {
		t.Setenv(k, v)
	}
	return &execHelper{name: "fake", exe: self}
}
