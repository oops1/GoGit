package ops

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func installTestHook(t testing.TB, dir, name string, h testHook) {
	t.Helper()
	var b strings.Builder
	b.WriteString("@echo off\r\n")
	if h.log != "" {
		log := "\"" + h.log + "\""
		b.WriteString(">>" + log + " echo == " + name + " %*\r\n")
		if h.stdin {
			b.WriteString("findstr \"^\" >>" + log + "\r\n")
		}
	}
	if h.appendTo != "" {
		b.WriteString(">>\"%~1\" echo.\r\n>>\"%~1\" echo " + h.appendTo + "\r\n")
	}
	if h.empty {
		b.WriteString("type nul >\"%~1\"\r\n")
	}
	if h.print != "" {
		b.WriteString("echo " + h.print + "\r\n")
	}
	b.WriteString("exit /b " + strconv.Itoa(h.exit) + "\r\n")
	writeHookFile(t, filepath.Join(dir, name+".cmd"), b.String())
}
