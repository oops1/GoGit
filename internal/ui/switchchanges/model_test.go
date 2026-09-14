package switchchanges

import (
	"testing"

	"github.com/oops1/gogit/internal/config"
)

func TestOnlyARememberedChoiceActsAutomatically(t *testing.T) {
	for _, mode := range []string{config.SwitchChangesStash, config.SwitchChangesMerge, config.SwitchChangesOverwrite} {
		if got, ok := Automatic(mode); !ok || got != mode {
			t.Fatalf("Automatic(%q) = %q, %v", mode, got, ok)
		}
	}
	for _, mode := range []string{config.SwitchChangesAsk, "", "bogus"} {
		if _, ok := Automatic(mode); ok {
			t.Fatalf("Automatic(%q) acted without asking", mode)
		}
	}
}

func TestListPathsShortensALongList(t *testing.T) {
	if got := ListPaths([]string{"a", "b"}); got != "a, b" {
		t.Fatalf("ListPaths = %q", got)
	}
	long := []string{"1", "2", "3", "4", "5", "6", "7", "8", "9"}
	if got := ListPaths(long); got != "1, 2, 3, 4, 5, 6, 7, 8, …" {
		t.Fatalf("ListPaths = %q", got)
	}
}
