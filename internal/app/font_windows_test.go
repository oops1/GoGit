//go:build windows

package app

import (
	"path/filepath"
	"testing"
)

func TestSystemInterfaceFontIsVerdanaFromWindowsFonts(t *testing.T) {
	t.Setenv("WINDIR", `C:\Windows`)
	got := platformInterfaceFont()
	want := interfaceFontFiles{dir: filepath.Join(`C:\Windows`, "Fonts"), regular: "verdana.ttf", bold: "verdanab.ttf"}
	if got != want {
		t.Errorf("platformInterfaceFont = %+v, want %+v", got, want)
	}
}

func TestSystemInterfaceFontWithoutWindowsDirectory(t *testing.T) {
	t.Setenv("WINDIR", "")
	if got := platformInterfaceFont(); got != (interfaceFontFiles{}) {
		t.Errorf("platformInterfaceFont = %+v, want none", got)
	}
}
