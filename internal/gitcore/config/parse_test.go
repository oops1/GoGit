package config

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestParseReadsVariablesWithGitSemantics(t *testing.T) {
	tests := []struct {
		name string
		text string
		want []string
	}{
		{"emptyInputHasNoVariables", "", nil},
		{"byteOrderMarkIsSkipped", "\xef\xbb\xbf[a]\n\tb = c\n", []string{"a.b=c"}},
		{"sectionAndKeyAreLowercased", "[Core]\n\tBare = TRUE\n", []string{"core.bare=TRUE"}},
		{"subsectionKeepsCase", "[core \"MiXeD\"]\n\tk = v\n", []string{"core.MiXeD.k=v"}},
		{"legacyDottedSubsectionIsLowercased", "[A.B]\n\tx = 1\n", []string{"a.b.x=1"}},
		{"legacyDottedSubsectionMayBeEmpty", "[a.]\n\tx = 1\n", []string{"a..x=1"}},
		{"sectionHeaderMayShareLineWithEntry", "[a] b = c\n", []string{"a.b=c"}},
		{"twoHeadersMayShareLine", "[a][b] c = d\n", []string{"b.c=d"}},
		{"keyWithoutEqualsIsBoolTrue", "[a]\n\tb\n", []string{"a.b"}},
		{"keyWithoutEqualsAtEndOfFile", "[a]\n\tb", []string{"a.b"}},
		{"emptyValue", "[a]\n\tb =\n", []string{"a.b="}},
		{"emptyQuotedValue", "[a]\n\tb = \"\"\n", []string{"a.b="}},
		{"leadingAndTrailingBlanksAreTrimmed", "[a]\n\tb =   c d  \n", []string{"a.b=c d"}},
		{"quotesPreserveBlanks", "[a]\n\tb = \"  c  \"\n", []string{"a.b=  c  "}},
		{"quotesMayBeSpliced", "[a]\n\tb = x\"y\"z\n", []string{"a.b=xyz"}},
		{"hashStartsComment", "[a]\n\tb = c # d\n", []string{"a.b=c"}},
		{"semicolonStartsComment", "[a]\n\tb = c ; d\n", []string{"a.b=c"}},
		{"commentCharsInsideQuotesAreLiteral", "[a]\n\tb = \"c;d#e\"\n", []string{"a.b=c;d#e"}},
		{"tabEscape", "[a]\n\tb = x\\ty\n", []string{"a.b=x\ty"}},
		{"newlineEscape", "[a]\n\tb = x\\ny\n", []string{"a.b=x\ny"}},
		{"backspaceEscape", "[a]\n\tb = x\\by\n", []string{"a.b=x\by"}},
		{"backslashEscape", "[a]\n\tb = x\\\\y\n", []string{"a.b=x\\y"}},
		{"quoteEscape", "[a]\n\tb = x\\\"y\n", []string{"a.b=x\"y"}},
		{"escapedTabSurvivesTrimming", "[a]\n\tb = x\\t\n", []string{"a.b=x\t"}},
		{"backslashJoinsLines", "[a]\n\tb = one\\\ntwo\n", []string{"a.b=onetwo"}},
		{"backslashJoinsCRLFLines", "[a]\r\n\tb = one\\\r\ntwo\r\n", []string{"a.b=onetwo"}},
		{"backslashAtEndOfFileEndsValue", "[a]\n\tb = one\\", []string{"a.b=one"}},
		{"blanksAfterJoinAreKept", "[a]\n\tb = one\\\n   two\n", []string{"a.b=one   two"}},
		{"crlfIsALineBreak", "[a]\r\n\tb = c\r\n", []string{"a.b=c"}},
		{"loneCarriageReturnIsBlank", "[a]\n\tb = c\r\n", []string{"a.b=c"}},
		{"subsectionEscapesAreLiteral", "[a \"x\\\"y\\\\z\\t\"]\n\tk = v\n", []string{"a.x\"y\\zt.k=v"}},
		{"subsectionMayBeEmpty", "[a \"\"]\n\tk = v\n", []string{"a..k=v"}},
		{"sectionNameMayBeEmptyBeforeSubsection", "[ \"x\"]\n\tk = v\n", []string{".x.k=v"}},
		{"blanksAllowedAroundSubsection", "[a \t \"x\"]\n\tk = v\n", []string{"a.x.k=v"}},
		{"commentsAndBlankLinesAreIgnored", "# c\n;d\n\n\t\n[a]\n\tb = c\n", []string{"a.b=c"}},
		{"entryBeforeAnySectionIsNotAddressable", "b = c\n[a]\n\td = e\n", []string{"a.d=e"}},
		{"repeatedKeysAreKept", "[a]\n\tb = 1\n\tb = 2\n", []string{"a.b=1", "a.b=2"}},
		{"reopenedSectionsAccumulate", "[a]\n\tb = 1\n[c]\n\td = 2\n[a]\n\te = 3\n", []string{"a.b=1", "c.d=2", "a.e=3"}},
		{"digitsAndDashesAllowedInNames", "[a-1]\n\tb-2 = v\n", []string{"a-1.b-2=v"}},
		{"commentAfterValueKeepsTrailingBlankTrim", "[a]\n\tb = c   # d\n", []string{"a.b=c"}},
		{"onlyBlanksAfterEqualsIsEmpty", "[a]\n\tb =    \n", []string{"a.b="}},
		{"quotedValueMayContainEscapedNewline", "[a]\n\tb = \"x\\ny\"\n", []string{"a.b=x\ny"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := mustParse(t, tc.text)
			if got := dump(f); !slices.Equal(got, tc.want) {
				t.Errorf("variables = %q, want %q", got, tc.want)
			}
			if got := string(f.Encode()); got != tc.text {
				t.Errorf("Encode round-trip = %q, want %q", got, tc.text)
			}
		})
	}
}

func TestParseRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name string
		text string
		want error
	}{
		{"lineStartingWithPunctuation", "=x\n", ErrSyntax},
		{"lineStartingWithDigit", "1 = 2\n", ErrSyntax},
		{"partialByteOrderMark", "\xef\xbb[a]\n", ErrSyntax},
		{"unterminatedSectionAtEndOfFile", "[a", ErrBadSection},
		{"newlineInsideSectionHeader", "[a\nb]\n", ErrBadSection},
		{"emptySectionHeader", "[]\n", ErrBadSection},
		{"invalidCharacterInSectionName", "[a_b]\n", ErrBadSection},
		{"subsectionWithoutQuote", "[a b]\n", ErrBadSection},
		{"subsectionNotClosedAtEndOfFile", "[a \"b\"", ErrBadSection},
		{"subsectionQuoteNotClosed", "[a \"b\n", ErrBadSection},
		{"subsectionEscapeAtLineEnd", "[a \"b\\\n\"]\n", ErrBadSection},
		{"junkAfterSubsection", "[a \"b\"x]\n", ErrBadSection},
		{"blanksThenEndOfFileInHeader", "[a ", ErrBadSection},
		{"keyFollowedByJunk", "[a]\n\tb ; c\n", ErrBadKey},
		{"keyFollowedByCarriageReturn", "[a]\n\tb\rc\n", ErrBadKey},
		{"unterminatedQuoteInValue", "[a]\n\tb = \"c\n", ErrUnterminatedQuote},
		{"unterminatedQuoteAtEndOfFile", "[a]\n\tb = \"c", ErrUnterminatedQuote},
		{"unknownEscapeSequence", "[a]\n\tb = \\q\n", ErrBadEscape},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(tc.text))
			if !errors.Is(err, tc.want) {
				t.Fatalf("Parse(%q) error = %v, want %v", tc.text, err, tc.want)
			}
			if !strings.Contains(err.Error(), "line ") {
				t.Errorf("error %q does not mention a line number", err)
			}
		})
	}
}

func TestParseReportsLineNumberOfFailure(t *testing.T) {
	_, err := Parse([]byte("[a]\n\tb = c\\\nd\n\te = \\q\n"))
	if err == nil || !strings.Contains(err.Error(), "line 4") {
		t.Fatalf("error = %v, want a mention of line 4", err)
	}
}

func TestVariablesReportLineNumbers(t *testing.T) {
	f := mustParse(t, "# c\n[a]\n\tb = one\\\ntwo\n\tc = 3\n")
	var lines []int
	for v := range f.Variables() {
		lines = append(lines, v.Line)
	}
	if want := []int{3, 5}; !slices.Equal(lines, want) {
		t.Fatalf("lines = %v, want %v", lines, want)
	}
}

func TestVariablesStopsWhenConsumerBreaks(t *testing.T) {
	f := mustParse(t, "[a]\n\tb = 1\n\tc = 2\n")
	count := 0
	for range f.Variables() {
		count++
		break
	}
	if count != 1 {
		t.Fatalf("visited %d variables, want 1", count)
	}
}

func TestEncodeReproducesFixturesByteForByte(t *testing.T) {
	for _, name := range []string{
		"local.config", "global.config", "tricky.config", "crlf.config",
		"include-level1.config", "include-level2.config",
	} {
		t.Run(name, func(t *testing.T) {
			text := fixture(t, name)
			f := mustParse(t, text)
			if got := string(f.Encode()); got != text {
				t.Fatalf("Encode round-trip differs for %s", name)
			}
		})
	}
}

func TestTrickyFixtureMatchesGitSemantics(t *testing.T) {
	f := mustParse(t, fixture(t, "tricky.config"))
	want := []string{
		"core.bare=true",
		"core.MiXeD.key=Value",
		"core.MiXeD.key=Second",
		"a.b.x=1",
		"quoted.spaced=  keep  ",
		"quoted.tab=a\tb",
		"quoted.nl=a\nb",
		"quoted.bs=a\\b",
		"quoted.dq=a\"b",
		"quoted.back=a\bb",
		"quoted.cont=onetwo",
		"quoted.hash=value",
		"quoted.semi=value",
		"quoted.inquote=a;b#c",
		"quoted.novalue",
		"quoted.empty=",
		"quoted.emptyq=",
		"quoted.trail=value",
		"quoted.joined=abc",
		"core.reopened=yes",
		"tail.last=x",
	}
	if got := dump(f); !slices.Equal(got, want) {
		t.Fatalf("variables =\n%q\nwant\n%q", got, want)
	}
}
