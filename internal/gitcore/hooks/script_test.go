package hooks

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func hookFileName(name string) string { return name }

func workingDirectoryCommand() string {
	if runtime.GOOS == "windows" {
		return "pwd -W"
	}
	return "pwd"
}

func renderScript(s script) string {
	var b strings.Builder
	b.WriteString("#!/bin/sh\n")
	for _, line := range s.print {
		b.WriteString("echo '" + line + "'\n")
	}
	if s.stderr != "" {
		b.WriteString("echo '" + s.stderr + "' >&2\n")
	}
	if s.record != "" {
		log := "'" + filepath.ToSlash(s.record) + "'"
		b.WriteString("echo \"args:$*\" >> " + log + "\n")
		b.WriteString("echo \"dir:$(" + workingDirectoryCommand() + ")\" >> " + log + "\n")
		b.WriteString("echo \"gitdir:$GIT_DIR\" >> " + log + "\n")
		b.WriteString("echo \"extra:$HOOK_EXTRA\" >> " + log + "\n")
		b.WriteString("echo \"stdin:\" >> " + log + "\n")
		b.WriteString("cat >> " + log + "\n")
	}
	if s.sleep {
		b.WriteString("echo started\nsleep 60\n")
	}
	if s.background {
		b.WriteString("sleep 3 &\n")
	}
	b.WriteString("exit " + strconv.Itoa(s.exit) + "\n")
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
