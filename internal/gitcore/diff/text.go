package diff

import "bytes"

const spaceChars = " \t\n\v\f\r"

func isSpaceByte(c byte) bool {
	switch c {
	case ' ', '\t', '\n', '\v', '\f', '\r':
		return true
	default:
		return false
	}
}

type Text struct {
	data  []byte
	offs  []int
	whole string
}

func (t Text) shared() Text {
	if t.whole == "" {
		t.whole = string(t.data)
	}
	return t
}

func (t Text) line(at int) string {
	return t.whole[t.offs[at]:t.offs[at+1]]
}

func NewText(data []byte) Text {
	return splitLines(data)
}

const maxPooledTexts = 8

type TextPool struct {
	free [][]int
}

func (p *TextPool) Text(data []byte) Text {
	last := len(p.free) - 1
	if len(data) == 0 || last < 0 {
		return splitLines(data)
	}
	offs := p.free[last]
	p.free = p.free[:last]
	return splitInto(offs[:1:cap(offs)], data)
}

func (p *TextPool) Release(text Text) {
	if text.offs != nil && len(p.free) < maxPooledTexts {
		p.free = append(p.free, text.offs)
	}
}

func splitLines(data []byte) Text {
	if len(data) == 0 {
		return Text{}
	}
	return splitInto(make([]int, 1, bytes.Count(data, []byte{'\n'})+2), data)
}

func splitInto(offs []int, data []byte) Text {
	offs[0] = 0
	for at := 0; at < len(data); {
		next := bytes.IndexByte(data[at:], '\n')
		if next < 0 {
			at = len(data)
		} else {
			at += next + 1
		}
		offs = append(offs, at)
	}
	return Text{data: data, offs: offs}
}

func (t Text) Count() int {
	if len(t.offs) == 0 {
		return 0
	}
	return len(t.offs) - 1
}

func (t Text) Ready() bool { return t.offs != nil }

func (t Text) at(line int) []byte {
	return t.data[t.offs[line]:t.offs[line+1]]
}

func (t Text) slice(from, count int) Text {
	if count == 0 {
		return Text{}
	}
	return Text{data: t.data, offs: t.offs[from : from+count+1], whole: t.whole}
}

func (t Text) span(from, to int) []byte {
	if from >= to {
		return nil
	}
	return t.data[t.offs[from]:t.offs[to]]
}

func lineTextOf(record string) (text string, newline bool) {
	if len(record) > 0 && record[len(record)-1] == '\n' {
		return record[:len(record)-1], true
	}
	return record, false
}

func lineKey(record, scratch []byte, ws Whitespace) ([]byte, []byte) {
	switch {
	case ws&IgnoreAllSpace != 0:
		scratch = appendWithoutSpace(scratch[:0], record)
		return scratch, scratch
	case ws&IgnoreSpaceChange != 0:
		scratch = appendCollapsedSpace(scratch[:0], record)
		return scratch, scratch
	case ws&IgnoreSpaceAtEOL != 0:
		return bytes.TrimRight(record, spaceChars), scratch
	case ws&IgnoreCRAtEOL != 0:
		return withoutCRAtEOL(record), scratch
	default:
		return record, scratch
	}
}

func withoutCRAtEOL(record []byte) []byte {
	if body, complete := bytes.CutSuffix(record, []byte{'\n'}); complete {
		return bytes.TrimSuffix(body, []byte{'\r'})
	}
	return record
}

func appendWithoutSpace(out, record []byte) []byte {
	for at := range len(record) {
		if !isSpaceByte(record[at]) {
			out = append(out, record[at])
		}
	}
	return out
}

func appendCollapsedSpace(out, record []byte) []byte {
	for at := 0; at < len(record); {
		if !isSpaceByte(record[at]) {
			out = append(out, record[at])
			at++
			continue
		}
		for at < len(record) && isSpaceByte(record[at]) {
			at++
		}
		if at < len(record) {
			out = append(out, ' ')
		}
	}
	return out
}

func isBlankLine(record []byte, ws Whitespace) bool {
	if !ws.ignoresSpace() {
		return len(record) <= 1
	}
	return len(bytes.TrimLeft(record, spaceChars)) == 0
}

const maxIndent = 200

func lineIndent(record []byte) int {
	indent := 0
	for at := range len(record) {
		c := record[at]
		if !isSpaceByte(c) {
			return indent
		}
		switch c {
		case ' ':
			indent++
		case '\t':
			indent += 8 - indent%8
		}
		if indent >= maxIndent {
			return maxIndent
		}
	}
	return -1
}
