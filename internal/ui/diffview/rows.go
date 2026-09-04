package diffview

import "unicode/utf8"

type Mode int

const (
	SideBySide Mode = iota
	Unified
)

type cell struct {
	filled bool
	kind   Kind
	oldNo  int
	newNo  int
	text   string
	spans  []Span
	line   int
}

type row struct {
	hunk   int
	header bool
	text   string
	left   cell
	right  cell
}

type rowSet struct {
	rows     []row
	maxRunes int
	digits   int
}

func buildRows(doc Document, mode Mode) rowSet {
	var rows []row
	for hi, hunk := range doc.Hunks {
		if hunk.Header != "" {
			rows = append(rows, row{hunk: hi, header: true, text: hunk.Header})
		}
		if mode == Unified {
			rows = append(rows, unifiedRows(hi, hunk)...)
			continue
		}
		rows = append(rows, sideRows(hi, hunk)...)
	}
	return rowSet{rows: rows, maxRunes: widestRow(rows, mode), digits: digitsOf(maxNumber(rows))}
}

func unifiedRows(hi int, hunk Hunk) []row {
	rows := make([]row, 0, len(hunk.Lines))
	for i, line := range hunk.Lines {
		if line.Kind == HunkHeader {
			rows = append(rows, row{hunk: hi, header: true, text: line.Text})
			continue
		}
		rows = append(rows, row{hunk: hi, left: unifiedCell(line, i)})
	}
	return rows
}

func sideRows(hi int, hunk Hunk) []row {
	lines := hunk.Lines
	rows := make([]row, 0, len(lines))
	i := 0
	for i < len(lines) {
		switch lines[i].Kind {
		case Removed:
			removedStart := i
			for i < len(lines) && lines[i].Kind == Removed {
				i++
			}
			addedStart := i
			for i < len(lines) && lines[i].Kind == Added {
				i++
			}
			rows = append(rows, pairedRows(hi, lines, removedStart, addedStart, i)...)
		case Added:
			rows = append(rows, row{hunk: hi, right: rightCell(lines[i], i)})
			i++
		case HunkHeader:
			rows = append(rows, row{hunk: hi, header: true, text: lines[i].Text})
			i++
		case NoNewline:
			rows = append(rows, noNewlineRow(hi, lines, i))
			i++
		default:
			rows = append(rows, row{hunk: hi, left: leftCell(lines[i], i), right: rightCell(lines[i], i)})
			i++
		}
	}
	return rows
}

func pairedRows(hi int, lines []Line, removedStart, addedStart, end int) []row {
	removed := lines[removedStart:addedStart]
	added := lines[addedStart:end]
	rows := make([]row, 0, max(len(removed), len(added)))
	for i := range max(len(removed), len(added)) {
		var r row
		r.hunk = hi
		if i < len(removed) {
			r.left = leftCell(removed[i], removedStart+i)
		}
		if i < len(added) {
			r.right = rightCell(added[i], addedStart+i)
		}
		rows = append(rows, r)
	}
	return rows
}

func noNewlineRow(hi int, lines []Line, i int) row {
	r := row{hunk: hi}
	switch previousKind(lines, i) {
	case Removed:
		r.left = leftCell(lines[i], i)
	case Added:
		r.right = rightCell(lines[i], i)
	default:
		r.left = leftCell(lines[i], i)
		r.right = rightCell(lines[i], i)
	}
	return r
}

func previousKind(lines []Line, i int) Kind {
	for j := i - 1; j >= 0; j-- {
		if lines[j].Kind != NoNewline {
			return lines[j].Kind
		}
	}
	return Context
}

func leftCell(line Line, index int) cell {
	return cell{filled: true, kind: line.Kind, oldNo: line.OldNo, text: line.Text, spans: line.Spans, line: index}
}

func rightCell(line Line, index int) cell {
	return cell{filled: true, kind: line.Kind, newNo: line.NewNo, text: line.Text, spans: line.Spans, line: index}
}

func unifiedCell(line Line, index int) cell {
	return cell{
		filled: true,
		kind:   line.Kind,
		oldNo:  line.OldNo,
		newNo:  line.NewNo,
		text:   line.Text,
		spans:  line.Spans,
		line:   index,
	}
}

func widestRow(rows []row, mode Mode) int {
	widest := 0
	for _, r := range rows {
		if r.header {
			widest = max(widest, utf8.RuneCountInString(r.text))
			continue
		}
		if mode == Unified {
			widest = max(widest, utf8.RuneCountInString(r.left.text)+1)
			continue
		}
		widest = max(widest, utf8.RuneCountInString(r.left.text), utf8.RuneCountInString(r.right.text))
	}
	return widest
}

func maxNumber(rows []row) int {
	highest := 0
	for _, r := range rows {
		highest = max(highest, r.left.oldNo, r.left.newNo, r.right.oldNo, r.right.newNo)
	}
	return highest
}

func digitsOf(n int) int {
	digits := 1
	for n >= 10 {
		n /= 10
		digits++
	}
	return digits
}

func (r row) lineFor(rightSide bool) int {
	switch {
	case r.header:
		return -1
	case rightSide && r.right.filled:
		return r.right.line
	case !rightSide && r.left.filled:
		return r.left.line
	case r.left.filled:
		return r.left.line
	default:
		return r.right.line
	}
}
