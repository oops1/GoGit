package main

import (
	"testing"

	"golang.org/x/sys/windows"
)

func TestStartupMessageOpensAWarningBoxWithTheTitleAndText(t *testing.T) {
	var gotTitle, gotText string
	var gotStyle uint32
	previous := messageBox
	messageBox = func(_ windows.HWND, text, caption *uint16, style uint32) (int32, error) {
		gotTitle = windows.UTF16PtrToString(caption)
		gotText = windows.UTF16PtrToString(text)
		gotStyle = style
		return 1, nil
	}
	t.Cleanup(func() { messageBox = previous })

	nativeStartupMessage("Go.Git", "config.toml could not be read")

	if gotTitle != "Go.Git" || gotText != "config.toml could not be read" {
		t.Fatalf("message box got title %q and text %q", gotTitle, gotText)
	}
	if want := uint32(windows.MB_OK | windows.MB_ICONWARNING | windows.MB_SETFOREGROUND); gotStyle != want {
		t.Fatalf("message box style = %#x, want %#x", gotStyle, want)
	}
}
