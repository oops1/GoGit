package linelog

import (
	"errors"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/posixre"
)

func TestCompilePatternReportsBadPatterns(t *testing.T) {
	for _, pattern := range []string{`a\{2,1\}`, `[z-a]`, `x\`, `\(a\)\1`, `[abc`, `[]`} {
		if _, err := compilePattern(pattern); !errors.Is(err, ErrPattern) {
			t.Errorf("compilePattern(%q) returned %v", pattern, err)
		}
	}
	if _, err := compilePattern(`\(a\)\1`); !errors.Is(err, posixre.ErrUnsupported) {
		t.Errorf("a back reference returned %v", err)
	}
}

func TestCompilePatternMatchesLinesAsGitDoes(t *testing.T) {
	re, err := compilePattern(`^b.*d$`)
	if err != nil {
		t.Fatal(err)
	}
	loc := re.FindIndex([]byte("a\nbcd\ne"))
	if len(loc) != 2 || loc[0] != 2 || loc[1] != 5 {
		t.Fatalf("compilePattern matched %v", loc)
	}
	negated, err := compilePattern(`b[^x]*`)
	if err != nil {
		t.Fatal(err)
	}
	if loc := negated.FindIndex([]byte("abc\nd")); len(loc) != 2 || loc[1] != 3 {
		t.Fatalf("a negated list crossed a line end: %v", loc)
	}
}
