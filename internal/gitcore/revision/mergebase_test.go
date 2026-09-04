package revision

import (
	"errors"
	"slices"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/refs"
)

func crissCrossFixture(t testing.TB) *builder {
	t.Helper()
	b := newBuilder(t)
	b.commit("root")
	b.commit("left", "root")
	b.commit("right", "root")
	b.commit("leftmerge", "left", "right")
	b.commit("rightmerge", "right", "left")
	b.branch("main", "leftmerge")
	b.branch("topic", "rightmerge")
	b.head(refs.BranchName("main"))
	return b
}

func (b *builder) ids2(t testing.TB, want ...string) []hash.ObjectID {
	t.Helper()
	out := make([]hash.ObjectID, 0, len(want))
	for _, name := range want {
		out = append(out, b.id(name))
	}
	return out
}

func TestMergeBaseFindsCommonAncestors(t *testing.T) {
	tests := []struct {
		name    string
		fixture func(testing.TB) *builder
		tips    []string
		want    []string
	}{
		{"linear ancestor", linearFixture, []string{"c", "a"}, []string{"a"}},
		{"fork", mergeFixture, []string{"main1", "side"}, []string{"a"}},
		{"merge and branch", mergeFixture, []string{"merge", "side"}, []string{"side"}},
		{"same commit", linearFixture, []string{"b", "b"}, []string{"b"}},
		{"criss cross", crissCrossFixture, []string{"leftmerge", "rightmerge"}, []string{"right", "left"}},
		{"octopus", crissCrossFixture, []string{"leftmerge", "rightmerge", "left"}, []string{"left"}},
		{"single tip", linearFixture, []string{"b"}, []string{"b"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			b := test.fixture(t)
			got, err := MergeBase(b.context(), b.ids2(t, test.tips...)...)
			if err != nil {
				t.Fatalf("MergeBase returned error %v", err)
			}
			gotNames := names(t, b, got)
			slices.Sort(gotNames)
			want := slices.Clone(test.want)
			slices.Sort(want)
			if !slices.Equal(gotNames, want) {
				t.Errorf("MergeBase returned %v, want %v", gotNames, want)
			}
		})
	}
}

func TestMergeBaseOfUnrelatedHistoriesIsEmpty(t *testing.T) {
	b := newBuilder(t)
	b.commit("one")
	b.commit("two")
	got, err := MergeBase(b.context(), b.id("one"), b.id("two"))
	if err != nil {
		t.Fatalf("MergeBase returned error %v", err)
	}
	if len(got) != 0 {
		t.Errorf("MergeBase returned %v, want nothing", names(t, b, got))
	}
	got, err = MergeBase(b.context(), b.id("one"), b.id("two"), b.id("one"))
	if err != nil {
		t.Fatalf("MergeBase of three tips returned error %v", err)
	}
	if len(got) != 0 {
		t.Errorf("MergeBase of three tips returned %v, want nothing", names(t, b, got))
	}
}

func TestMergeBaseWithoutTipsIsEmpty(t *testing.T) {
	b := linearFixture(t)
	got, err := MergeBase(b.context())
	if err != nil || got != nil {
		t.Fatalf("MergeBase returned (%v, %v), want (nil, nil)", got, err)
	}
}

func TestMergeBaseReportsMissingObjects(t *testing.T) {
	b := crissCrossFixture(t)
	b.objects.fail[b.id("root")] = errors.New("gone")
	if _, err := MergeBase(b.context(), b.id("leftmerge"), b.id("rightmerge")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("MergeBase returned %v, want %v", err, ErrNotFound)
	}
	missing := hash.SumSHA1("commit", []byte("nothing"))
	if _, err := MergeBase(b.context(), missing, b.id("left")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("MergeBase of a missing tip returned %v, want %v", err, ErrNotFound)
	}
	if _, err := MergeBase(b.context(), b.id("left"), missing); !errors.Is(err, ErrNotFound) {
		t.Fatalf("MergeBase of a missing second tip returned %v, want %v", err, ErrNotFound)
	}
}

func TestIsAncestorFollowsReachability(t *testing.T) {
	b := crissCrossFixture(t)
	tests := []struct {
		name string
		from string
		to   string
		want bool
	}{
		{"self", "left", "left", true},
		{"parent", "root", "leftmerge", true},
		{"side branch", "right", "leftmerge", true},
		{"descendant", "leftmerge", "root", false},
		{"sibling", "leftmerge", "rightmerge", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := IsAncestor(b.context(), b.id(test.from), b.id(test.to))
			if err != nil {
				t.Fatalf("IsAncestor returned error %v", err)
			}
			if got != test.want {
				t.Errorf("IsAncestor(%s, %s) = %v, want %v", test.from, test.to, got, test.want)
			}
		})
	}
}

func TestIsAncestorReportsMissingObjects(t *testing.T) {
	b := linearFixture(t)
	b.objects.fail[b.id("a")] = errors.New("gone")
	if _, err := IsAncestor(b.context(), b.id("b"), b.id("c")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("IsAncestor returned %v, want %v", err, ErrNotFound)
	}
}

func TestMergeBaseCollapsesRepeatedCandidates(t *testing.T) {
	b := crissCrossFixture(t)
	got, err := MergeBase(b.context(), b.id("leftmerge"), b.id("rightmerge"), b.id("root"))
	if err != nil {
		t.Fatalf("MergeBase returned error %v", err)
	}
	if want := []string{"root"}; !slices.Equal(names(t, b, got), want) {
		t.Errorf("MergeBase returned %v, want %v", names(t, b, got), want)
	}
}

func TestMergeBaseReportsFailuresWhileDroppingRedundantBases(t *testing.T) {
	b := newBuilder(t)
	b.commit("deep")
	b.commit("root", "deep")
	b.commit("left", "root")
	b.commit("right", "root")
	b.commit("leftmerge", "left", "right")
	b.commit("rightmerge", "right", "left")
	b.objects.fail[b.id("deep")] = errors.New("gone")
	if _, err := MergeBase(b.context(), b.id("leftmerge"), b.id("rightmerge")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("MergeBase returned %v, want %v", err, ErrNotFound)
	}
}
