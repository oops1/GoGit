package diff

const (
	patienceNone      = 0
	patienceNonUnique = -1
	patienceEnd       = -1
)

type patienceEntry struct {
	line1    int
	line2    int
	next     int
	previous int
}

func (e *env) patience(line1, count1, line2, count2 int) {
	if count1 == 0 {
		e.markRange(e.b, line2, count2)
		return
	}
	if count2 == 0 {
		e.markRange(e.a, line1, count1)
		return
	}
	entries, hasMatches := e.patienceEntries(line1, count1, line2, count2)
	if !hasMatches {
		e.markRange(e.a, line1, count1)
		e.markRange(e.b, line2, count2)
		return
	}
	first := longestUniqueSequence(entries)
	if first == patienceEnd {
		e.fallBack(line1, count1, line2, count2)
		return
	}
	e.walkUniqueSequence(entries, first, line1, count1, line2, count2)
}

func (e *env) markRange(s *source, line, count int) {
	for at := range count {
		s.mark(line - 1 + at)
	}
}

func (e *env) patienceEntries(line1, count1, line2, count2 int) ([]patienceEntry, bool) {
	byID := make(map[int]int, count1)
	entries := make([]patienceEntry, 0, count1)
	for line := line1; line < line1+count1; line++ {
		id := e.a.ids[line-1]
		if at, seen := byID[id]; seen {
			entries[at].line2 = patienceNonUnique
			continue
		}
		byID[id] = len(entries)
		entries = append(entries, patienceEntry{line1: line, next: patienceEnd, previous: patienceEnd})
	}
	hasMatches := false
	for line := line2; line < line2+count2; line++ {
		at, seen := byID[e.b.ids[line-1]]
		if !seen {
			continue
		}
		hasMatches = true
		if entries[at].line2 != patienceNone {
			entries[at].line2 = patienceNonUnique
			continue
		}
		entries[at].line2 = line
	}
	return entries, hasMatches
}

func longestUniqueSequence(entries []patienceEntry) int {
	var sequence []int
	for at := range entries {
		entry := &entries[at]
		if entry.line2 == patienceNone || entry.line2 == patienceNonUnique {
			continue
		}
		left, right := -1, len(sequence)
		for left+1 < right {
			middle := left + (right-left)/2
			if entries[sequence[middle]].line2 > entry.line2 {
				right = middle
			} else {
				left = middle
			}
		}
		entry.previous = patienceEnd
		if left >= 0 {
			entry.previous = sequence[left]
		}
		if left+1 == len(sequence) {
			sequence = append(sequence, at)
			continue
		}
		sequence[left+1] = at
	}
	if len(sequence) == 0 {
		return patienceEnd
	}
	current := sequence[len(sequence)-1]
	entries[current].next = patienceEnd
	for entries[current].previous != patienceEnd {
		previous := entries[current].previous
		entries[previous].next = current
		current = previous
	}
	return current
}

func (e *env) walkUniqueSequence(entries []patienceEntry, first, line1, count1, line2, count2 int) {
	end1, end2 := line1+count1, line2+count2
	for {
		next1, next2 := end1, end2
		if first != patienceEnd {
			next1, next2 = entries[first].line1, entries[first].line2
			for next1 > line1 && next2 > line2 && e.a.ids[next1-2] == e.b.ids[next2-2] {
				next1--
				next2--
			}
		}
		for line1 < next1 && line2 < next2 && e.a.ids[line1-1] == e.b.ids[line2-1] {
			line1++
			line2++
		}
		if next1 > line1 || next2 > line2 {
			e.patience(line1, next1-line1, line2, next2-line2)
		}
		if first == patienceEnd {
			return
		}
		for next := entries[first].next; next != patienceEnd &&
			entries[next].line1 == entries[first].line1+1 &&
			entries[next].line2 == entries[first].line2+1; next = entries[first].next {
			first = next
		}
		line1, line2 = entries[first].line1+1, entries[first].line2+1
		first = entries[first].next
	}
}
