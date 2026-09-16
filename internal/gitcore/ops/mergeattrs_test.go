package ops

import (
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/merge"
)

func TestMergeDriversFollowTheAttributesAndTheConfig(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile(".gitattributes", "set merge\nunset -merge\nunion merge=union\nbin merge=binary\ncustom merge=custom\nlayered merge=layered\nnamed merge=unknown\nwide conflict-marker-size=11\n")
	r.appendConfig("[merge \"custom\"]\n\tdriver = cat %A\n[merge \"layered\"]\n\tdriver = cat %A\n\trecursive = union\n[merge]\n\tdefault = union\n")
	m, err := openMerger(t.Context(), r.reopen(), MergeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer m.close()

	for _, c := range []struct {
		path    string
		virtual bool
		want    merge.PathAttributes
	}{
		{"set", false, merge.PathAttributes{Driver: merge.DriverText, MarkerSize: 7}},
		{"unset", false, merge.PathAttributes{Driver: merge.DriverBinary, MarkerSize: 7}},
		{"union", false, merge.PathAttributes{Driver: merge.DriverUnion, Name: "union", MarkerSize: 7}},
		{"bin", false, merge.PathAttributes{Driver: merge.DriverBinary, Name: "binary", MarkerSize: 7}},
		{"custom", false, merge.PathAttributes{Driver: merge.DriverExternal, Name: "custom", MarkerSize: 7}},
		{"custom", true, merge.PathAttributes{Driver: merge.DriverExternal, Name: "custom", MarkerSize: 7}},
		{"layered", false, merge.PathAttributes{Driver: merge.DriverExternal, Name: "layered", MarkerSize: 7}},
		{"layered", true, merge.PathAttributes{Driver: merge.DriverUnion, Name: "union", MarkerSize: 7}},
		{"named", false, merge.PathAttributes{Driver: merge.DriverText, Name: "unknown", MarkerSize: 7}},
		{"wide", false, merge.PathAttributes{Driver: merge.DriverUnion, Name: "union", MarkerSize: 11}},
	} {
		if got := m.mergeAttributes(c.path, c.virtual); got != c.want {
			t.Errorf("%s (virtual %v) = %+v, want %+v", c.path, c.virtual, got, c.want)
		}
	}
}

func TestAPathWithoutAnyMergeSettingUsesTheTextDriver(t *testing.T) {
	r := newTestRepo(t)
	m, err := openMerger(t.Context(), r.reopen(), MergeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer m.close()

	if got := m.mergeAttributes("f", false); got != (merge.PathAttributes{MarkerSize: merge.DefaultMarkerSize}) {
		t.Fatalf("attributes = %+v", got)
	}
}

func TestTheBaseLabelNamesTheMergeBases(t *testing.T) {
	one, two := hash.SumSHA1("commit", []byte("one")), hash.SumSHA1("commit", []byte("two"))

	for bases, want := range map[int]string{0: emptyTreeLabel, 1: abbreviate(one), 2: mergedAncestorsLabel} {
		if got := baseLabel([]hash.ObjectID{one, two}[:bases]); got != want {
			t.Errorf("%d bases: label %q, want %q", bases, got, want)
		}
	}
}
