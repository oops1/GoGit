package wildmatch

import (
	"strings"
	"testing"
)

func TestManyStarsGiveUpQuicklyOnTextThatCannotMatch(t *testing.T) {
	pattern := strings.Repeat("*a", 20) + "*b"
	text := strings.Repeat("a", 60)
	for _, flags := range []Flags{0, Pathname, Pathname | CaseFold} {
		if Match(pattern, text, flags) {
			t.Fatalf("Match(%q, %q, %v) = true", pattern, text, flags)
		}
		if !Match(pattern, text+"b", flags) {
			t.Fatalf("Match(%q, %q, %v) = false", pattern, text+"b", flags)
		}
	}
	deep := strings.Repeat("*/", 12) + "x"
	if Match(deep, strings.Repeat("d/", 30)+"y", Pathname) {
		t.Fatal("a component pattern matched a path with the wrong leaf")
	}
	if !Match("**/"+deep, strings.Repeat("d/", 30)+"x", Pathname) {
		t.Fatal("a leading double star did not skip the extra directories")
	}
}
