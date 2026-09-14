package attributes

import "testing"

func TestUnmarkedUTFFollowsLibiconv(t *testing.T) {
	checkIconvCases(t, decodeToUTF8, []iconvCase{
		{"utf16WithoutBOMIsBigEndian", "UTF-16LE-BOM", "\x00a", "a", true},
		{"dashlessUTF16NameIsUnknown", "utf16le", "a\x00", "", false},
	})
	checkIconvCases(t, encodeFromUTF8, []iconvCase{
		{"utf16WritesBigEndianBOM", "UTF-16", "a", "\xfe\xff\x00a", true},
		{"utf32WritesBigEndianBOM", "UTF-32", "a", "\x00\x00\xfe\xff\x00\x00\x00a", true},
		{"dashlessUTF32NameIsUnknown", "UTF32", "a", "", false},
	})
}
