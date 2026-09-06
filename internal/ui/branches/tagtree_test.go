package branches

import (
	"fmt"
	"strings"
	"testing"
)

func manyTags(count int) []string {
	names := make([]string, 0, count)
	for i := range count {
		names = append(names, fmt.Sprintf("v3.%d.%d", 10+i/3, i%3))
	}
	return names
}

func pathsOf(nodes []TagNode, prefix string) []string {
	var out []string
	for _, n := range nodes {
		path := n.Label
		if prefix != "" {
			path = prefix + "/" + n.Label
		}
		if n.IsTag() {
			out = append(out, path)
			continue
		}
		out = append(out, pathsOf(n.Children, path)...)
	}
	return out
}

func TestFewTagsStayFlat(t *testing.T) {
	names := []string{"v1.0.0", "v1.1.0", "v2.0.0"}
	recent, nodes := GroupTags(names)
	if recent != nil {
		t.Fatalf("recent = %v, want none while the list is short", recent)
	}
	if got := pathsOf(nodes, ""); len(got) != len(names) {
		t.Fatalf("paths = %v, want a flat list of %d tags", got, len(names))
	}
}

func TestManyTagsCollapseByPrefixIgnoringDots(t *testing.T) {
	names := append(manyTags(20), "v3.16.6", "v3.16.8", "v3.20.0", "v3.21.0", "v3.22.0")
	recent, nodes := GroupTags(names)
	if len(recent) != tagRecentCount {
		t.Fatalf("recent = %v, want %d newest tags", recent, tagRecentCount)
	}
	paths := pathsOf(nodes, "")
	var found string
	for _, p := range paths {
		if strings.HasSuffix(p, "/v3.16.6") {
			found = p
		}
	}
	if found != "v3/1/6/v3.16.6" {
		t.Fatalf("path of v3.16.6 = %q, want v3/1/6/v3.16.6 (dots ignored, one character per level)", found)
	}
}

func TestNewestTagsComeOutFirstAndAreNotCollapsed(t *testing.T) {
	names := append(manyTags(20), "v3.16.6", "v3.16.8", "v3.17.0")
	recent, nodes := GroupTags(names)
	if recent[0] != "v3.17.0" {
		t.Fatalf("recent = %v, want the newest tag first", recent)
	}
	for _, p := range pathsOf(nodes, "") {
		for _, r := range recent {
			if strings.HasSuffix(p, r) {
				t.Fatalf("tag %q is both in the recent list and in the tree (%s)", r, p)
			}
		}
	}
}

func TestSingleTagInAGroupIsNotWrappedInAFolder(t *testing.T) {
	names := append(manyTags(20), "z-release")
	_, nodes := GroupTags(names)
	for _, p := range pathsOf(nodes, "") {
		if strings.HasSuffix(p, "z-release") && strings.Count(p, "/") > 1 {
			t.Fatalf("a tag standing alone got wrapped into folders: %q", p)
		}
	}
}

func TestTagsWithoutACommonPrefixGroupByTheFirstCharacter(t *testing.T) {
	names := make([]string, 0, 14)
	for i := range 7 {
		names = append(names, fmt.Sprintf("alpha%d.0", i), fmt.Sprintf("beta%d.0", i))
	}
	_, nodes := GroupTags(names)
	for _, n := range nodes {
		if n.IsTag() {
			continue
		}
		if len(n.Label) != 1 {
			t.Fatalf("top level label = %q, want a single character when tags share nothing", n.Label)
		}
	}
}

func TestNaturalOrderPutsTenAfterNine(t *testing.T) {
	if compareTagNames("v3.9.0", "v3.10.0") >= 0 {
		t.Fatal("v3.9.0 must sort before v3.10.0")
	}
}
