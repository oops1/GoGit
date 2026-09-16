package hooks

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func hookFileName(name string) string { return name + ".cmd" }

func renderScript(s script) string {
	var b strings.Builder
	b.WriteString("@echo off\r\n")
	for _, line := range s.print {
		b.WriteString("echo " + line + "\r\n")
	}
	if s.stderr != "" {
		b.WriteString("1>&2 echo " + s.stderr + "\r\n")
	}
	if s.record != "" {
		log := "\"" + s.record + "\""
		b.WriteString(">>" + log + " echo args:%*\r\n")
		b.WriteString(">>" + log + " echo dir:%CD%\r\n")
		b.WriteString(">>" + log + " echo gitdir:%GIT_DIR%\r\n")
		b.WriteString(">>" + log + " echo extra:%HOOK_EXTRA%\r\n")
		b.WriteString(">>" + log + " echo stdin:\r\n")
		b.WriteString("findstr \"^\" >>" + log + "\r\n")
	}
	if s.sleep {
		b.WriteString("echo started\r\nping -n 60 127.0.0.1 >nul\r\n")
	}
	if s.background {
		b.WriteString("start /b ping -n 2 127.0.0.1 >nul\r\n")
	}
	b.WriteString("exit /b " + strconv.Itoa(s.exit) + "\r\n")
	return b.String()
}

func writeHook(t *testing.T, dir, name string, s script) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o777); err != nil {
		t.Fatalf("MkdirAll returned error %v", err)
	}
	path := filepath.Join(dir, hookFileName(name))
	if err := os.WriteFile(path, []byte(renderScript(s)), 0o755); err != nil {
		t.Fatalf("WriteFile returned error %v", err)
	}
	return path
}
