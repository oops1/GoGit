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

func tagPath(name string) []string {
	parts := strings.Split(name, ".")
	segments := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			segments = append(segments, p)
		}
	}
	if len(segments) < 2 {
		return nil
	}
	path := []string{segments[0]}
	for _, segment := range segments[1 : len(segments)-1] {
		for _, digit := range segment {
			path = append(path, string(digit))
		}
	}
	return path
}

func groupByPrefix(names []string) []TagNode {
	return groupAtDepth(names, 0)
}

func groupAtDepth(names []string, depth int) []TagNode {
	order := make([]string, 0, len(names))
	buckets := map[string][]string{}
	var here []string
	for _, name := range names {
		path := tagPath(name)
		if depth >= len(path) {
			here = append(here, name)
			continue
		}
		key := path[depth]
		if _, seen := buckets[key]; !seen {
			order = append(order, key)
		}
		buckets[key] = append(buckets[key], name)
	}
	sort.Slice(order, func(i, j int) bool { return compareTagNames(order[i], order[j]) < 0 })
	nodes := leaves(here)
	for _, key := range order {
		group := buckets[key]
		if len(group) == 1 {
			nodes = append(nodes, leaves(group)...)
			continue
		}
		nodes = append(nodes, TagNode{Label: key, Children: groupAtDepth(group, depth+1)})
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
