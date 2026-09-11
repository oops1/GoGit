package trailer

import "strings"

type Line struct {
	Key   string
	Value string
}

var extraAttributionKeys = map[string]bool{
	"thanks-to":     true,
	"co-authored":   true,
	"attributed-to": true,
}

func IsAttribution(key string) bool {
	folded := strings.ToLower(strings.TrimSpace(key))
	return strings.HasSuffix(folded, "-by") || extraAttributionKeys[folded]
}

func WithoutAttribution(message string) string {
	body, lines, ok := Block(message)
	if !ok {
		return message
	}
	kept := make([]Line, 0, len(lines))
	for _, line := range lines {
		if !IsAttribution(line.Key) {
			kept = append(kept, line)
		}
	}
	if len(kept) == len(lines) {
		return message
	}
	return join(body, kept)
}

func Block(message string) (body string, lines []Line, ok bool) {
	text := strings.TrimRight(message, "\n")
	at := strings.LastIndex(text, "\n\n")
	if at < 0 {
		return "", nil, false
	}
	block := text[at+2:]
	lines, ok = parseBlock(block)
	if !ok {
		return "", nil, false
	}
	return text[:at], lines, true
}

func parseBlock(block string) ([]Line, bool) {
	var lines []Line
	for _, raw := range strings.Split(block, "\n") {
		if isContinuation(raw) && len(lines) > 0 {
			last := &lines[len(lines)-1]
			last.Value += "\n" + raw
			continue
		}
		key, value, found := strings.Cut(raw, ":")
		if !found || !isKey(key) {
			return nil, false
		}
		lines = append(lines, Line{Key: key, Value: strings.TrimSpace(value)})
	}
	return lines, len(lines) > 0
}

func isContinuation(raw string) bool {
	return strings.HasPrefix(raw, " ") || strings.HasPrefix(raw, "\t")
}

func isKey(key string) bool {
	if key == "" {
		return false
	}
	for i, r := range key {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9', r == '-':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func join(body string, lines []Line) string {
	if len(lines) == 0 {
		return strings.TrimRight(body, "\n") + "\n"
	}
	var out strings.Builder
	out.WriteString(strings.TrimRight(body, "\n"))
	out.WriteString("\n\n")
	for _, line := range lines {
		out.WriteString(line.Key)
		out.WriteString(": ")
		out.WriteString(line.Value)
		out.WriteString("\n")
	}
	return out.String()
}

func Attributed(message string) []string {
	_, lines, ok := Block(message)
	if !ok {
		return nil
	}
	var names []string
	seen := map[string]bool{}
	for _, line := range lines {
		if !IsAttribution(line.Key) {
			continue
		}
		name := personOf(line.Value)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	return names
}

func personOf(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	if at := strings.IndexByte(value, '<'); at >= 0 {
		if name := strings.TrimSpace(value[:at]); name != "" {
			return name
		}
		return strings.Trim(strings.TrimSpace(value[at:]), "<>")
	}
	return value
}
