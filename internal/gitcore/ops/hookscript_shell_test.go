//go:build linux || oracle

package ops

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func shellHookScript(name string, h testHook) string {
	var b strings.Builder
	b.WriteString("#!/bin/sh\n")
	if h.log != "" {
		log := "'" + filepath.ToSlash(h.log) + "'"
		b.WriteString("{ printf '== %s' '" + name + "'; for a in \"$@\"; do printf ' %s' \"$a\"; done; printf '\\n'; } >> " + log + "\n")
		if h.stdin {
			b.WriteString("cat >> " + log + "\n")
		}
	}
	if h.appendTo != "" {
		b.WriteString("printf '\\n%s\\n' '" + h.appendTo + "' >> \"$1\"\n")
	}
	if h.empty {
		b.WriteString(": > \"$1\"\n")
	}
	if h.print != "" {
		b.WriteString("echo '" + h.print + "'\n")
	}
	b.WriteString("exit " + strconv.Itoa(h.exit) + "\n")
	return b.String()
}

func installShellHook(t testing.TB, dir, name string, h testHook) {
	t.Helper()
	writeHookFile(t, filepath.Join(dir, name), shellHookScript(name, h))
}
