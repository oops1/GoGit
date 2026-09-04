package diff

import (
	"strings"
	"testing"
)

func spanString(spans []Span) string {
	var out strings.Builder
	for _, span := range spans {
		out.WriteString(span.Kind.prefix())
		out.WriteString(span.Text)
		out.WriteString("|")
	}
	return out.String()
}

func TestInlineDiffMarksTheChangedTokens(t *testing.T) {
	cases := []struct {
		name string
		old  string
		new  string
		want string
	}{
		{"equal lines", "alpha beta", "alpha beta", " alpha beta|"},
		{"both empty", "", "", ""},
		{"insertion only", "", "added", "+added|"},
		{"deletion only", "gone", "", "-gone|"},
		{"one word replaced", "alpha beta", "alpha gamma", " alpha |-beta|+gamma|"},
		{"word appended", "call(a)", "call(a, b)", " call(a|+, b| )|"},
		{"punctuation inserted", "a = b;", "a := b;", " a |+:| = b;|"},
		{"leading token replaced", "old tail", "new tail", "-old|+new|  tail|"},
		{"whitespace grows", "a b", "a  b", " a|- |+  | b|"},
		{"unicode words", "привет мир", "привет всем", " привет |-мир|+всем|"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := spanString(InlineDiff(c.old, c.new)); got != c.want {
				t.Errorf("InlineDiff(%q, %q) produced %q instead of %q", c.old, c.new, got, c.want)
			}
		})
	}
}

func TestTokenizeSplitsByCharacterClass(t *testing.T) {
	got := tokenize("go_1 += x\t;")
	want := []string{"go_1", " ", "+", "=", " ", "x", "\t", ";"}
	if len(got) != len(want) {
		t.Fatalf("tokenize produced %q instead of %q", got, want)
	}
	for at := range want {
		if got[at] != want[at] {
			t.Errorf("token %d is %q instead of %q", at, got[at], want[at])
		}
	}
}

func TestClassOfSortsRunesIntoWordSpaceAndOther(t *testing.T) {
	cases := []struct {
		value rune
		want  tokenClass
	}{
		{'a', classWord},
		{'Z', classWord},
		{'7', classWord},
		{'_', classWord},
		{'я', classWord},
		{' ', classSpace},
		{'\t', classSpace},
		{'+', classOther},
		{'"', classOther},
	}
	for _, c := range cases {
		if got := classOf(c.value); got != c.want {
			t.Errorf("classOf(%q) returned %d instead of %d", c.value, got, c.want)
		}
	}
}
