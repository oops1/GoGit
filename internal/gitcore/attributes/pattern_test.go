package attributes

import (
	"slices"
	"testing"
)

func TestParsePatternSplitsNegationAndDirectoryMarker(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		text     string
		negative bool
		dirOnly  bool
		noDir    bool
	}{
		{"plainName", "foo", "foo", false, false, true},
		{"negated", "!foo", "foo", true, false, true},
		{"directoryOnly", "foo/", "foo", false, true, true},
		{"negatedDirectory", "!foo/", "foo", true, true, true},
		{"anchoredByLeadingSlash", "/foo", "/foo", false, false, false},
		{"anchoredByInnerSlash", "a/b", "a/b", false, false, false},
		{"anchoredDirectory", "a/b/", "a/b", false, true, false},
		{"escapedBangIsLiteral", `\!foo`, `\!foo`, false, false, true},
		{"emptyPattern", "", "", false, false, true},
		{"bareSlash", "/", "", false, true, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parsePattern(tc.input, "")
			if got.text != tc.text || got.negative != tc.negative || got.dirOnly != tc.dirOnly || got.noDir != tc.noDir {
				t.Fatalf("parsePattern(%q) = %+v, want text %q negative %v dirOnly %v noDir %v",
					tc.input, got, tc.text, tc.negative, tc.dirOnly, tc.noDir)
			}
		})
	}
}

func TestPatternMatchAppliesGitPathRules(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		base    string
		path    string
		isDir   bool
		icase   bool
		want    bool
	}{
		{"basenameAnywhere", "foo", "", "a/b/foo", false, false, true},
		{"basenameIgnoresDirectories", "foo", "", "foo/b", false, false, false},
		{"directoryOnlyNeedsDirectory", "foo/", "", "foo", false, false, false},
		{"directoryOnlyMatchesDirectory", "foo/", "", "foo", true, false, true},
		{"anchoredAtRoot", "/foo", "", "foo", false, false, true},
		{"anchoredAtRootRejectsNested", "/foo", "", "a/foo", false, false, false},
		{"innerSlashIsAnchored", "a/b", "", "a/b", false, false, true},
		{"innerSlashRejectsDeeper", "a/b", "", "x/a/b", false, false, false},
		{"doubleStarInBasenameCrossesNothing", "a**b", "", "axxb", false, false, true},
		{"baseRestrictsPath", "b", "sub", "sub/b", false, false, true},
		{"baseRejectsOtherDirectory", "/b", "sub", "other/b", false, false, false},
		{"baseRejectsShortPath", "/b", "sub", "sub", false, false, false},
		{"baseRejectsMissingSeparator", "/b", "sub", "subx/b", false, false, false},
		{"baseFoldsCase", "/b", "Sub", "sub/b", false, true, true},
		{"baseDoesNotFoldWhenExact", "/b", "Sub", "sub/b", false, false, false},
		{"anchoredEmptyPathNeverMatches", "/x", "", "", false, false, false},
		{"caseFoldedBasename", "FOO", "", "a/foo", false, true, true},
		{"caseSensitiveBasename", "FOO", "", "a/foo", false, false, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := parsePattern(tc.pattern, tc.base)
			if got := p.match(tc.path, tc.isDir, tc.icase); got != tc.want {
				t.Fatalf("pattern %q base %q match(%q, %v, %v) = %v, want %v",
					tc.pattern, tc.base, tc.path, tc.isDir, tc.icase, got, tc.want)
			}
		})
	}
}

func TestEqualFoldComparesAsciiBytesOnly(t *testing.T) {
	tests := []struct {
		name  string
		a     string
		b     string
		icase bool
		want  bool
	}{
		{"exactWhenCaseSensitive", "abc", "abc", false, true},
		{"differentWhenCaseSensitive", "ABC", "abc", false, false},
		{"foldedEqual", "ABC", "abc", true, true},
		{"foldedDifferentLength", "AB", "abc", true, false},
		{"foldedDifferentBytes", "ABD", "abc", true, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := equalFold(tc.a, tc.b, tc.icase); got != tc.want {
				t.Fatalf("equalFold(%q, %q, %v) = %v, want %v", tc.a, tc.b, tc.icase, got, tc.want)
			}
		})
	}
}

func TestBaseNameAndParentDirSplitSlashPaths(t *testing.T) {
	tests := []struct {
		name   string
		path   string
		base   string
		parent string
	}{
		{"nested", "a/b/c", "c", "a/b"},
		{"topLevel", "a", "a", ""},
		{"empty", "", "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := baseName(tc.path); got != tc.base {
				t.Fatalf("baseName(%q) = %q, want %q", tc.path, got, tc.base)
			}
			if got := parentDir(tc.path); got != tc.parent {
				t.Fatalf("parentDir(%q) = %q, want %q", tc.path, got, tc.parent)
			}
		})
	}
}

func TestNormalizePathStripsPrefixAndTrailingSlashes(t *testing.T) {
	tests := []struct {
		name  string
		path  string
		clean string
		isDir bool
	}{
		{"plain", "a/b", "a/b", false},
		{"currentDirectoryPrefix", "./a/b", "a/b", false},
		{"trailingSlash", "a/b/", "a/b", true},
		{"repeatedTrailingSlash", "a/b//", "a/b", true},
		{"root", "/", "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			clean, isDir := normalizePath(tc.path)
			if clean != tc.clean || isDir != tc.isDir {
				t.Fatalf("normalizePath(%q) = (%q, %v), want (%q, %v)", tc.path, clean, isDir, tc.clean, tc.isDir)
			}
		})
	}
}

func TestLinesSplitsAndDropsByteOrderMark(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{"empty", "", nil},
		{"singleLineWithoutNewline", "a", []string{"a"}},
		{"singleLineWithNewline", "a\n", []string{"a"}},
		{"twoLines", "a\nb\n", []string{"a", "b"}},
		{"blankLineInside", "a\n\nb\n", []string{"a", "", "b"}},
		{"byteOrderMark", utf8BOM + "a\n", []string{"a"}},
		{"onlyNewline", "\n", []string{""}},
		{"carriageReturnKept", "a\r\n", []string{"a\r"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := lines([]byte(tc.input)); !slices.Equal(got, tc.want) {
				t.Fatalf("lines(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestTrimTrailingSpacesKeepsEscapedSpaces(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"nothingToTrim", "abc", "abc"},
		{"trailingSpaces", "abc   ", "abc"},
		{"escapedTrailingSpace", `abc\ `, `abc\ `},
		{"escapedThenUnescaped", `abc\  `, `abc\ `},
		{"innerSpacesKept", "a b c", "a b c"},
		{"trailingBackslash", `abc\`, `abc\`},
		{"onlySpaces", "   ", ""},
		{"tabIsNotTrimmed", "abc\t", "abc\t"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := trimTrailingSpaces(tc.input); got != tc.want {
				t.Fatalf("trimTrailingSpaces(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestUnquoteCFollowsGitQuoting(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
		rest  string
		ok    bool
	}{
		{"notQuoted", "abc", "", "", false},
		{"empty", "", "", "", false},
		{"simple", `"abc"`, "abc", "", true},
		{"withRest", `"abc" def`, "abc", " def", true},
		{"spaceInside", `"a b"`, "a b", "", true},
		{"escapedQuote", `"a\"b"`, `a"b`, "", true},
		{"escapedBackslash", `"a\\b"`, `a\b`, "", true},
		{"controlEscapes", `"\a\b\f\n\r\t\v"`, "\a\b\f\n\r\t\v", "", true},
		{"octalEscape", `"\101"`, "A", "", true},
		{"octalZero", `"\000"`, "\x00", "", true},
		{"unterminated", `"abc`, "", "", false},
		{"backslashAtEnd", `"abc\`, "", "", false},
		{"unknownEscape", `"a\qb"`, "", "", false},
		{"shortOctal", `"\10"`, "", "", false},
		{"badOctalDigit", `"\19a"`, "", "", false},
		{"badSecondOctalDigit", `"\118"`, "", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, rest, ok := unquoteC(tc.input)
			if got != tc.want || rest != tc.rest || ok != tc.ok {
				t.Fatalf("unquoteC(%q) = (%q, %q, %v), want (%q, %q, %v)",
					tc.input, got, rest, ok, tc.want, tc.rest, tc.ok)
			}
		})
	}
}
