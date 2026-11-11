package app

import (
	"errors"
	"path/filepath"
	"slices"
	"testing"

	"github.com/oops1/headless-gui/v4/engine"
	"github.com/oops1/headless-gui/v4/widget"
)

type recordingFontRegistrar struct {
	failPaths   map[string]bool
	refuseDef   bool
	registered  []string
	defaultFont string
}

func (r *recordingFontRegistrar) RegisterFontFile(name, path string) error {
	if r.failPaths[filepath.Base(path)] {
		return errors.New("unreadable font")
	}
	r.registered = append(r.registered, name+"="+filepath.Base(path))
	return nil
}

func (r *recordingFontRegistrar) SetDefaultFont(name string) bool {
	if r.refuseDef {
		return false
	}
	r.defaultFont = name
	return true
}

func TestUseInterfaceFontRegistersRegularAndBold(t *testing.T) {
	files := interfaceFontFiles{dir: "fonts", regular: "ui.ttf", bold: "uib.ttf"}
	tests := []struct {
		name        string
		files       interfaceFontFiles
		reg         recordingFontRegistrar
		wantOK      bool
		wantDefault string
		wantFonts   []string
	}{
		{
			name:        "regular and bold",
			files:       files,
			wantOK:      true,
			wantDefault: interfaceFontName,
			wantFonts:   []string{interfaceFontName + "=ui.ttf", interfaceFontBoldName + "=uib.ttf", widget.BuiltinFontBold + "=uib.ttf"},
		},
		{
			name:        "no bold file configured",
			files:       interfaceFontFiles{dir: "fonts", regular: "ui.ttf"},
			wantOK:      true,
			wantDefault: interfaceFontName,
			wantFonts:   []string{interfaceFontName + "=ui.ttf"},
		},
		{
			name:        "unreadable bold keeps the regular font",
			files:       files,
			reg:         recordingFontRegistrar{failPaths: map[string]bool{"uib.ttf": true}},
			wantOK:      true,
			wantDefault: interfaceFontName,
			wantFonts:   []string{interfaceFontName + "=ui.ttf"},
		},
		{
			name:  "no font directory",
			files: interfaceFontFiles{regular: "ui.ttf"},
		},
		{
			name:  "no regular file",
			files: interfaceFontFiles{dir: "fonts"},
		},
		{
			name:  "unreadable regular font",
			files: files,
			reg:   recordingFontRegistrar{failPaths: map[string]bool{"ui.ttf": true}},
		},
		{
			name:      "engine refuses the default font",
			files:     files,
			reg:       recordingFontRegistrar{refuseDef: true},
			wantFonts: []string{interfaceFontName + "=ui.ttf"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg := tt.reg
			if got := useInterfaceFont(&reg, tt.files); got != tt.wantOK {
				t.Fatalf("useInterfaceFont = %v, want %v", got, tt.wantOK)
			}
			if reg.defaultFont != tt.wantDefault {
				t.Errorf("default font %q, want %q", reg.defaultFont, tt.wantDefault)
			}
			if !slices.Equal(reg.registered, tt.wantFonts) {
				t.Errorf("registered %v, want %v", reg.registered, tt.wantFonts)
			}
		})
	}
}

func TestUseInterfaceFontWithSystemFontOnRealEngine(t *testing.T) {
	files := platformInterfaceFont()
	if files.dir == "" {
		t.Skip("this platform has no interface font")
	}
	eng := engine.New(100, 80, 30)
	defer eng.Stop()
	if !useInterfaceFont(eng, files) {
		t.Skipf("interface font %s is not installed", filepath.Join(files.dir, files.regular))
	}
	fonts := eng.AvailableFonts()
	for _, name := range []string{interfaceFontName, interfaceFontBoldName, widget.BuiltinFontBold} {
		if !slices.Contains(fonts, name) {
			t.Errorf("font %q not registered: %v", name, fonts)
		}
	}
}
