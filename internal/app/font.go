package app

import (
	"path/filepath"

	"github.com/oops1/headless-gui/v4/widget"
)

const (
	interfaceFontName     = "GoGitUI"
	interfaceFontBoldName = "GoGitUI-Bold"
)

var systemInterfaceFont = platformInterfaceFont

type fontRegistrar interface {
	RegisterFontFile(name, path string) error
	SetDefaultFont(name string) bool
}

type interfaceFontFiles struct {
	dir     string
	regular string
	bold    string
}

func useInterfaceFont(eng fontRegistrar, files interfaceFontFiles) bool {
	if files.dir == "" || files.regular == "" {
		return false
	}
	if eng.RegisterFontFile(interfaceFontName, filepath.Join(files.dir, files.regular)) != nil {
		return false
	}
	if !eng.SetDefaultFont(interfaceFontName) {
		return false
	}
	if files.bold == "" {
		return true
	}
	bold := filepath.Join(files.dir, files.bold)
	if eng.RegisterFontFile(interfaceFontBoldName, bold) == nil {
		_ = eng.RegisterFontFile(widget.BuiltinFontBold, bold)
	}
	return true
}
