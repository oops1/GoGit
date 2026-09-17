package linelog

import (
	"errors"
	"testing"
)

func TestTranslateBREMapsPosixBasicSyntax(t *testing.T) {
	cases := []struct {
		pattern string
		want    string
	}{
		{pattern: `abc`, want: `abc`},
		{pattern: `a(b)+c?{d}|e`, want: `a\(b\)\+c\?\{d\}\|e`},
		{pattern: `\(ab\)\{2\}`, want: `(ab){2}`},
		{pattern: `a\+b\?c\|d`, want: `a+b?c|d`},
		{pattern: `*star`, want: `\*star`},
		{pattern: `^*star`, want: `^\*star`},
		{pattern: `\(*x\)`, want: `(\*x)`},
		{pattern: `a*`, want: `a*`},
		{pattern: `^a^b`, want: `^a\^b`},
		{pattern: `a$b$`, want: `a\$b$`},
		{pattern: `\(a$\)`, want: `(a$)`},
		{pattern: `a$\|b`, want: `a$|b`},
		{pattern: `.x`, want: `.x`},
		{pattern: `\<word\>`, want: `\bword\b`},
		{pattern: `\w\W\s\S\b\B`, want: `\w\W\s\S\b\B`},
		{pattern: "\\`a\\'", want: `\Aa\z`},
		{pattern: `\.\*\[\/`, want: `\.\*\[/`},
		{pattern: `[abc]`, want: `[abc]`},
		{pattern: `[^a-z]`, want: `[^\na-z]`},
		{pattern: `[]a]`, want: `[\]a]`},
		{pattern: `[^]a]`, want: `[^\n\]a]`},
		{pattern: `[[:alpha:]_]`, want: `[[:alpha:]_]`},
		{pattern: `[\^[]`, want: `[\\\^\[]`},
		{pattern: `[[:x]`, want: `[\[:x]`},
		{pattern: "é", want: "é"},
	}
	for _, c := range cases {
		got, err := translateBRE(c.pattern)
		if err != nil || got != c.want {
			t.Errorf("translateBRE(%q) = %q, %v; want %q", c.pattern, got, err, c.want)
		}
	}
}

func TestTranslateBRERejectsWhatItCannotExpress(t *testing.T) {
	for _, pattern := range []string{`abc\`, `\(a\)\1`, `[abc`, `[]`} {
		if _, err := translateBRE(pattern); !errors.Is(err, ErrPattern) {
			t.Errorf("translateBRE(%q) returned %v", pattern, err)
		}
	}
}

func TestCompileBREReportsBadPatterns(t *testing.T) {
	for _, pattern := range []string{`a\{2,1\}`, `[z-a]`, `x\`} {
		if _, err := compileBRE(pattern); !errors.Is(err, ErrPattern) {
			t.Errorf("compileBRE(%q) returned %v", pattern, err)
		}
	}
	re, err := compileBRE(`^b.*d$`)
	if err != nil || re.FindString("a\nbcd\ne") != "bcd" {
		t.Fatalf("compileBRE matched %v, %v", re, err)
	}
}
