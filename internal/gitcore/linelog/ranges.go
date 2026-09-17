package linelog

import (
	"cmp"
	"slices"

	"github.com/oops1/gogit/internal/gitcore/diff"
)

type Span struct {
	Start int
	End   int
}

type spans []Span

type diffRanges struct {
	parent spans
	target spans
}

func (s spans) with(start, end int) spans {
	return append(s, Span{Start: start, End: end})
}

func (s spans) clone() spans {
	return slices.Clone(s)
}

func (s spans) empty() bool {
	return len(s) == 0
}

func sortAndMerge(s spans) spans {
	slices.SortStableFunc(s, func(a, b Span) int { return cmp.Compare(a.Start, b.Start) })
	out := s[:0]
	for _, r := range s {
		if r.Start == r.End {
			continue
		}
		if last := len(out) - 1; last >= 0 && r.Start <= out[last].End {
			out[last].End = max(out[last].End, r.End)
			continue
		}
		out = append(out, r)
	}
	return out
}

func union(a, b spans) spans {
	var out spans
	i, j := 0, 0
	for i < len(a) || j < len(b) {
		var next Span
		switch {
		case i < len(a) && j < len(b) && (a[i].Start < b[j].Start || a[i].Start == b[j].Start && a[i].End < b[j].End):
			next = a[i]
			i++
		case j < len(b) && (i == len(a) || a[i].Start >= b[j].Start):
			next = b[j]
			j++
		default:
			next = a[i]
			i++
		}
		switch last := len(out) - 1; {
		case next.Start == next.End:
		case last < 0 || out[last].End < next.Start:
			out = append(out, next)
		case out[last].End < next.End:
			out[last].End = next.End
		}
	}
	return out
}

func difference(a, b spans) spans {
	var out spans
	j := 0
	for _, r := range a {
		start, end := r.Start, r.End
		for start < end {
			for j < len(b) && start >= b[j].End {
				j++
			}
			if j >= len(b) || end < b[j].Start {
				out = out.with(start, end)
				break
			}
			if start < b[j].Start {
				out = out.with(start, b[j].Start)
			}
			start = b[j].End
		}
	}
	return out
}

func overlaps(a, b Span) bool {
	return a.End > b.Start && b.End > a.Start
}

func filterTouched(d diffRanges, rs spans) diffRanges {
	var out diffRanges
	j := 0
	for i, target := range d.target {
		for target.Start > rs[j].End {
			j++
			if j == len(rs) {
				return out
			}
		}
		if overlaps(target, rs[j]) {
			out.parent = append(out.parent, d.parent[i])
			out.target = append(out.target, target)
		}
	}
	return out
}

func shiftDiff(rs spans, d diffRanges) spans {
	var out spans
	j, offset := 0, 0
	for _, r := range rs {
		for j < len(d.target) && r.Start >= d.target[j].Start {
			offset += (d.parent[j].End - d.parent[j].Start) - (d.target[j].End - d.target[j].Start)
			j++
		}
		out = out.with(r.Start+offset, r.End+offset)
	}
	return out
}

func mapAcrossDiff(rs spans, d diffRanges) (spans, diffRanges) {
	touched := filterTouched(d, rs)
	return union(shiftDiff(difference(rs, touched.target), d), touched.parent), touched
}

func collectDiff(parent, target []byte) diffRanges {
	var out diffRanges
	for _, c := range diff.Changes(parent, target, diff.Options{}) {
		out.parent = out.parent.with(c.OldIndex, c.OldIndex+c.OldCount)
		out.target = out.target.with(c.NewIndex, c.NewIndex+c.NewCount)
	}
	return out
}

func MapSpans(parent, target []byte, lines []Span) []Span {
	merged := sortAndMerge(spans(lines).clone())
	if merged.empty() {
		return nil
	}
	mapped, _ := mapAcrossDiff(merged, collectDiff(parent, target))
	return mapped
}
