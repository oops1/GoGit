package merge

import (
	"maps"
	"slices"
	"testing"
)

func TestDirectoriesOfCollectsEveryParentOnce(t *testing.T) {
	tree := Snapshot{"d/x": {}, "d/y": {}, "d/e/z": {}, "top": {}}

	got := slices.Sorted(maps.Keys(directoriesOf(tree)))

	if !slices.Equal(got, []string{"d", "d/e"}) {
		t.Fatalf("directoriesOf = %v", got)
	}
}

func TestAsidePathAvoidsSlashesAndTakenNames(t *testing.T) {
	tree := Snapshot{"docs~origin_main": {}, "docs~origin_main_0": {}}

	taken := func(candidate string) bool { _, ok := tree[candidate]; return ok }
	if got := asidePath(taken, "docs", "origin/main"); got != "docs~origin_main_1" {
		t.Fatalf("asidePath = %q", got)
	}
	if got := asidePath(func(string) bool { return false }, "docs", "HEAD"); got != "docs~HEAD" {
		t.Fatalf("asidePath = %q", got)
	}
}
