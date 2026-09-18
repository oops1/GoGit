package linelog

import (
	"slices"
	"testing"
)

func sp(pairs ...int) spans {
	out := spans{}
	for at := 0; at+1 < len(pairs); at += 2 {
		out = append(out, Span{Start: pairs[at], End: pairs[at+1]})
	}
	return out
}

func sameSpans(a, b spans) bool {
	return slices.Equal(a, b) || len(a) == 0 && len(b) == 0
}

func TestSortAndMergeOrdersJoinsAndDropsEmptyRanges(t *testing.T) {
	cases := []struct {
		name string
		in   spans
		want spans
	}{
		{name: "unsorted overlapping", in: sp(10, 20, 0, 5, 3, 8), want: sp(0, 8, 10, 20)},
		{name: "adjacent", in: sp(0, 5, 5, 9), want: sp(0, 9)},
		{name: "empty dropped", in: sp(4, 4, 1, 2), want: sp(1, 2)},
		{name: "contained", in: sp(0, 10, 2, 3), want: sp(0, 10)},
		{name: "nothing", in: sp(), want: sp()},
	}
	for _, c := range cases {
		if got := sortAndMerge(c.in); !sameSpans(got, c.want) {
			t.Errorf("%s: sortAndMerge = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestUnionJoinsOverlappingRangesFromBothSets(t *testing.T) {
	cases := []struct {
		name string
		a, b spans
		want spans
	}{
		{name: "interleaved", a: sp(0, 2, 10, 12), b: sp(5, 6, 11, 15), want: sp(0, 2, 5, 6, 10, 15)},
		{name: "same start shorter first", a: sp(0, 3), b: sp(0, 5), want: sp(0, 5)},
		{name: "same start longer first", a: sp(0, 5), b: sp(0, 3), want: sp(0, 5)},
		{name: "empty ranges skipped", a: sp(1, 1), b: sp(2, 4, 6, 6), want: sp(2, 4)},
		{name: "adjacent join", a: sp(0, 2), b: sp(2, 4), want: sp(0, 4)},
		{name: "a only", a: sp(1, 2, 3, 4), b: sp(), want: sp(1, 2, 3, 4)},
		{name: "b only", a: sp(), b: sp(1, 2), want: sp(1, 2)},
	}
	for _, c := range cases {
		if got := union(c.a, c.b); !sameSpans(got, c.want) {
			t.Errorf("%s: union = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestDifferenceRemovesTheTargetSide(t *testing.T) {
	cases := []struct {
		name string
		a, b spans
		want spans
	}{
		{name: "hole in the middle", a: sp(0, 10), b: sp(3, 5), want: sp(0, 3, 5, 10)},
		{name: "cut the head", a: sp(2, 10), b: sp(0, 4), want: sp(4, 10)},
		{name: "cut the tail", a: sp(0, 10), b: sp(8, 12), want: sp(0, 8)},
		{name: "fully covered", a: sp(3, 5), b: sp(0, 10), want: sp()},
		{name: "b before and after", a: sp(5, 8, 20, 30), b: sp(0, 2, 9, 10, 25, 26), want: sp(5, 8, 20, 25, 26, 30)},
		{name: "touching end", a: sp(0, 5), b: sp(5, 7), want: sp(0, 5)},
		{name: "nothing removed", a: sp(0, 5), b: sp(), want: sp(0, 5)},
	}
	for _, c := range cases {
		if got := difference(c.a, c.b); !sameSpans(got, c.want) {
			t.Errorf("%s: difference = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestFilterTouchedKeepsHunksOverlappingTheCurrentRange(t *testing.T) {
	d := diffRanges{parent: sp(0, 1, 4, 4, 6, 9, 20, 22), target: sp(0, 2, 5, 5, 7, 7, 30, 31)}

	got := filterTouched(d, sp(1, 6))
	if !sameSpans(got.target, sp(0, 2, 5, 5)) || !sameSpans(got.parent, sp(0, 1, 4, 4)) {
		t.Fatalf("touched = %+v", got)
	}

	past := filterTouched(d, sp(0, 1))
	if !sameSpans(past.target, sp(0, 2)) {
		t.Fatalf("touched past the last range = %+v", past)
	}
}

func TestFilterTouchedComparesEachHunkWithOneRangeOnly(t *testing.T) {
	d := diffRanges{parent: sp(5, 9), target: sp(5, 9)}

	got := filterTouched(d, sp(0, 5, 7, 10))

	if len(got.target) != 0 {
		t.Fatalf("a hunk starting at the end of a range was taken as touching the next one: %+v", got)
	}
}

func TestShiftDiffMovesRangesByTheLinesAddedAbove(t *testing.T) {
	d := diffRanges{parent: sp(0, 0, 10, 12), target: sp(0, 3, 13, 13)}

	got := shiftDiff(sp(5, 8, 20, 25), d)

	if !sameSpans(got, sp(2, 5, 19, 24)) {
		t.Fatalf("shifted = %v", got)
	}
}

func TestMapAcrossDiffReplacesTouchedLinesWithTheParentSide(t *testing.T) {
	parent := []byte("a\nb\nc\nd\ne\n")
	target := []byte("a\nB\nc\nx\ny\nd\ne\n")

	mapped, touched := mapAcrossDiff(sp(1, 4), collectDiff(parent, target))

	if !sameSpans(mapped, sp(1, 3)) {
		t.Fatalf("mapped = %v", mapped)
	}
	if !sameSpans(touched.target, sp(1, 2, 3, 5)) || !sameSpans(touched.parent, sp(1, 2, 3, 3)) {
		t.Fatalf("touched = %+v", touched)
	}
}

func TestMapSpansFollowsLinesIntoTheBaseVersion(t *testing.T) {
	base := []byte("one\ntwo\nthree\nfour\n")
	edited := []byte("zero\none\ntwo\nTHREE\nfour\n")

	if got := MapSpans(base, edited, []Span{{Start: 4, End: 5}, {Start: 1, End: 3}}); !slices.Equal(got, []Span{{Start: 0, End: 2}, {Start: 3, End: 4}}) {
		t.Fatalf("mapped = %v", got)
	}
	if got := MapSpans(base, edited, []Span{{Start: 0, End: 1}}); len(got) != 0 {
		t.Fatalf("a new line mapped to %v", got)
	}
	if got := MapSpans(base, edited, []Span{{Start: 2, End: 2}}); got != nil {
		t.Fatalf("an empty selection mapped to %v", got)
	}
}
