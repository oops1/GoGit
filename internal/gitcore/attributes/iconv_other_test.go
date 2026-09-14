//go:build !windows

package attributes

import "testing"

func TestUnmarkedUTFFollowsGlibc(t *testing.T) {
	checkIconvCases(t, decodeToUTF8, []iconvCase{
		{"utf16WithoutBOMIsLittleEndian", "UTF-16LE-BOM", "a\x00", "a", true},
		{"dashlessUTF16Name", "utf16le", "a\x00", "a", true},
		{"dashlessUTF32Name", "UTF32BE", "\x00\x00\x00a", "a", true},
		{"nameWithoutUTFPrefix", "16LE", "a\x00", "", false},
		{"doubledDash", "UTF--16LE", "a\x00", "", false},
	})
	checkIconvCases(t, encodeFromUTF8, []iconvCase{
		{"utf16WritesLittleEndianBOM", "UTF-16", "a", "\xff\xfea\x00", true},
		{"utf32WritesLittleEndianBOM", "utf32", "a", "\xff\xfe\x00\x00a\x00\x00\x00", true},
	})
}
