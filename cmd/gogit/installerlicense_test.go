package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const installerLicenseHeader = `{\rtf1\ansi\ansicpg1252\deff0{\fonttbl{\f0\fswiss Segoe UI;}}\fs18`

func readRepositoryFile(t *testing.T, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestTheInstallerLicenseIsAWellFormedRTFHeader(t *testing.T) {
	rtf := readRepositoryFile(t, "packaging/windows/license.rtf")

	if !strings.HasPrefix(rtf, installerLicenseHeader+"\n") {
		t.Fatalf("license.rtf starts with %q, want the RTF header %q", rtf[:min(len(rtf), 80)], installerLicenseHeader)
	}
	for at, b := range []byte(rtf) {
		if b < 0x20 && b != '\n' {
			t.Fatalf("license.rtf holds control byte %#02x at offset %d: a backslash sequence was turned into a control character", b, at)
		}
	}
	if !strings.HasSuffix(rtf, "}\n") {
		t.Fatal("license.rtf does not close its RTF group")
	}
}

func TestTheInstallerLicenseShowsTheTextOfLICENSE(t *testing.T) {
	rtf := readRepositoryFile(t, "packaging/windows/license.rtf")
	license := readRepositoryFile(t, "LICENSE")

	body := strings.TrimSuffix(strings.TrimPrefix(rtf, installerLicenseHeader+"\n"), "}\n")
	unescape := strings.NewReplacer(`\\`, `\`, `\{`, `{`, `\}`, `}`)
	var shown []string
	for line := range strings.Lines(body) {
		text, ok := strings.CutSuffix(strings.TrimSuffix(line, "\n"), `\par`)
		if !ok {
			t.Fatalf("line %q of license.rtf does not end a paragraph", line)
		}
		shown = append(shown, unescape.Replace(text))
	}

	if got, want := strings.Join(shown, "\n"), strings.TrimRight(license, "\n"); got != want {
		t.Fatalf("the installer license differs from LICENSE:\n%s", firstDifference(got, want))
	}
}

func firstDifference(got, want string) string {
	gotLines, wantLines := strings.Split(got, "\n"), strings.Split(want, "\n")
	for i := range max(len(gotLines), len(wantLines)) {
		var g, w string
		if i < len(gotLines) {
			g = gotLines[i]
		}
		if i < len(wantLines) {
			w = wantLines[i]
		}
		if g != w {
			return fmt.Sprintf("line %d: got %q, want %q", i+1, g, w)
		}
	}
	return "no differing line"
}
