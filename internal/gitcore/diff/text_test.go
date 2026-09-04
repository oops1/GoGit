package diff

import (
	"slices"
	"strings"
	"testing"
)

func TestSplitLinesKeepsTheTerminators(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"empty input", "", nil},
		{"one terminated line", "a\n", []string{"a\n"}},
		{"one bare line", "a", []string{"a"}},
		{"missing final newline", "a\nb", []string{"a\n", "b"}},
		{"blank lines", "\n\n", []string{"\n", "\n"}},
		{"carriage returns stay", "a\r\nb\r\n", []string{"a\r\n", "b\r\n"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := splitLines([]byte(c.in)); !slices.Equal(got, c.want) {
				t.Errorf("splitLines(%q) returned %q instead of %q", c.in, got, c.want)
			}
		})
	}
}

func TestLineTextStripsOnlyTheTrailingNewline(t *testing.T) {
	text, newline := lineText("a\n")
	if text != "a" || !newline {
		t.Errorf("lineText(\"a\\n\") returned (%q, %v)", text, newline)
	}
	text, newline = lineText("a")
	if text != "a" || newline {
		t.Errorf("lineText(\"a\") returned (%q, %v)", text, newline)
	}
}

func TestLineKeyAppliesTheWhitespaceOption(t *testing.T) {
	cases := []struct {
		name   string
		record string
		option Whitespace
		want   string
	}{
		{"no option keeps the record", "  a  b \n", 0, "  a  b \n"},
		{"all space is dropped", "  a  b \n", IgnoreAllSpace, "ab"},
		{"space runs collapse", "  a  b \n", IgnoreSpaceChange, " a b"},
		{"space at the end is dropped", "  a  b \n", IgnoreSpaceAtEOL, "  a  b"},
		{"all space wins over the other flags", " a ", IgnoreAllSpace | IgnoreSpaceChange, "a"},
		{"a record without space is returned as is", "ab\n", IgnoreAllSpace, "ab"},
		{"an inner tab collapses to one space", "a\t\tb", IgnoreSpaceChange, "a b"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := lineKey(c.record, c.option); got != c.want {
				t.Errorf("lineKey(%q, %d) returned %q instead of %q", c.record, c.option, got, c.want)
			}
		})
	}
}

func TestStripSpaceLeavesRecordsWithoutSpaceUntouched(t *testing.T) {
	if got := stripSpace("abc"); got != "abc" {
		t.Errorf("stripSpace returned %q instead of %q", got, "abc")
	}
}

func TestIsBlankLineFollowsTheWhitespaceOption(t *testing.T) {
	cases := []struct {
		name   string
		record string
		option Whitespace
		want   bool
	}{
		{"a bare newline is blank", "\n", 0, true},
		{"an empty record is blank", "", 0, true},
		{"spaces are not blank without an option", "  \n", 0, false},
		{"spaces are blank when space is ignored", "  \n", IgnoreAllSpace, true},
		{"text is never blank", "a\n", IgnoreAllSpace, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isBlankLine(c.record, c.option); got != c.want {
				t.Errorf("isBlankLine(%q, %d) returned %v instead of %v", c.record, c.option, got, c.want)
			}
		})
	}
}

func TestLineIndentCountsTabsToTheNextStop(t *testing.T) {
	cases := []struct {
		name   string
		record string
		want   int
	}{
		{"no indent", "a\n", 0},
		{"two spaces", "  a\n", 2},
		{"one tab", "\ta\n", 8},
		{"space then tab", " \ta\n", 8},
		{"tab then space", "\t a\n", 9},
		{"a blank line has no indent", "\n", -1},
		{"a long indent is capped", strings.Repeat(" ", maxIndent+50) + "a\n", maxIndent},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := lineIndent(c.record); got != c.want {
				t.Errorf("lineIndent(%q) returned %d instead of %d", c.record, got, c.want)
			}
		})
	}
}

func TestIsSpaceByteAcceptsTheWhitespaceSet(t *testing.T) {
	for at := range len(spaceChars) {
		if !isSpaceByte(spaceChars[at]) {
			t.Errorf("isSpaceByte(%q) returned false", spaceChars[at])
		}
	}
	if isSpaceByte('a') {
		t.Error("isSpaceByte('a') returned true")
	}
}

func TestBogosqrtGrowsWithTheInput(t *testing.T) {
	cases := []struct {
		in   int
		want int
	}{
		{0, 1},
		{1, 2},
		{4, 4},
		{16, 8},
		{4003, 64},
	}
	for _, c := range cases {
		if got := bogosqrt(c.in); got != c.want {
			t.Errorf("bogosqrt(%d) returned %d instead of %d", c.in, got, c.want)
		}
	}
}
