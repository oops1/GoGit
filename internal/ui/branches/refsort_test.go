package branches

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/oops1/headless-gui/v3/widget"
	"github.com/oops1/headless-gui/v3/widget/treeview"

	"github.com/oops1/gogit/internal/gitcore/refs"
)

func outline(items []*treeview.TreeViewItem) []string {
	var out []string
	var walk func(items []*treeview.TreeViewItem, depth int)
	walk = func(items []*treeview.TreeViewItem, depth int) {
		for _, item := range items {
			out = append(out, strings.Repeat("  ", depth)+item.DisplayText())
			walk(item.Children, depth+1)
		}
	}
	walk(items, 0)
	return out
}

func rootByKey(t *testing.T, v *View, tw *treeview.TreeView, key string) *treeview.TreeViewItem {
	t.Helper()
	for _, root := range tw.Roots() {
		if v.keyByItem[root] == key {
			return root
		}
	}
	t.Fatalf("no root with key %q among %v", key, outline(tw.Roots()))
	return nil
}

func sectionOutline(t *testing.T, v *View, tw *treeview.TreeView, key string) []string {
	t.Helper()
	return outline([]*treeview.TreeViewItem{rootByKey(t, v, tw, key)})
}

func localOutline(t *testing.T, v *View, tw *treeview.TreeView) []string {
	t.Helper()
	return outline(rootByKey(t, v, tw, localGroupKey).Children)
}

func at(minute int) time.Time {
	return time.Date(2026, time.March, 1, 12, minute, 0, 0, time.UTC)
}

func localFixture(t *testing.T) Snapshot {
	t.Helper()
	return Snapshot{
		Local: []Branch{
			{Name: refs.BranchName("main"), Target: oid(t, "11"), When: at(3)},
			{Name: refs.BranchName("feature/a"), Target: oid(t, "22"), When: at(5)},
			{Name: refs.BranchName("feature/b"), Target: oid(t, "33"), When: at(1)},
			{Name: refs.BranchName("release/1.9"), Target: oid(t, "44"), When: at(2)},
			{Name: refs.BranchName("release/1.10"), Target: oid(t, "55"), When: at(4)},
			{Name: refs.BranchName("solo/only"), Target: oid(t, "66"), When: at(6)},
		},
	}
}

func TestLocalBranchesFollowTheChosenSortAndGrouping(t *testing.T) {
	for _, tc := range []struct {
		name    string
		options Options
		want    []string
	}{
		{
			name:    "by name, nested by path",
			options: DefaultOptions(),
			want:    []string{"feature", "  a", "  b", "main", "release", "  1.9", "  1.10", "solo", "  only"},
		},
		{
			name:    "by name with numbers in reverse order",
			options: Options{Sort: SortByNameReverseNumbers, Grouping: Grouping{ByPath: true}},
			want:    []string{"feature", "  a", "  b", "main", "release", "  1.10", "  1.9", "solo", "  only"},
		},
		{
			name:    "by commit time, newest first",
			options: Options{Sort: SortByCommitTime, Grouping: Grouping{ByPath: true}},
			want:    []string{"solo", "  only", "feature", "  a", "  b", "release", "  1.10", "  1.9", "main"},
		},
		{
			name:    "without grouping every branch keeps its path",
			options: Options{},
			want:    []string{"feature/a", "feature/b", "main", "release/1.9", "release/1.10", "solo/only"},
		},
		{
			name:    "a single branch keeps its path when singletons are excluded",
			options: Options{Grouping: Grouping{ByPath: true, ExceptSingles: true}},
			want:    []string{"feature", "  a", "  b", "main", "release", "  1.9", "  1.10", "solo/only"},
		},
		{
			name:    "groups first pushes plain branches down",
			options: Options{Grouping: Grouping{ByPath: true, GroupsFirst: true}},
			want:    []string{"feature", "  a", "  b", "release", "  1.9", "  1.10", "solo", "  only", "main"},
		},
		{
			name:    "one level deep stays the same after the last slash",
			options: Options{Grouping: Grouping{ByPath: true, AfterLastSlash: true}},
			want:    []string{"feature", "  a", "  b", "main", "release", "  1.9", "  1.10", "solo", "  only"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v, tw := bound(t)
			v.SetOptions(tc.options)
			v.Render(localFixture(t))
			if got := localOutline(t, v, tw.Tree); !slices.Equal(got, tc.want) {
				t.Fatalf("local outline = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestGroupingSplitsOnlyAtTheLastSlashWhenAsked(t *testing.T) {
	snap := Snapshot{
		Local: []Branch{
			{Name: refs.BranchName("a/b/c"), Target: oid(t, "11")},
			{Name: refs.BranchName("a/b/d"), Target: oid(t, "22")},
			{Name: refs.BranchName("a/e"), Target: oid(t, "33")},
		},
	}
	for _, tc := range []struct {
		name    string
		options Options
		want    []string
	}{
		{
			name:    "every segment makes a level",
			options: DefaultOptions(),
			want:    []string{"a", "  b", "    c", "    d", "  e"},
		},
		{
			name:    "only the last slash makes a level",
			options: Options{Grouping: Grouping{ByPath: true, AfterLastSlash: true}},
			want:    []string{"a/b", "  c", "  d", "a", "  e"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v, tw := bound(t)
			v.SetOptions(tc.options)
			v.Render(snap)
			if got := localOutline(t, v, tw.Tree); !slices.Equal(got, tc.want) {
				t.Fatalf("local outline = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSetOptionsBeforeBindKeepsThemForTheFirstRender(t *testing.T) {
	v := NewView()
	v.SetOptions(Options{Sort: SortByCommitTime})
	if v.Options().Sort != SortByCommitTime {
		t.Fatalf("sort = %v, want by commit time", v.Options().Sort)
	}
	tw := widget.NewTreeViewWidget()
	v.Bind(tw)
	v.Render(localFixture(t))
	want := []string{"solo/only", "feature/a", "release/1.10", "main", "release/1.9", "feature/b"}
	if got := localOutline(t, v, tw.Tree); !slices.Equal(got, want) {
		t.Fatalf("local outline = %v, want %v", got, want)
	}
}

func TestSetOptionsRerendersTheTreeAfterBind(t *testing.T) {
	v, tw := bound(t)
	v.Render(localFixture(t))
	v.SetOptions(Options{})
	want := []string{"feature/a", "feature/b", "main", "release/1.9", "release/1.10", "solo/only"}
	if got := localOutline(t, v, tw.Tree); !slices.Equal(got, want) {
		t.Fatalf("local outline = %v, want %v", got, want)
	}
}

func TestRemoteBranchesFollowTheChosenSort(t *testing.T) {
	snap := Snapshot{
		Remotes: []Remote{{
			Name: "origin",
			Branches: []Branch{
				{Name: refs.RemoteBranchName("origin", "v1.9"), Target: oid(t, "11")},
				{Name: refs.RemoteBranchName("origin", "v1.10"), Target: oid(t, "22")},
			},
		}},
	}
	v, tw := bound(t)
	v.SetOptions(Options{Sort: SortByNameReverseNumbers})
	v.Render(snap)
	_, remotes, _ := roots(t, tw)
	want := []string{"origin", "  v1.10", "  v1.9"}
	if got := outline(remotes.Children); !slices.Equal(got, want) {
		t.Fatalf("remotes outline = %v, want %v", got, want)
	}
}

func TestNestedTagsFollowTheChosenSort(t *testing.T) {
	snap := Snapshot{
		Tags: []Tag{
			{Name: refs.TagName("rel/1.9"), Target: oid(t, "11")},
			{Name: refs.TagName("rel/1.10"), Target: oid(t, "22")},
		},
	}
	v, tw := bound(t)
	v.SetOptions(Options{Sort: SortByNameReverseNumbers, Grouping: Grouping{ByPath: true}})
	v.Render(snap)
	_, _, tags := roots(t, tw)
	want := []string{"rel", "  1.10", "  1.9"}
	if got := outline(tags.Children); !slices.Equal(got, want) {
		t.Fatalf("tags outline = %v, want %v", got, want)
	}
}

func TestCompareNaturalOrdersDigitRunsAndTails(t *testing.T) {
	for _, tc := range []struct {
		a, b       string
		descending bool
		want       int
	}{
		{a: "v2", b: "v10", want: -1},
		{a: "v2", b: "v10", descending: true, want: 1},
		{a: "v2", b: "v2", want: 0},
		{a: "v2", b: "v2.1", want: -1},
		{a: "a1", b: "b1", want: -1},
		{a: "1a", b: "1a", descending: true, want: 0},
	} {
		if got := compareNatural(tc.a, tc.b, tc.descending); got != tc.want {
			t.Fatalf("compareNatural(%q, %q, %v) = %d, want %d", tc.a, tc.b, tc.descending, got, tc.want)
		}
	}
}

func TestLeafLabelFallsBackToTheWholePath(t *testing.T) {
	if got := leafLabel(segmentedEntry{entry: pathEntry{path: "main"}}); got != "main" {
		t.Fatalf("leafLabel without segments = %q, want %q", got, "main")
	}
}
