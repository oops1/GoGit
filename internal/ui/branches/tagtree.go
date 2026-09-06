package branches

import (
	"sort"
	"strings"
)

const (
	tagCollapseThreshold = 12
	tagRecentCount       = 3
)

type TagNode struct {
	Label    string
	Tag      string
	Children []TagNode
}

func (n TagNode) IsTag() bool { return n.Tag != "" }

func GroupTags(names []string) (recent []string, nodes []TagNode) {
	sorted := make([]string, len(names))
	copy(sorted, names)
	sort.Slice(sorted, func(i, j int) bool { return compareTagNames(sorted[i], sorted[j]) < 0 })
	if len(sorted) <= tagCollapseThreshold {
		return nil, leaves(sorted)
	}
	split := len(sorted) - tagRecentCount
	recent = reversed(sorted[split:])
	return recent, groupByPrefix(sorted[:split])
}

func leaves(names []string) []TagNode {
	nodes := make([]TagNode, 0, len(names))
	for _, name := range names {
		nodes = append(nodes, TagNode{Label: name, Tag: name})
	}
	return nodes
}

func reversed(names []string) []string {
	out := make([]string, 0, len(names))
	for i := len(names) - 1; i >= 0; i-- {
		out = append(out, names[i])
	}
	return out
}

func tagKey(name string) string { return strings.ReplaceAll(name, ".", "") }

func groupByPrefix(names []string) []TagNode {
	if len(names) == 0 {
		return nil
	}
	prefix := commonPrefix(names)
	if prefix == "" {
		return groupByCharacter(names, 0)
	}
	return []TagNode{{Label: prefix, Children: groupByCharacter(names, len(prefix))}}
}

func commonPrefix(names []string) string {
	prefix := majorPrefix(tagKey(names[0]))
	if prefix == "" {
		return ""
	}
	for _, name := range names[1:] {
		if !strings.HasPrefix(tagKey(name), prefix) {
			return ""
		}
	}
	if len(prefix) >= len(tagKey(names[0])) {
		return ""
	}
	return prefix
}

func majorPrefix(key string) string {
	at := 0
	for at < len(key) && !isDigit(key[at]) {
		at++
	}
	if at >= len(key) {
		return ""
	}
	for at < len(key) && isDigit(key[at]) {
		at++
		break
	}
	return key[:at]
}

func groupByCharacter(names []string, depth int) []TagNode {
	if len(names) == 1 {
		return leaves(names)
	}
	buckets := make([]string, 0, len(names))
	byChar := map[string][]string{}
	for _, name := range names {
		key := tagKey(name)
		if depth >= len(key)-1 {
			byChar[""] = append(byChar[""], name)
			continue
		}
		char := string(key[depth])
		if _, seen := byChar[char]; !seen {
			buckets = append(buckets, char)
		}
		byChar[char] = append(byChar[char], name)
	}
	nodes := leaves(byChar[""])
	sort.Strings(buckets)
	for _, char := range buckets {
		group := byChar[char]
		if len(group) == 1 {
			nodes = append(nodes, leaves(group)...)
			continue
		}
		nodes = append(nodes, TagNode{Label: char, Children: groupByCharacter(group, depth+1)})
	}
	return nodes
}

func compareTagNames(a, b string) int {
	ai, bi := 0, 0
	for ai < len(a) && bi < len(b) {
		if isDigit(a[ai]) && isDigit(b[bi]) {
			aNum, aNext := scanNumber(a, ai)
			bNum, bNext := scanNumber(b, bi)
			if aNum != bNum {
				return compareInt(aNum, bNum)
			}
			ai, bi = aNext, bNext
			continue
		}
		if a[ai] != b[bi] {
			return compareByte(a[ai], b[bi])
		}
		ai++
		bi++
	}
	return compareInt(len(a)-ai, len(b)-bi)
}

func scanNumber(s string, at int) (int, int) {
	value := 0
	for at < len(s) && isDigit(s[at]) {
		value = value*10 + int(s[at]-'0')
		at++
	}
	return value, at
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func compareInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

func compareByte(a, b byte) int { return compareInt(int(a), int(b)) }
