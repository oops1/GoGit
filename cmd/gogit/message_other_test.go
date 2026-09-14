//go:build !windows

package main

import (
	"bytes"
	"testing"
)

func TestNativeStartupMessageWritesToStandardErrorOutsideWindows(t *testing.T) {
	var buf bytes.Buffer
	prev := startupMessageOutput
	startupMessageOutput = &buf
	t.Cleanup(func() { startupMessageOutput = prev })
	nativeStartupMessage("Go.Git", "broken")
	if buf.String() != "Go.Git: broken\n" {
		t.Fatalf("output = %q", buf.String())
	}
}
