package linelog

type filePair struct {
	at       int
	oldPath  string
	oldValid bool
	oldData  []byte
	newPath  string
	newData  []byte
}

type fileRanges struct {
	path    string
	ranges  spans
	pair    *filePair
	touched diffRanges
}

type rangeList []*fileRanges

func (l rangeList) find(path string) *fileRanges {
	for _, entry := range l {
		if entry.path == path {
			return entry
		}
	}
	return nil
}

func (l rangeList) insert(path string, begin, end int) rangeList {
	at := 0
	for index, entry := range l {
		if entry.path == path {
			entry.ranges = entry.ranges.with(begin, end)
			return l
		}
		if entry.path < path {
			at = index + 1
		}
	}
	created := &fileRanges{path: path, ranges: spans{{Start: begin, End: end}}}
	out := make(rangeList, 0, len(l)+1)
	out = append(out, l[:at]...)
	out = append(out, created)
	return append(out, l[at:]...)
}

func (l rangeList) copy() rangeList {
	out := make(rangeList, 0, len(l))
	for _, entry := range l {
		out = append(out, &fileRanges{path: entry.path, ranges: entry.ranges.clone()})
	}
	return out
}

func mergeLists(a, b rangeList) rangeList {
	var out rangeList
	for len(a) > 0 || len(b) > 0 {
		switch {
		case len(b) == 0 || len(a) > 0 && a[0].path < b[0].path:
			out = append(out, &fileRanges{path: a[0].path, ranges: a[0].ranges.clone()})
			a = a[1:]
		case len(a) == 0 || a[0].path > b[0].path:
			out = append(out, &fileRanges{path: b[0].path, ranges: b[0].ranges.clone()})
			b = b[1:]
		default:
			out = append(out, &fileRanges{path: a[0].path, ranges: union(a[0].ranges, b[0].ranges)})
			a, b = a[1:], b[1:]
		}
	}
	return out
}

func (l rangeList) live() bool {
	for _, entry := range l {
		if !entry.ranges.empty() {
			return true
		}
	}
	return false
}
