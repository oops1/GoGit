package diff

import "strings"

func quoteEscape(c byte) string {
	switch c {
	case '\a':
		return `\a`
	case '\b':
		return `\b`
	case '\t':
		return `\t`
	case '\n':
		return `\n`
	case '\v':
		return `\v`
	case '\f':
		return `\f`
	case '\r':
		return `\r`
	case '"':
		return `\"`
	case '\\':
		return `\\`
	}
	if c < 0x20 || c >= 0x7f {
		return string([]byte{'\\', '0' + (c>>6)&3, '0' + (c>>3)&7, '0' + c&7})
	}
	return ""
}

func needsQuoting(name string) bool {
	for at := range len(name) {
		if quoteEscape(name[at]) != "" {
			return true
		}
	}
	return false
}

func quoteInner(name string, out *strings.Builder) {
	for at := range len(name) {
		if escape := quoteEscape(name[at]); escape != "" {
			out.WriteString(escape)
			continue
		}
		out.WriteByte(name[at])
	}
}

func quoteCStyle(name string) string {
	if !needsQuoting(name) {
		return name
	}
	var out strings.Builder
	out.WriteByte('"')
	quoteInner(name, &out)
	out.WriteByte('"')
	return out.String()
}

func quoteTwo(prefix, name string) string {
	if !needsQuoting(prefix) && !needsQuoting(name) {
		return prefix + name
	}
	var out strings.Builder
	out.WriteByte('"')
	quoteInner(prefix, &out)
	quoteInner(name, &out)
	out.WriteByte('"')
	return out.String()
}

func renameName(oldPath, newPath string) string {
	if needsQuoting(oldPath) || needsQuoting(newPath) {
		return quoteCStyle(oldPath) + " => " + quoteCStyle(newPath)
	}

	prefix := 0
	for at := 0; at < len(oldPath) && at < len(newPath) && oldPath[at] == newPath[at]; at++ {
		if oldPath[at] == '/' {
			prefix = at + 1
		}
	}

	suffix := 0
	adjust := 0
	if prefix > 0 {
		adjust = 1
	}
	oldAt, newAt := len(oldPath), len(newPath)
	for oldAt >= prefix-adjust && newAt >= prefix-adjust && byteAt(oldPath, oldAt) == byteAt(newPath, newAt) {
		if byteAt(oldPath, oldAt) == '/' {
			suffix = len(oldPath) - oldAt
		}
		oldAt--
		newAt--
	}

	oldMid := max(len(oldPath)-prefix-suffix, 0)
	newMid := max(len(newPath)-prefix-suffix, 0)

	var out strings.Builder
	if prefix+suffix > 0 {
		out.WriteString(oldPath[:prefix])
		out.WriteByte('{')
	}
	out.WriteString(oldPath[prefix : prefix+oldMid])
	out.WriteString(" => ")
	out.WriteString(newPath[prefix : prefix+newMid])
	if prefix+suffix > 0 {
		out.WriteByte('}')
		out.WriteString(oldPath[len(oldPath)-suffix:])
	}
	return out.String()
}

func byteAt(text string, at int) byte {
	if at >= len(text) {
		return 0
	}
	return text[at]
}
