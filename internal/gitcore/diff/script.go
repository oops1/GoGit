package diff

import (
	"slices"
	"strings"
)

type change struct {
	i1     int
	i2     int
	chg1   int
	chg2   int
	ignore bool
}

func (e *env) script() []change {
	var changes []change
	a, b := e.a, e.b
	i1, i2 := a.count(), b.count()
	for i1 >= 0 || i2 >= 0 {
		if a.changed(i1-1) || b.changed(i2-1) {
			l1, l2 := i1, i2
			for a.changed(i1 - 1) {
				i1--
			}
			for b.changed(i2 - 1) {
				i2--
			}
			changes = append(changes, change{i1: i1, i2: i2, chg1: l1 - i1, chg2: l2 - i2})
		}
		i1--
		i2--
	}
	slices.Reverse(changes)
	return changes
}

func (e *env) markIgnorable(changes []change) {
	ws := e.opts.IgnoreWhitespace
	for at := range changes {
		ignore := true
		for step := 0; step < changes[at].chg1 && ignore; step++ {
			ignore = isBlankLine(e.a.recs[changes[at].i1+step], ws)
		}
		for step := 0; step < changes[at].chg2 && ignore; step++ {
			ignore = isBlankLine(e.b.recs[changes[at].i2+step], ws)
		}
		changes[at].ignore = ignore
	}
}

func getHunk(changes []change, from int, opts Options) (start, last int, ok bool) {
	maxCommon := 2*opts.Context + opts.InterHunkContext
	maxIgnorable := opts.Context
	ignored := 0

	start = from
	for at := from; at < len(changes) && changes[at].ignore; at++ {
		next := at + 1
		if next == len(changes) || changes[next].i1-(changes[at].i1+changes[at].chg1) >= maxIgnorable {
			start = next
		}
	}
	if start >= len(changes) {
		return 0, 0, false
	}

	last = start
	for previous, at := start, start+1; at < len(changes); previous, at = at, at+1 {
		distance := changes[at].i1 - (changes[previous].i1 + changes[previous].chg1)
		if distance > maxCommon {
			break
		}
		switch {
		case distance < maxIgnorable && (!changes[at].ignore || last == previous):
			last = at
			ignored = 0
		case distance < maxIgnorable && changes[at].ignore:
			ignored += changes[at].chg2
		case last != previous && changes[at].i1+ignored-(changes[last].i1+changes[last].chg1) > maxCommon:
			return start, last, true
		case !changes[at].ignore:
			last = at
			ignored = 0
		default:
			ignored += changes[at].chg2
		}
	}
	return start, last, true
}

func (e *env) line(s *source, at int, kind Kind) Line {
	text, newline := lineText(s.recs[at])
	return Line{Kind: kind, Text: text, NoNewline: !newline}
}

func (e *env) emit(changes []change) []Hunk {
	opts := e.opts
	var hunks []Hunk
	name, previous := "", -1
	for at := 0; at < len(changes); {
		first, last, ok := getHunk(changes, at, opts)
		if !ok {
			break
		}
		hunk := e.emitHunk(changes, first, last)
		name = e.functionName(hunk.OldStart-2, previous, name)
		previous = hunk.OldStart - 2
		hunk.Header = name
		hunks = append(hunks, hunk)
		at = last + 1
	}
	return hunks
}

const funcNameLimit = 80

func (e *env) functionName(start, limit int, previous string) string {
	step := 1
	if start > limit {
		step = -1
	}
	for at := start; at != limit && 0 <= at && at < e.a.count(); at += step {
		if name, ok := functionRecord(e.a.recs[at]); ok {
			return name
		}
	}
	return previous
}

func startsName(head byte) bool {
	return head == '_' || head == '$' || ('a' <= head && head <= 'z') || ('A' <= head && head <= 'Z')
}

func functionRecord(record string) (string, bool) {
	if record == "" || !startsName(record[0]) {
		return "", false
	}
	name := record
	if len(name) > funcNameLimit {
		name = name[:funcNameLimit]
	}
	return strings.TrimRight(name, spaceChars), true
}

func (e *env) emitHunk(changes []change, first, last int) Hunk {
	opts := e.opts
	s1 := max(changes[first].i1-opts.Context, 0)
	s2 := max(changes[first].i2-opts.Context, 0)
	tail := min(
		opts.Context,
		e.a.count()-(changes[last].i1+changes[last].chg1),
		e.b.count()-(changes[last].i2+changes[last].chg2),
	)
	e1 := changes[last].i1 + changes[last].chg1 + tail
	e2 := changes[last].i2 + changes[last].chg2 + tail

	hunk := Hunk{OldStart: s1 + 1, OldLines: e1 - s1, NewStart: s2 + 1, NewLines: e2 - s2}
	for ; s2 < changes[first].i2; s2++ {
		hunk.Lines = append(hunk.Lines, e.line(e.b, s2, KindContext))
	}
	s1 = changes[first].i1
	for current := first; ; current++ {
		for s1 < changes[current].i1 && s2 < changes[current].i2 {
			hunk.Lines = append(hunk.Lines, e.line(e.b, s2, KindContext))
			s1++
			s2++
		}
		for s1 = changes[current].i1; s1 < changes[current].i1+changes[current].chg1; s1++ {
			hunk.Lines = append(hunk.Lines, e.line(e.a, s1, KindDel))
		}
		for s2 = changes[current].i2; s2 < changes[current].i2+changes[current].chg2; s2++ {
			hunk.Lines = append(hunk.Lines, e.line(e.b, s2, KindAdd))
		}
		if current == last {
			break
		}
		s1 = changes[current].i1 + changes[current].chg1
		s2 = changes[current].i2 + changes[current].chg2
	}
	for s2 = changes[last].i2 + changes[last].chg2; s2 < e2; s2++ {
		hunk.Lines = append(hunk.Lines, e.line(e.b, s2, KindContext))
	}
	return hunk
}
