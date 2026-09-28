package diff

import (
	"slices"
	"strings"
	"testing"
)

func textLines(text Text) []string {
	var out []string
	for at := range text.Count() {
		out = append(out, string(text.at(at)))
	}
	return out
}

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
			if got := textLines(NewText([]byte(c.in))); !slices.Equal(got, c.want) {
				t.Errorf("splitLines(%q) returned %q instead of %q", c.in, got, c.want)
			}
		})
	}
}

func TestEmptyTextIsNotReadyAndHasNoLines(t *testing.T) {
	var text Text
	if text.Ready() {
		t.Error("the zero Text reports itself as ready")
	}
	if text.Count() != 0 {
		t.Errorf("the zero Text counts %d lines", text.Count())
	}
	if NewText([]byte("a\n")).Ready() != true {
		t.Error("a split Text does not report itself as ready")
	}
}

func TestSliceTakesTheLinesOfTheRange(t *testing.T) {
	text := NewText([]byte("a\nb\nc\nd\n"))
	if got := textLines(text.slice(1, 2)); !slices.Equal(got, []string{"b\n", "c\n"}) {
		t.Errorf("slice(1, 2) returned %q", got)
	}
	if got := text.slice(2, 0); got.Count() != 0 {
		t.Errorf("slice of no lines returned %q", textLines(got))
	}
}

func TestSpanJoinsTheLinesWithoutCopying(t *testing.T) {
	text := NewText([]byte("a\nb\nc\n"))
	if got := string(text.span(0, 2)); got != "a\nb\n" {
		t.Errorf("span(0, 2) returned %q", got)
	}
	if got := text.span(1, 1); got != nil {
		t.Errorf("span of no lines returned %q", got)
	}
}

func TestLineTextStripsOnlyTheTrailingNewline(t *testing.T) {
	text, newline := lineTextOf("a\n")
	if text != "a" || !newline {
		t.Errorf("lineTextOf(\"a\\n\") returned (%q, %v)", text, newline)
	}
	text, newline = lineTextOf("a")
	if text != "a" || newline {
		t.Errorf("lineTextOf(\"a\") returned (%q, %v)", text, newline)
	}
	text, newline = lineTextOf("")
	if text != "" || newline {
		t.Errorf("lineTextOf(\"\") returned (%q, %v)", text, newline)
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
		{"a carriage return before the newline is dropped", "a \r\n", IgnoreCRAtEOL, "a "},
		{"a complete line loses only its newline", "a\r\r\n", IgnoreCRAtEOL, "a\r"},
		{"an incomplete line keeps its carriage return", "a\r", IgnoreCRAtEOL, "a\r"},
		{"space at the end wins over the carriage return flag", "a \r\n", IgnoreSpaceAtEOL | IgnoreCRAtEOL, "a"},
	}
	var scratch []byte
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var key []byte
			key, scratch = lineKey([]byte(c.record), scratch, c.option)
			if got := string(key); got != c.want {
				t.Errorf("lineKey(%q, %d) returned %q instead of %q", c.record, c.option, got, c.want)
			}
		})
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
			if got := isBlankLine([]byte(c.record), c.option); got != c.want {
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
			if got := lineIndent([]byte(c.record)); got != c.want {
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

func TestATextPoolReusesTheLineOffsetsItGetsBack(t *testing.T) {
	var pool TextPool
	first := pool.Text([]byte("a\nb\nc\n"))
	if got := textLines(first); !slices.Equal(got, []string{"a\n", "b\n", "c\n"}) {
		t.Fatalf("the pool split the first text into %q", got)
	}
	pool.Release(Text{})
	if len(pool.free) != 0 {
		t.Fatal("the pool kept a text that has no line offsets")
	}
	pool.Release(first)
	second := pool.Text([]byte("x\ny\n"))
	if got := textLines(second); !slices.Equal(got, []string{"x\n", "y\n"}) {
		t.Fatalf("the pool split the second text into %q", got)
	}
	if &second.offs[0] != &first.offs[0] {
		t.Fatal("the pool made new line offsets instead of reusing the ones it got back")
	}
	if len(pool.free) != 0 {
		t.Fatalf("the pool still holds %d buffers while one is in use", len(pool.free))
	}
	if got := pool.Text(nil); got.Ready() {
		t.Fatal("the pool split an empty file into lines")
	}
}

func TestATextPoolStopsCollectingBuffersAtItsLimit(t *testing.T) {
	var pool TextPool
	for range maxPooledTexts + 3 {
		pool.Release(NewText([]byte("a\nb\n")))
	}
	if len(pool.free) != maxPooledTexts {
		t.Fatalf("the pool holds %d buffers instead of %d", len(pool.free), maxPooledTexts)
	}
}
