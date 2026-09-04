package diff

import "strings"

const spaceChars = " \t\n\v\f\r"

func isSpaceByte(c byte) bool {
	switch c {
	case ' ', '\t', '\n', '\v', '\f', '\r':
		return true
	default:
		return false
	}
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
			lines = append(lines, text)
			break
		}
		lines = append(lines, text[:at+1])
		text = text[at+1:]
	}
	return lines
}

func lineText(record string) (text string, newline bool) {
	if strings.HasSuffix(record, "\n") {
		return record[:len(record)-1], true
	}
	return record, false
}

func lineKey(record string, ws Whitespace) string {
	switch {
	case ws&IgnoreAllSpace != 0:
		return stripSpace(record)
	case ws&IgnoreSpaceChange != 0:
		return collapseSpace(record)
	case ws&IgnoreSpaceAtEOL != 0:
		return strings.TrimRight(record, spaceChars)
	default:
		return record
	}
}

func stripSpace(record string) string {
	if !strings.ContainsAny(record, spaceChars) {
		return record
	}
	var out strings.Builder
	out.Grow(len(record))
	for at := range len(record) {
		if !isSpaceByte(record[at]) {
			out.WriteByte(record[at])
		}
	}
	return out.String()
}

func collapseSpace(record string) string {
	var out strings.Builder
	out.Grow(len(record))
	for at := 0; at < len(record); {
		if !isSpaceByte(record[at]) {
			out.WriteByte(record[at])
			at++
			continue
		}
		for at < len(record) && isSpaceByte(record[at]) {
			at++
		}
		if at < len(record) {
			out.WriteByte(' ')
		}
	}
	return out.String()
}

func isBlankLine(record string, ws Whitespace) bool {
	if !ws.ignoresSpace() {
		return len(record) <= 1
	}
	return strings.TrimLeft(record, spaceChars) == ""
}

const maxIndent = 200

func lineIndent(record string) int {
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
