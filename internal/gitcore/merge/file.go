package merge

import (
	"slices"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/diff"
)

type Style int

const (
	StyleMerge Style = iota
	StyleDiff3
	StyleZDiff3
)

type Level int

const (
	LevelZealous Level = iota
	LevelZealousAlnum
)

const (
	oursMarker        = '<'
	baseMarker        = '|'
	middleMarker      = '='
	theirsMarker      = '>'
	DefaultMarkerSize = 7
	joinGap           = 3
)

type Labels struct {
	Ours   string
	Base   string
	Theirs string
}

type Options struct {
	Style      Style
	Labels     Labels
	Diff       diff.Options
	MarkerSize int
	Level      Level
	Union      bool
}

func (o Options) markerSize() int {
	if o.MarkerSize <= 0 {
		return DefaultMarkerSize
	}
	return o.MarkerSize
}

type Result struct {
	Content   []byte
	Conflicts int
}

type Chunk struct {
	Conflict bool
	Ours     []string
	Base     []string
	Theirs   []string
	Merged   []string
}

type edit struct {
	i1, chg1 int
	i2, chg2 int
}

const (
	nodeConflict  = 0
	nodeOurs      = 1
	nodeTheirs    = 2
	nodeUnion     = 3
	nodeIdentical = 4
)

type node struct {
	mode     int
	i0, chg0 int
	i1, chg1 int
	i2, chg2 int
}

type sides struct {
	base   []string
	ours   []string
	theirs []string
}

func File(base, ours, theirs []byte, opts Options) Result {
	s := sides{base: splitLines(base), ours: splitLines(ours), theirs: splitLines(theirs)}
	return s.render(s.nodes(base, ours, theirs, opts), opts)
}

func Chunks(base, ours, theirs []byte, opts Options) []Chunk {
	s := sides{base: splitLines(base), ours: splitLines(ours), theirs: splitLines(theirs)}
	return s.chunks(s.nodes(base, ours, theirs, opts))
}

func (s sides) nodes(base, ours, theirs []byte, opts Options) []node {
	nodes := s.combine(editsOf(base, ours, opts.Diff), editsOf(base, theirs, opts.Diff))
	switch opts.Style {
	case StyleDiff3:
		return nodes
	case StyleZDiff3:
		return s.trimConflicts(nodes)
	}
	return s.simplify(s.refine(nodes, opts.Diff), opts.Level == LevelZealousAlnum)
}

func (s sides) combine(ours, theirs []edit) []node {
	var out []node
	for len(ours) > 0 && len(theirs) > 0 {
		c1, c2 := ours[0], theirs[0]
		if c1.i1+c1.chg1 < c2.i1 {
			out = appendNode(out, node{mode: nodeOurs, i0: c1.i1, chg0: c1.chg1, i1: c1.i2, chg1: c1.chg2, i2: c2.i2 - c2.i1 + c1.i1, chg2: c1.chg1})
			ours = ours[1:]
			continue
		}
		if c2.i1+c2.chg1 < c1.i1 {
			out = appendNode(out, node{mode: nodeTheirs, i0: c2.i1, chg0: c2.chg1, i1: c1.i2 - c1.i1 + c2.i1, chg1: c2.chg1, i2: c2.i2, chg2: c2.chg2})
			theirs = theirs[1:]
			continue
		}
		if !s.sameEdit(c1, c2) {
			out = appendNode(out, conflictOf(c1, c2))
		}
		end1, end2 := c1.i1+c1.chg1, c2.i1+c2.chg1
		if end1 >= end2 {
			theirs = theirs[1:]
		}
		if end2 >= end1 {
			ours = ours[1:]
		}
	}
	for _, c1 := range ours {
		out = appendNode(out, node{mode: nodeOurs, i0: c1.i1, chg0: c1.chg1, i1: c1.i2, chg1: c1.chg2, i2: c1.i1 + len(s.theirs) - len(s.base), chg2: c1.chg1})
	}
	for _, c2 := range theirs {
		out = appendNode(out, node{mode: nodeTheirs, i0: c2.i1, chg0: c2.chg1, i1: c2.i1 + len(s.ours) - len(s.base), chg1: c2.chg1, i2: c2.i2, chg2: c2.chg2})
	}
	return out
}

func (s sides) sameEdit(c1, c2 edit) bool {
	return c1.i1 == c2.i1 && c1.chg1 == c2.chg1 && c1.chg2 == c2.chg2 &&
		slices.Equal(s.ours[c1.i2:c1.i2+c1.chg2], s.theirs[c2.i2:c2.i2+c2.chg2])
}

func conflictOf(c1, c2 edit) node {
	off := c1.i1 - c2.i1
	ffo := off + c1.chg1 - c2.chg1
	i0, i1, i2 := c1.i1, c1.i2, c2.i2
	if off > 0 {
		i0 -= off
		i1 -= off
	} else {
		i2 += off
	}
	chg0 := c1.i1 + c1.chg1 - i0
	chg1 := c1.i2 + c1.chg2 - i1
	chg2 := c2.i2 + c2.chg2 - i2
	if ffo < 0 {
		chg0 -= ffo
		chg1 -= ffo
	} else {
		chg2 += ffo
	}
	return node{mode: nodeConflict, i0: i0, chg0: chg0, i1: i1, chg1: chg1, i2: i2, chg2: chg2}
}

func appendNode(out []node, n node) []node {
	if last := len(out) - 1; last >= 0 {
		m := &out[last]
		if n.i1 <= m.i1+m.chg1 || n.i2 <= m.i2+m.chg2 {
			if n.mode != m.mode {
				m.mode = nodeConflict
			}
			m.chg0 = n.i0 + n.chg0 - m.i0
			m.chg1 = n.i1 + n.chg1 - m.i1
			m.chg2 = n.i2 + n.chg2 - m.i2
			return out
		}
	}
	return append(out, n)
}

func (s sides) refine(nodes []node, opts diff.Options) []node {
	out := make([]node, 0, len(nodes))
	for _, m := range nodes {
		if m.mode != nodeConflict || m.chg1 == 0 || m.chg2 == 0 {
			out = append(out, m)
			continue
		}
		edits := editsOf([]byte(strings.Join(s.ours[m.i1:m.i1+m.chg1], "")), []byte(strings.Join(s.theirs[m.i2:m.i2+m.chg2], "")), opts)
		if len(edits) == 0 {
			m.mode = nodeIdentical
			out = append(out, m)
			continue
		}
		for at, e := range edits {
			piece := node{mode: nodeConflict, i0: m.i0 + m.chg0, i1: m.i1 + e.i1, chg1: e.chg1, i2: m.i2 + e.i2, chg2: e.chg2}
			if at == 0 {
				piece.i0, piece.chg0 = m.i0, m.chg0
			}
			out = append(out, piece)
		}
	}
	return out
}

func (s sides) simplify(nodes []node, unlessAlnum bool) []node {
	out := make([]node, 0, len(nodes))
	for _, next := range nodes {
		if last := len(out) - 1; last >= 0 && s.joinable(out[last], next, unlessAlnum) {
			m := &out[last]
			m.chg0 = next.i0 + next.chg0 - m.i0
			m.chg1 = next.i1 + next.chg1 - m.i1
			m.chg2 = next.i2 + next.chg2 - m.i2
			continue
		}
		out = append(out, next)
	}
	return out
}

func (s sides) joinable(m, next node, unlessAlnum bool) bool {
	if m.mode != nodeConflict || next.mode != nodeConflict {
		return false
	}
	begin, end := m.i1+m.chg1, next.i1
	return end-begin <= joinGap || unlessAlnum && !containsAlnum(s.ours[begin:end])
}

func containsAlnum(lines []string) bool {
	for _, line := range lines {
		for i := range len(line) {
			c := line[i]
			if '0' <= c && c <= '9' || 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' {
				return true
			}
		}
	}
	return false
}

func (s sides) trimConflicts(nodes []node) []node {
	for at := range nodes {
		m := &nodes[at]
		if m.mode != nodeConflict {
			continue
		}
		for m.chg1 > 0 && m.chg2 > 0 && s.ours[m.i1] == s.theirs[m.i2] {
			m.i1++
			m.i2++
			m.chg1--
			m.chg2--
		}
		for m.chg1 > 0 && m.chg2 > 0 && s.ours[m.i1+m.chg1-1] == s.theirs[m.i2+m.chg2-1] {
			m.chg1--
			m.chg2--
		}
	}
	return nodes
}

func (s sides) render(nodes []node, opts Options) Result {
	var out []string
	conflicts, i := 0, 0
	for _, m := range nodes {
		mode := m.mode
		if mode == nodeConflict && opts.Union {
			mode = nodeUnion
		}
		switch {
		case mode == nodeConflict:
			conflicts++
			out = append(out, s.ours[i:m.i1]...)
			out = s.conflictHunk(out, m, opts)
		case mode&nodeUnion != 0:
			out = append(out, s.ours[i:m.i1]...)
			if mode&nodeOurs != 0 {
				out = appendRecords(out, s.ours[m.i1:m.i1+m.chg1], s.needsCR(m), mode&nodeTheirs != 0)
			}
			if mode&nodeTheirs != 0 {
				out = append(out, s.theirs[m.i2:m.i2+m.chg2]...)
			}
		default:
			continue
		}
		i = m.i1 + m.chg1
	}
	out = append(out, s.ours[i:]...)
	return Result{Content: []byte(strings.Join(out, "")), Conflicts: conflicts}
}

func (s sides) conflictHunk(out []string, m node, opts Options) []string {
	crlf := s.needsCR(m)
	out = append(out, opts.marker(oursMarker, opts.Labels.Ours, crlf))
	out = appendRecords(out, s.ours[m.i1:m.i1+m.chg1], crlf, true)
	if opts.Style != StyleMerge {
		out = append(out, opts.marker(baseMarker, opts.Labels.Base, crlf))
		out = appendRecords(out, s.base[m.i0:m.i0+m.chg0], crlf, true)
	}
	out = append(out, opts.marker(middleMarker, "", crlf))
	out = appendRecords(out, s.theirs[m.i2:m.i2+m.chg2], crlf, true)
	return append(out, opts.marker(theirsMarker, opts.Labels.Theirs, crlf))
}

func appendRecords(out, lines []string, crlf, addNewline bool) []string {
	out = append(out, lines...)
	if !addNewline || len(lines) == 0 || strings.HasSuffix(lines[len(lines)-1], "\n") {
		return out
	}
	return append(out, lineEnd(crlf))
}

func lineEnd(crlf bool) string {
	if crlf {
		return "\r\n"
	}
	return "\n"
}

func (s sides) needsCR(m node) bool {
	crlf := eolIsCRLF(s.ours, max(m.i1-1, 0))
	if crlf != 0 {
		crlf = eolIsCRLF(s.theirs, max(m.i2-1, 0))
	}
	if crlf != 0 {
		crlf = eolIsCRLF(s.base, 0)
	}
	return crlf > 0
}

func eolIsCRLF(lines []string, at int) int {
	switch {
	case at < len(lines)-1:
		return crlfFlag(lines[at])
	case len(lines) == 0:
		return -1
	case strings.HasSuffix(lines[at], "\n"):
		return crlfFlag(lines[at])
	case at == 0:
		return -1
	}
	return crlfFlag(lines[at-1])
}

func crlfFlag(line string) int {
	if strings.HasSuffix(line, "\r\n") {
		return 1
	}
	return 0
}

func (o Options) marker(sign byte, label string, crlf bool) string {
	mark := strings.Repeat(string(sign), o.markerSize())
	if label != "" {
		mark += " " + label
	}
	return mark + lineEnd(crlf)
}

func (s sides) chunks(nodes []node) []Chunk {
	var out []Chunk
	p0, p1, p2 := 0, 0, 0
	for _, m := range nodes {
		out = appendClean(out, Chunk{Ours: s.ours[p1:m.i1], Base: s.base[p0:m.i0], Theirs: s.theirs[p2:m.i2], Merged: s.ours[p1:m.i1]})
		current := Chunk{Ours: s.ours[m.i1 : m.i1+m.chg1], Base: s.base[m.i0 : m.i0+m.chg0], Theirs: s.theirs[m.i2 : m.i2+m.chg2]}
		switch m.mode {
		case nodeConflict:
			current.Conflict = true
			out = append(out, cloneChunk(current))
		case nodeTheirs:
			current.Merged = current.Theirs
			out = appendClean(out, current)
		default:
			current.Merged = current.Ours
			out = appendClean(out, current)
		}
		p0, p1, p2 = m.i0+m.chg0, m.i1+m.chg1, m.i2+m.chg2
	}
	return appendClean(out, Chunk{Ours: s.ours[p1:], Base: s.base[p0:], Theirs: s.theirs[p2:], Merged: s.ours[p1:]})
}

func appendClean(out []Chunk, clean Chunk) []Chunk {
	if len(clean.Merged) == 0 && len(clean.Ours) == 0 && len(clean.Base) == 0 && len(clean.Theirs) == 0 {
		return out
	}
	if last := len(out) - 1; last >= 0 && !out[last].Conflict {
		out[last].Ours = append(out[last].Ours, clean.Ours...)
		out[last].Base = append(out[last].Base, clean.Base...)
		out[last].Theirs = append(out[last].Theirs, clean.Theirs...)
		out[last].Merged = append(out[last].Merged, clean.Merged...)
		return out
	}
	return append(out, cloneChunk(clean))
}

func cloneChunk(c Chunk) Chunk {
	c.Ours = slices.Clone(c.Ours)
	c.Base = slices.Clone(c.Base)
	c.Theirs = slices.Clone(c.Theirs)
	c.Merged = slices.Clone(c.Merged)
	return c
}

func editsOf(base, side []byte, opts diff.Options) []edit {
	opts.Context = 0
	opts.InterHunkContext = 0
	hunks := diff.Blobs(base, side, opts)
	out := make([]edit, 0, len(hunks))
	for _, hunk := range hunks {
		out = append(out, edit{i1: hunk.OldStart - 1, chg1: hunk.OldLines, i2: hunk.NewStart - 1, chg2: hunk.NewLines})
	}
	return out
}

func splitLines(data []byte) []string {
	if len(data) == 0 {
		return nil
	}
	text := string(data)
	lines := make([]string, 0, strings.Count(text, "\n")+1)
	for len(text) > 0 {
		at := strings.IndexByte(text, '\n')
		if at < 0 {
			return append(lines, text)
		}
		lines = append(lines, text[:at+1])
		text = text[at+1:]
	}
	return lines
}
