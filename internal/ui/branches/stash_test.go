package branches

import (
	"testing"

	"github.com/oops1/gogit/internal/gitcore/refs"
)

func TestStashRefsRoundTripThroughTheirIndex(t *testing.T) {
	for _, index := range []int{0, 3, 12} {
		if got, ok := StashIndex(StashRef(index)); !ok || got != index {
			t.Fatalf("StashIndex(StashRef(%d)) = %d, %v", index, got, ok)
		}
	}
	for _, ref := range []refs.Name{"refs/stash", "stash@{x}", "stash@{-1}", "stash@{2", refs.BranchName("main")} {
		if _, ok := StashIndex(ref); ok {
			t.Fatalf("StashIndex(%q) accepted a non-stash", ref)
		}
	}
}
