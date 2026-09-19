package branches

import (
	"slices"
	"strings"
)

type refNode struct {
	label    string
	entry    pathEntry
	leaf     bool
	children []refNode
}

type segmentedEntry struct {
	entry    pathEntry
	segments []string
}

func sortRefEntries(entries []pathEntry, mode SortMode) {
	slices.SortStableFunc(entries, func(x, y pathEntry) int { return compareRefEntries(x, y, mode) })
}

func compareRefEntries(x, y pathEntry, mode SortMode) int {
	switch mode {
	case SortByNameReverseNumbers:
		return compareNatural(x.path, y.path, true)
	case SortByCommitTime:
		if order := y.when.Compare(x.when); order != 0 {
			return order
		}
	}
	return compareNatural(x.path, y.path, false)
}

func groupRefEntries(entries []pathEntry, g Grouping) []refNode {
	if !g.ByPath {
		nodes := make([]refNode, 0, len(entries))
		for _, e := range entries {
			nodes = append(nodes, refNode{label: e.path, entry: e, leaf: true})
		}
		return nodes
	}
	items := make([]segmentedEntry, 0, len(entries))
	for _, e := range entries {
		items = append(items, segmentedEntry{entry: e, segments: pathSegments(e.path, g.AfterLastSlash)})
	}
	return groupSegmentedEntries(items, g)
}

func pathSegments(path string, afterLastSlash bool) []string {
	if !afterLastSlash {
		return strings.Split(path, "/")
	}
	slash := strings.LastIndex(path, "/")
	if slash < 0 {
		return []string{path}
	}
	return []string{path[:slash], path[slash+1:]}
}

func groupSegmentedEntries(items []segmentedEntry, g Grouping) []refNode {
	nodes := make([]refNode, 0, len(items))
	buckets := map[string][]segmentedEntry{}
	for _, item := range items {
		if len(item.segments) <= 1 {
			nodes = append(nodes, refNode{label: leafLabel(item), entry: item.entry, leaf: true})
			continue
		}
		key := item.segments[0]
		if _, seen := buckets[key]; !seen {
			nodes = append(nodes, refNode{label: key})
		}
		buckets[key] = append(buckets[key], segmentedEntry{entry: item.entry, segments: item.segments[1:]})
	}
	for i := range nodes {
		if nodes[i].leaf {
			continue
		}
		inner := buckets[nodes[i].label]
		if g.ExceptSingles && len(inner) == 1 {
			nodes[i] = refNode{
				label: nodes[i].label + "/" + strings.Join(inner[0].segments, "/"),
				entry: inner[0].entry,
				leaf:  true,
			}
			continue
		}
		nodes[i].children = groupSegmentedEntries(inner, g)
	}
	if g.GroupsFirst {
		slices.SortStableFunc(nodes, groupsBeforeLeaves)
	}
	return nodes
}

func leafLabel(item segmentedEntry) string {
	if len(item.segments) == 0 {
		return item.entry.path
	}
	return item.segments[0]
}

func groupsBeforeLeaves(x, y refNode) int {
	switch {
	case x.leaf == y.leaf:
		return 0
	case x.leaf:
		return 1
	}
	return -1
}

func compareNatural(a, b string, numbersDescending bool) int {
	ai, bi := 0, 0
	for ai < len(a) && bi < len(b) {
		if isDigit(a[ai]) && isDigit(b[bi]) {
			aNum, aNext := scanNumber(a, ai)
			bNum, bNext := scanNumber(b, bi)
			if aNum != bNum {
				if numbersDescending {
					return compareInt(bNum, aNum)
				}
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
