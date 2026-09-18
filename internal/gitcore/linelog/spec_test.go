package linelog

import (
	"errors"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/posixre"
	"github.com/oops1/gogit/internal/gitcore/userdiff"
)

func TestParseArgSplitsTheRangeFromThePath(t *testing.T) {
	cases := []struct {
		arg  string
		want Spec
	}{
		{arg: "1,5:f", want: Spec{Range: "1,5", Path: "f"}},
		{arg: "7:dir/f:g", want: Spec{Range: "7", Path: "dir/f:g"}},
		{arg: ",+3:f", want: Spec{Range: ",+3", Path: "f"}},
		{arg: "/a:b/,/c\\/d/:f", want: Spec{Range: "/a:b/,/c\\/d/", Path: "f"}},
		{arg: "^/x/:f", want: Spec{Range: "^/x/", Path: "f"}},
		{arg: ":func\\:name:f", want: Spec{Range: ":func\\:name", Path: "f"}},
		{arg: "^:main:cmd.go", want: Spec{Range: "^:main", Path: "cmd.go"}},
	}
	for _, c := range cases {
		got, err := ParseArg(c.arg)
		if err != nil || got != c.want {
			t.Errorf("ParseArg(%q) = %+v, %v; want %+v", c.arg, got, err, c.want)
		}
		if err == nil && got.String() != c.arg {
			t.Errorf("Spec.String() = %q, want %q", got.String(), c.arg)
		}
	}
}

func TestParseArgRejectsMalformedArguments(t *testing.T) {
	for _, arg := range []string{"1,5", "1,5:", "abc:f", "::f", ":f", "/unterminated:f", "1,x:f"} {
		if _, err := ParseArg(arg); !errors.Is(err, ErrMalformedArg) {
			t.Errorf("ParseArg(%q) returned %v", arg, err)
		}
	}
}

func TestLinesSpecWritesAnInclusiveRange(t *testing.T) {
	if got := LinesSpec("a/b.go", 3, 9); got != (Spec{Range: "3,9", Path: "a/b.go"}) {
		t.Fatalf("LinesSpec = %+v", got)
	}
}

func TestNewTextIndexesLineStarts(t *testing.T) {
	cases := []struct {
		data  string
		lines int
		last  string
	}{
		{data: "", lines: 0},
		{data: "a\nb\n", lines: 2, last: "b\n"},
		{data: "a\nb", lines: 2, last: "b"},
		{data: "\n", lines: 1, last: "\n"},
	}
	for _, c := range cases {
		text := newText([]byte(c.data))
		if text.lines() != c.lines {
			t.Errorf("%q has %d lines, want %d", c.data, text.lines(), c.lines)
			continue
		}
		if c.lines > 0 && string(text.line(c.lines-1)) != c.last {
			t.Errorf("%q last line = %q", c.data, text.line(c.lines-1))
		}
	}
}

func TestStrtolParsesLikeC(t *testing.T) {
	cases := []struct {
		in    string
		value int
		used  int
	}{
		{in: "12,", value: 12, used: 2},
		{in: " \t-7x", value: -7, used: 4},
		{in: "+3", value: 3, used: 2},
		{in: "-", value: 0, used: 0},
		{in: "x", value: 0, used: 0},
		{in: "99999999999999999999", value: 2147483647, used: 20},
	}
	for _, c := range cases {
		value, used := strtol(c.in)
		if value != c.value || used != c.used {
			t.Errorf("strtol(%q) = %d, %d; want %d, %d", c.in, value, used, c.value, c.used)
		}
	}
}

const sample = "int alpha(void)\n{\n\treturn 1;\n}\n\nint beta(void)\n{\n\treturn 2;\n}\nzeta line\n"

func TestResolveRangeUnderstandsGitRangeForms(t *testing.T) {
	cases := []struct {
		name   string
		spec   string
		anchor int
		begin  int
		end    int
	}{
		{name: "numbers", spec: "2,4", begin: 2, end: 4},
		{name: "swapped numbers", spec: "4,2", begin: 2, end: 4},
		{name: "start only", spec: "3", begin: 3},
		{name: "end only", spec: ",4", end: 4},
		{name: "nothing", spec: ""},
		{name: "plus offset", spec: "2,+3", begin: 2, end: 4},
		{name: "minus offset", spec: "6,-3", begin: 4, end: 6},
		{name: "minus offset past the top", spec: "2,-9", begin: 1, end: 2},
		{name: "plus sign without digits", spec: "2,+", begin: 0, end: 0},
		{name: "regex start", spec: "/beta/", begin: 6},
		{name: "regex from anchor", spec: "/return/", anchor: 5, begin: 8},
		{name: "regex from top", spec: "^/return/", anchor: 5, begin: 3},
		{name: "regex end", spec: "6,/}/", begin: 6, end: 9},
		{name: "regex end past the start line", spec: "1,/int/", begin: 1, end: 6},
		{name: "regex at the very end", spec: `/\'/`, begin: 11},
		{name: "anchor on the last line", spec: "/zeta/", anchor: 10, begin: 10},
		{name: "funcname", spec: ":beta", begin: 6, end: 9},
		{name: "funcname stops at the next function", spec: ":alpha", begin: 1, end: 5},
		{name: "funcname from the top", spec: "^:alpha", anchor: 7, begin: 1, end: 5},
		{name: "funcname skips non function lines", spec: ":return", begin: 0, end: 0},
	}
	text := newText([]byte(sample))
	for _, c := range cases {
		begin, end, err := resolveRange(c.spec, text, c.anchor)
		if c.name == "plus sign without digits" || c.name == "funcname skips non function lines" {
			if err == nil {
				t.Errorf("%s: resolveRange(%q) = %d, %d; want an error", c.name, c.spec, begin, end)
			}
			continue
		}
		if err != nil || begin != c.begin || end != c.end {
			t.Errorf("%s: resolveRange(%q) = %d, %d, %v; want %d, %d", c.name, c.spec, begin, end, err, c.begin, c.end)
		}
	}
}

func TestResolveRangeReportsErrors(t *testing.T) {
	cases := []struct {
		spec string
		data string
		want error
	}{
		{spec: "0,3", data: sample, want: ErrInvalidLine},
		{spec: "2,0", data: sample, want: ErrInvalidLine},
		{spec: "2,+0", data: sample, want: ErrEmptyRange},
		{spec: "2x", data: sample, want: ErrMalformedRange},
		{spec: "2,/nothing/", data: sample, want: ErrNoMatch},
		{spec: "/[/", data: sample, want: ErrPattern},
		{spec: "2,/[/", data: sample, want: ErrPattern},
		{spec: ":nothing", data: sample, want: ErrNoMatch},
		{spec: ":[", data: sample, want: ErrPattern},
		{spec: ":", data: sample, want: ErrMalformedRange},
		{spec: ":beta:x", data: sample, want: ErrMalformedRange},
		{spec: ":y", data: "x\n", want: ErrNoMatch},
		{spec: ":zeta", data: "a\x00zeta\n", want: ErrNoMatch},
		{spec: "/zeta/", data: "a\x00zeta\n", want: ErrNoMatch},
	}
	for _, c := range cases {
		if _, _, err := resolveRange(c.spec, newText([]byte(c.data)), 1); !errors.Is(err, c.want) {
			t.Errorf("resolveRange(%q) returned %v, want %v", c.spec, err, c.want)
		}
	}
	if _, _, err := resolveRange("/zeta/", newText([]byte(sample)), 40); !errors.Is(err, ErrNoMatch) {
		t.Errorf("an anchor past the end searched from %v", err)
	}
}

func TestFindFuncnameWalksMatchesLineByLine(t *testing.T) {
	re, err := compilePattern(`a`)
	if err != nil {
		t.Fatal(err)
	}
	data := []byte("\ta first\n\n  a second\nalpha\n")
	if got := findFuncname(data, 0, re, nil); got != 21 {
		t.Fatalf("findFuncname = %d", got)
	}
	empty, err := compilePattern(`^`)
	if err != nil {
		t.Fatal(err)
	}
	if got := findFuncname([]byte("\n\nname"), 0, empty, nil); got != 2 {
		t.Fatalf("an empty match found %d", got)
	}
	python, err := userdiff.Compile(userdiff.Pattern{Source: "^[ \t]*def ", Flags: posixre.Extended})
	if err != nil {
		t.Fatal(err)
	}
	if got := findFuncname([]byte("a = 1\n    def alpha():\n"), 0, re, python); got != 6 {
		t.Fatalf("a driver match found %d", got)
	}
	indented, err := compilePattern(`x`)
	if err != nil {
		t.Fatal(err)
	}
	if got := findFuncname([]byte(" x"), 0, indented, nil); got != -1 {
		t.Fatalf("an indented match found %d", got)
	}
	if got := findFuncname([]byte(" x"), 0, re, nil); got != -1 {
		t.Fatalf("a missing match found %d", got)
	}
}
