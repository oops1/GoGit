package main

import "golang.org/x/sys/windows"

func nativeStartupMessage(title, text string) {
	caption, _ := windows.UTF16PtrFromString(title)
	body, _ := windows.UTF16PtrFromString(text)
	_, _ = windows.MessageBox(0, body, caption, windows.MB_OK|windows.MB_ICONWARNING|windows.MB_SETFOREGROUND)
}
