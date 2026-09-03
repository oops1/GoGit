package config

import "testing"

func TestWildMatchFollowsGitPathnameRules(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		text    string
		icase   bool
		want    bool
	}{
		{"literalEqual", "abc", "abc", false, true},
		{"literalDiffers", "abc", "abd", false, false},
		{"literalShorterText", "abc", "ab", false, false},
		{"literalLongerText", "abc", "abcd", false, false},
		{"emptyPatternEmptyText", "", "", false, true},
		{"emptyPatternNonEmptyText", "", "a", false, false},
		{"starMatchesWithinComponent", "a*c", "abbbc", false, true},
		{"starDoesNotCrossSlash", "a*c", "ab/c", false, false},
		{"trailingStarStopsAtSlash", "a*", "ab/c", false, false},
		{"trailingStarMatchesRest", "a*", "abc", false, true},
		{"starMatchesEmpty", "ab*", "ab", false, true},
		{"doubleStarCrossesSlashes", "a/**/d", "a/b/c/d", false, true},
		{"doubleStarMatchesNothing", "a/**/d", "a/d", false, true},
		{"trailingDoubleStarMatchesEverything", "a/**", "a/b/c", false, true},
		{"leadingDoubleStar", "**/x", "a/b/x", false, true},
		{"leadingDoubleStarMatchesBare", "**/x", "x", false, true},
		{"doubleStarMustBeWholeComponent", "a**b", "axxb", false, false},
		{"doubleStarFollowedByText", "**x", "ax", false, false},
		{"questionMatchesOneChar", "a?c", "abc", false, true},
		{"questionDoesNotMatchSlash", "a?c", "a/c", false, false},
		{"questionNeedsAChar", "a?", "a", false, false},
		{"bracketMatches", "a[bc]d", "acd", false, true},
		{"bracketRejects", "a[bc]d", "aed", false, false},
		{"bracketRange", "a[0-9]d", "a5d", false, true},
		{"bracketRangeRejects", "a[0-9]d", "axd", false, false},
		{"bracketNegatedBang", "a[!b]d", "acd", false, true},
		{"bracketNegatedCaret", "a[^b]d", "abd", false, false},
		{"bracketNeverMatchesSlash", "a[/]d", "a/d", false, false},
		{"bracketWithLiteralClose", "a[]]d", "a]d", false, true},
		{"bracketWithEscapedChar", `a[\]]d`, "a]d", false, true},
		{"bracketWithEscapedRangeEnd", `a[a-\c]d`, "abd", false, true},
		{"bracketWithTrailingDash", "a[b-]d", "a-d", false, true},
		{"bracketUnterminated", "a[bc", "abc", false, false},
		{"bracketEscapeAtEnd", `a[\`, "ab", false, false},
		{"bracketRangeEscapeAtEnd", `a[b-\`, "ab", false, false},
		{"bracketEmpty", "a[]", "a]", false, false},
		{"escapedStarIsLiteral", `a\*b`, "a*b", false, true},
		{"escapedStarRejectsOther", `a\*b`, "axb", false, false},
		{"trailingBackslashIsLiteral", `a\`, `a\`, false, true},
		{"caseFoldedLiteral", "ABC", "abc", true, true},
		{"caseSensitiveLiteral", "ABC", "abc", false, false},
		{"caseFoldedBracket", "[A-Z]bc", "abc", true, true},
		{"caseFoldedBracketUpperText", "[a-z]bc", "Abc", true, true},
		{"caseFoldedRangeRejects", "[a-c]x", "Zx", true, false},
		{"starThenLiteralBacktracks", "*bc", "abcbc", false, true},
		{"doubleStarThenLiteralBacktracks", "**/bc", "a/x/bc", false, true},
		{"doubleStarAtRootMatchesDeep", "**", "a/b/c", false, true},
		{"nestedDoubleStar", "a/**/b/**/c", "a/1/b/2/3/c", false, true},
		{"doubleStarAfterStarIsMalformed", "*/**x", "a/b", false, false},
		{"starWithNothingLeft", "a*", "a", false, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := wildMatch(tc.pattern, tc.text, tc.icase); got != tc.want {
				t.Fatalf("wildMatch(%q, %q, %v) = %v, want %v", tc.pattern, tc.text, tc.icase, got, tc.want)
			}
		})
	}
}
