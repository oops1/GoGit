//go:build !windows

package main

import (
	"fmt"
	"io"
	"os"
)

var startupMessageOutput io.Writer = os.Stderr

func nativeStartupMessage(title, text string) {
	_, _ = fmt.Fprintf(startupMessageOutput, "%s: %s\n", title, text)
}
