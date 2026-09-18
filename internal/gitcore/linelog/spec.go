package linelog

import (
	"bytes"
	"fmt"
	"math"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/posixre"
	"github.com/oops1/gogit/internal/gitcore/userdiff"
)

type Spec struct {
	Range string
	Path  string
}

func (s Spec) String() string { return s.Range + ":" + s.Path }

func LinesSpec(path string, first, last int) Spec {
	return Spec{Range: fmt.Sprintf("%d,%d", first, last), Path: path}
}

func ParseArg(arg string) (Spec, error) {
	rest, ok := skipRange(arg)
	if !ok || !strings.HasPrefix(rest, ":") || len(rest) == 1 {
		return Spec{}, fmt.Errorf("%w: %s", ErrMalformedArg, arg)
	}
	return Spec{Range: arg[:len(arg)-len(rest)], Path: rest[1:]}, nil
}

func skipRange(arg string) (string, bool) {
	if isFuncnameRange(arg) {
		rest, _, _, err := parseFuncname(arg, nil, 0)
		return rest, err == nil
	}
	rest, _, _ := parseLoc(arg, nil, -1)
	if after, found := strings.CutPrefix(rest, ","); found {
		rest, _, _ = parseLoc(after, nil, 0)
	}
	return rest, true
}

func isFuncnameRange(arg string) bool {
	return strings.HasPrefix(arg, ":") || strings.HasPrefix(arg, "^:")
}

type text struct {
	data   []byte
	starts []int
	funcs  *userdiff.Matcher
}

func newText(data []byte) *text {
	starts := []int{0}
	for at, c := range data {
		if c == '\n' || at == len(data)-1 {
			starts = append(starts, at+1)
		}
	}
	return &text{data: data, starts: starts}
}

func (t *text) lines() int { return len(t.starts) - 1 }

func (t *text) start(line int) int { return t.starts[line] }

func (t *text) line(at int) []byte { return t.data[t.starts[at]:t.starts[at+1]] }

func resolveRange(rangeText string, t *text, anchor int) (int, int, error) {
	lines := t.lines()
	anchor = min(max(anchor, 1), lines+1)
	if isFuncnameRange(rangeText) {
		rest, begin, end, err := parseFuncname(rangeText, t, anchor)
		if err != nil {
			return 0, 0, err
		}
		if rest != "" {
			return 0, 0, fmt.Errorf("%w: %s", ErrMalformedRange, rangeText)
		}
		return begin, end, nil
	}
	rest, begin, err := parseLoc(rangeText, t, -anchor)
	if err != nil {
		return 0, 0, err
	}
	end := 0
	if after, found := strings.CutPrefix(rest, ","); found {
		if rest, end, err = parseLoc(after, t, begin+1); err != nil {
			return 0, 0, err
		}
	}
	if rest != "" {
		return 0, 0, fmt.Errorf("%w: %s", ErrMalformedRange, rangeText)
	}
	if begin != 0 && end != 0 && end < begin {
		begin, end = end, begin
	}
	return begin, end, nil
}

func strtol(s string) (int, int) {
	at := 0
	for at < len(s) && strings.IndexByte(" \t\n\v\f\r", s[at]) >= 0 {
		at++
	}
	negative := false
	if at < len(s) && (s[at] == '+' || s[at] == '-') {
		negative = s[at] == '-'
		at++
	}
	digits := at
	value := 0
	for at < len(s) && '0' <= s[at] && s[at] <= '9' {
		value = min(value*10+int(s[at]-'0'), math.MaxInt32)
		at++
	}
	if at == digits {
		return 0, 0
	}
	if negative {
		value = -value
	}
	return value, at
}

func parseLoc(spec string, t *text, begin int) (string, int, error) {
	if begin >= 1 && (strings.HasPrefix(spec, "+") || strings.HasPrefix(spec, "-")) {
		return parseOffset(spec, t, begin)
	}
	if num, used := strtol(spec); used > 0 {
		if t != nil && num <= 0 {
			return "", 0, fmt.Errorf("%w: %d", ErrInvalidLine, num)
		}
		return spec[used:], num, nil
	}
	if begin < 0 {
		begin = -begin
		if rest, found := strings.CutPrefix(spec, "^"); found {
			begin, spec = 1, rest
		}
	}
	if !strings.HasPrefix(spec, "/") {
		return spec, 0, nil
	}
	term := 1
	for term < len(spec) && spec[term] != '/' {
		if spec[term] == '\\' {
			term++
		}
		term++
	}
	if term >= len(spec) {
		return spec, 0, nil
	}
	if t == nil {
		return spec[term+1:], 0, nil
	}
	line, err := matchLine(spec[1:term], t, begin-1)
	return spec[term+1:], line, err
}

func parseOffset(spec string, t *text, begin int) (string, int, error) {
	num, used := strtol(spec[1:])
	if used == 0 {
		return spec, 0, nil
	}
	rest := spec[1+used:]
	if num == 0 {
		return "", 0, ErrEmptyRange
	}
	if spec[0] == '-' {
		num = -num
	}
	if num > 0 {
		return rest, begin + num - 2, nil
	}
	return rest, max(begin+num, 1), nil
}

func matchLine(pattern string, t *text, from int) (int, error) {
	re, err := compilePattern(pattern)
	if err != nil {
		return 0, err
	}
	lines := t.lines()
	from = min(from, lines)
	found := searchable(t.data[t.start(from):], re)
	if found < 0 {
		return 0, fmt.Errorf("%w: %s starting at line %d", ErrNoMatch, pattern, from+1)
	}
	cp := t.start(from) + found
	line, at := t.start(from), from
	for at < lines {
		at++
		next := t.start(at)
		if line <= cp && cp < next {
			return at, nil
		}
		line = next
	}
	return at + 1, nil
}

func searchable(data []byte, re *posixre.Regexp) int {
	if cut := bytes.IndexByte(data, 0); cut >= 0 {
		data = data[:cut]
	}
	loc := re.FindIndex(data)
	if loc == nil {
		return -1
	}
	return loc[0]
}

func parseFuncname(arg string, t *text, anchor int) (string, int, int, error) {
	if rest, found := strings.CutPrefix(arg, "^"); found {
		anchor, arg = 1, rest
	}
	term := 1
	for term < len(arg) && arg[term] != ':' {
		if arg[term] == '\\' && term+1 < len(arg) {
			term++
		}
		term++
	}
	if term == 1 {
		return "", 0, 0, fmt.Errorf("%w: %s", ErrMalformedRange, arg)
	}
	if t == nil {
		return arg[term:], 0, 0, nil
	}
	pattern := arg[1:term]
	re, err := compilePattern(pattern)
	if err != nil {
		return "", 0, 0, err
	}
	data := t.data
	if cut := bytes.IndexByte(data, 0); cut >= 0 {
		data = data[:cut]
	}
	found := findFuncname(data, t.start(anchor-1), re, t.funcs)
	if found < 0 {
		return "", 0, 0, fmt.Errorf("%w: %s starting at line %d", ErrNoMatch, pattern, anchor)
	}
	begin := 0
	for found > t.start(begin) {
		begin++
	}
	lines := t.lines()
	end := begin + 1
	for end < lines && !isFuncLine(t.funcs, t.line(end)) {
		end++
	}
	return arg[term:], begin + 1, end, nil
}

func findFuncname(data []byte, start int, re *posixre.Regexp, funcs *userdiff.Matcher) int {
	subject := posixre.NewSubject(data)
	for start < len(data) {
		loc := subject.FindIndex(re, start)
		if loc == nil {
			return -1
		}
		bol, eol := loc[0], loc[1]
		for bol > start {
			bol--
			if data[bol] == '\n' {
				break
			}
		}
		if data[bol] == '\n' {
			bol++
		}
		for eol < len(data) && data[eol] != '\n' {
			eol++
		}
		if eol < len(data) {
			eol++
		}
		if isFuncLine(funcs, data[bol:eol]) {
			return bol
		}
		start = eol
	}
	return -1
}

func isFuncLine(funcs *userdiff.Matcher, line []byte) bool {
	_, ok := funcs.Match(line)
	return ok
}
