package posixre

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestTranslateWritesExtendedExpressionsForGo(t *testing.T) {
	cases := []struct {
		pattern string
		flags   Flags
		want    string
	}{
		{`abc`, Extended, `abc`},
		{`a|b|`, Extended, `a|b|`},
		{`|a||b`, Extended, `|a||b`},
		{`(a|)`, Extended, `(a|)`},
		{`()`, Extended, `()`},
		{`a+b?c*`, Extended, `a+b?c*`},
		{`a**`, Extended, `(?:a*)*`},
		{`a+?{2}`, Extended, `(?:(?:a+)?){2}`},
		{`x{2}y{2,}z{,3}w{1,4}`, Extended, `x{2}y{2,}z{0,3}w{1,4}`},
		{`^a$`, Extended, `^a$`},
		{`a^b$c`, Extended, `a^b$c`},
		{`.`, Extended, `[^\x00]`},
		{`.`, Extended | Newline, `(?m)[^\x00\n]`},
		{`\w\W\s\S`, Extended, `[0-9A-Z_a-z][^0-9A-Z_a-z][\t-\r ][^\t-\r ]`},
		{`\<a\>\bb\Bc`, Extended, `\ba\b\bb\Bc`},
		{"\\`a\\'", Extended, `\Aa\z`},
		{`\(\)\{\}\|\+\?\.\*\\`, Extended, `\(\)\{\}\|\+\?\.\*\\`},
		{`\a\n\t`, Extended, `ant`},
		{`)`, Extended, `\)`},
		{`}`, Extended, `\}`},
		{"a\tb\x01\x7f", Extended, `a\x{09}b\x{01}\x{7f}`},
		{"\xc3\xa9", Extended, `\x{c3}\x{a9}`},
		{`aB1`, Extended | IgnoreCase, `[Aa][Bb]1`},
		{`\a\B`, Extended | IgnoreCase, `[^\x00-\x{10FFFF}]\B`},
		{`\A`, Extended | IgnoreCase, `[Aa]`},
	}
	for _, c := range cases {
		got, err := Translate(c.pattern, c.flags)
		if err != nil || got != c.want {
			t.Errorf("Translate(%q, %d) = %q, %v; want %q", c.pattern, c.flags, got, err, c.want)
		}
	}
}

func TestTranslateWritesBasicExpressionsForGo(t *testing.T) {
	cases := []struct {
		pattern string
		want    string
	}{
		{`a|b(c)+d?{e}`, `a\|b\(c\)\+d\?\{e\}`},
		{`\(ab\)\{2\}`, `(ab){2}`},
		{`a\+b\?c\|d`, `a+b?c|d`},
		{`*star`, `\*star`},
		{`^*star`, `^\*star`},
		{`\(*x\)`, `(\*x)`},
		{`\+x`, `\+x`},
		{`a\|\?b`, `a|\?b`},
		{`a*\+`, `(?:a*)+`},
		{`^a^b`, `^a\^b`},
		{`\(^a\)\|^b`, `(^a)|^b`},
		{`a$b$`, `a\$b$`},
		{`\(a$\)`, `(a$)`},
		{`a$\|b`, `a$|b`},
		{`a\}`, `a\}`},
		{`\(\)`, `()`},
		{`a\{,2\}`, `a{0,2}`},
	}
	for _, c := range cases {
		got, err := Translate(c.pattern, 0)
		if err != nil || got != c.want {
			t.Errorf("Translate(%q) = %q, %v; want %q", c.pattern, got, err, c.want)
		}
	}
}

func TestTranslateWritesBracketExpressionsAsByteSets(t *testing.T) {
	cases := []struct {
		pattern string
		flags   Flags
		want    string
	}{
		{`[abc]`, Extended, `[a-c]`},
		{`[]a]`, Extended, `[\x{5d}a]`},
		{`[^]a]`, Extended, `[^\x{5d}a]`},
		{`[^a]`, Extended | Newline, `(?m)[^\x{a}a]`},
		{`[a-]`, Extended, `[\x{2d}a]`},
		{`[-a]`, Extended, `[\x{2d}a]`},
		{`[--/]`, Extended, `[\x{2d}-\x{2f}]`},
		{`[\^[]`, Extended, `[\x{5b}-\x{5c}\x{5e}]`},
		{`[[:lower:]]`, Extended, `[a-z]`},
		{`[a^]`, Extended, `[\x{5e}a]`},
		{`[[:digit:]_]`, Extended, `[0-9\x{5f}]`},
		{`[[:upper:]]`, Extended, `[A-Z]`},
		{`[[:lower:]]`, Extended | IgnoreCase, `[A-Za-z]`},
		{`[[:upper:]]`, Extended | IgnoreCase, `[A-Za-z]`},
		{`[[:space:][:blank:]]`, Extended, `[\x{9}-\x{d}\x{20}]`},
		{`[[:cntrl:]]`, Extended, `[\x{0}-\x{1f}\x{7f}]`},
		{`[[:xdigit:]]`, Extended, `[0-9A-Fa-f]`},
		{`[[:alnum:]]`, Extended, `[0-9A-Za-z]`},
		{`[[:print:]]`, Extended, `[\x{20}-\x{7e}]`},
		{`[[:graph:]]`, Extended, `[\x{21}-\x{7e}]`},
		{`[[:punct:]]`, Extended, `[\x{21}-\x{2f}\x{3a}-\x{40}\x{5b}-\x{60}\x{7b}-\x{7e}]`},
		{`[[.a.][=c=]]`, Extended, `[ac]`},
		{`[[..]-a]`, Extended, `[\x{0}-a]`},
		{`[[.a.]-c]`, Extended, `[a-c]`},
		{`[a-z]`, Extended | IgnoreCase, `[A-Za-z]`},
		{`[^a-z]`, Extended | IgnoreCase, `[^A-Za-z]`},
		{`[+-a]`, IgnoreCase, `[\x{2b}-Aa]`},
		{"[\xc3\xa9]", Extended, `[\x{a9}\x{c3}]`},
		{"[^\x80-\xff]", Extended, `[^\x{80}-\x{ff}]`},
		{`[[:alpha:]-]`, Extended, `[\x{2d}A-Za-z]`},
	}
	for _, c := range cases {
		got, err := Translate(c.pattern, c.flags)
		if err != nil || got != c.want {
			t.Errorf("Translate(%q, %d) = %q, %v; want %q", c.pattern, c.flags, got, err, c.want)
		}
	}
}

func TestTranslateRejectsWhatGitRejects(t *testing.T) {
	cases := []struct {
		pattern string
		flags   Flags
	}{
		{`*a`, Extended}, {`a|*b`, Extended}, {`(+a)`, Extended}, {`^*`, Extended}, {`{1}`, Extended},
		{`\{1\}`, 0}, {`a**`, 0}, {`a*\{2\}`, 0}, {`\)`, 0}, {`(a`, Extended}, {`\(a`, 0},
		{`a\`, Extended}, {`\1`, Extended}, {`(a)\2`, Extended},
		{`a{`, Extended}, {`a{1`, Extended}, {`a{1,`, Extended}, {`a{}`, Extended}, {`a{x}`, Extended},
		{`a{2,1}`, Extended}, {`a{1,2,3}`, Extended}, {`a{99999}`, Extended}, {`a{1x}`, Extended},
		{`[`, Extended}, {`[^`, Extended}, {`[a`, Extended}, {`[a-`, Extended}, {`[z-a]`, Extended},
		{`[a-z-9]`, Extended}, {`[[:alpha:]-z]`, Extended}, {`[a-[:alpha:]]`, Extended}, {`[a-[=b=]]`, Extended},
		{`[[.ab.]-z]`, Extended}, {`[a-[.bc.]]`, Extended}, {`[[.ab.]]`, Extended}, {`[[=ab=]]`, Extended},
		{`[[:nope:]]`, Extended}, {`[[:alpha`, Extended}, {`[[:`, Extended}, {`[[:x]`, Extended},
		{`[[:` + strings.Repeat("a", 40) + `:]]`, Extended}, {`[Z-a]`, IgnoreCase}, {`[_-a]`, IgnoreCase}, {`a{1001}`, Extended},
		{`[[:alpha:]`, Extended}, {`[[=a=]`, Extended}, {`[a-[.b`, Extended},
	}
	for _, c := range cases {
		if _, err := Compile(c.pattern, c.flags); !errors.Is(err, ErrPattern) {
			t.Errorf("Compile(%q, %d) returned %v", c.pattern, c.flags, err)
		}
	}
}

func TestCompileReportsBackReferencesAsUnsupported(t *testing.T) {
	for _, pattern := range []string{`(a)\1`, `\(a\)\1`} {
		flags := Extended
		if strings.HasPrefix(pattern, `\`) {
			flags = 0
		}
		_, err := Compile(pattern, flags)
		if !errors.Is(err, ErrUnsupported) || errors.Is(err, ErrPattern) {
			t.Errorf("Compile(%q) returned %v", pattern, err)
		}
	}
}

func TestRegexpMatchesBytesLeftmostLongest(t *testing.T) {
	re, err := Compile(`(a|ab)(c|bcd)(d*)`, Extended)
	if err != nil {
		t.Fatal(err)
	}
	if got := re.FindSubmatchIndex([]byte("xabcd")); !slices.Equal(got, []int{1, 5, 1, 2, 2, 5, 5, 5}) {
		t.Errorf("FindSubmatchIndex = %v", got)
	}
	if re.NumSubexp() != 3 || re.String() != `(a|ab)(c|bcd)(d*)` {
		t.Errorf("NumSubexp = %d, String = %q", re.NumSubexp(), re.String())
	}
	bytewise, err := Compile("\xc3\xa9+|(x)", Extended)
	if err != nil {
		t.Fatal(err)
	}
	data := []byte("\xd0\x9f \xc3\xa9\xa9\xa9!")
	if got := bytewise.FindIndex(data); !slices.Equal(got, []int{3, 7}) {
		t.Errorf("FindIndex = %v", got)
	}
	if got := bytewise.FindSubmatchIndex(data); !slices.Equal(got, []int{3, 7, -1, -1}) {
		t.Errorf("FindSubmatchIndex = %v", got)
	}
	if bytewise.FindIndex([]byte("none")) != nil || !bytewise.Match(data) || bytewise.Match([]byte("\xc3")) {
		t.Error("byte-wise matching disagrees")
	}
	lines, err := Compile(`^[^ ]\+$`, Newline)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		text string
		from int
		want []int
	}{
		{text: "a b\nword\nx y\n", from: 0, want: []int{4, 8}},
		{text: "a b\nword\nx y\nlast", from: 9, want: []int{13, 17}},
		{text: "\xc3\xa9 b\n\xc3\xa9\xa9\nx y\n", from: 5, want: []int{5, 8}},
		{text: "\xc3\xa9 b\n\xc3\xa9\xa9\nx y\n", from: 9, want: nil},
	} {
		subject := NewSubject([]byte(c.text))
		if got := subject.FindIndex(lines, c.from); !slices.Equal(got, c.want) {
			t.Errorf("FindIndex(%q, %d) = %v, want %v", c.text, c.from, got, c.want)
		}
		if got := subject.FindSubmatchIndex(lines, c.from); !slices.Equal(got, c.want) {
			t.Errorf("FindSubmatchIndex(%q, %d) = %v, want %v", c.text, c.from, got, c.want)
		}
	}
	dot, err := Compile(`^.$`, Extended)
	if err != nil {
		t.Fatal(err)
	}
	if dot.Match([]byte("\xc3\xa9")) || !dot.Match([]byte("\xc3")) || dot.Match([]byte{0}) {
		t.Error("a period must match exactly one byte other than NUL")
	}
}
