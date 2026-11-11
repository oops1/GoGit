//go:build windows

package app

import (
	"os"
	"path/filepath"
)

func platformInterfaceFont() interfaceFontFiles {
	windir := os.Getenv("WINDIR")
	if windir == "" {
		return interfaceFontFiles{}
	}
	return interfaceFontFiles{dir: filepath.Join(windir, "Fonts"), regular: "verdana.ttf", bold: "verdanab.ttf"}
}
