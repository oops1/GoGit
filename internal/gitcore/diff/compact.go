package diff

const (
	maxBlanks                       = 20
	startOfFilePenalty              = 1
	endOfFilePenalty                = 21
	totalBlankWeight                = -30
	postBlankWeight                 = 6
	relativeIndentPenalty           = -4
	relativeIndentWithBlankPenalty  = 10
	relativeOutdentPenalty          = 24
	relativeOutdentWithBlankPenalty = 17
	relativeDedentPenalty           = 23
	relativeDedentWithBlankPenalty  = 17
	indentWeight                    = 60
	indentHeuristicMaxSliding       = 100
)

type group struct {
	start int
	end   int
}

type splitMeasurement struct {
	endOfFile  bool
	indent     int
	preBlank   int
	preIndent  int
	postBlank  int
	postIndent int
}

type splitScore struct {
	effectiveIndent int
	penalty         int
}

func groupInit(s *source) group {
	g := group{}
	for s.changed(g.end) {
		g.end++
	}
	return g
}

func groupNext(s *source, g *group) bool {
	if g.end == s.count() {
		return false
	}
	g.start = g.end + 1
	for g.end = g.start; s.changed(g.end); g.end++ {
	}
	return true
}

func groupPrevious(s *source, g *group) bool {
	if g.start == 0 {
		return false
	}
	g.end = g.start - 1
	for g.start = g.end; s.changed(g.start - 1); g.start-- {
	}
	return true
}

func groupSlideDown(s *source, g *group) bool {
	if g.end >= s.count() || s.ids[g.start] != s.ids[g.end] {
		return false
	}
	s.unmark(g.start)
	g.start++
	s.mark(g.end)
	g.end++
	for s.changed(g.end) {
		g.end++
	}
	return true
}

func groupSlideUp(s *source, g *group) bool {
	if g.start <= 0 || s.ids[g.start-1] != s.ids[g.end-1] {
		return false
	}
	g.start--
	s.mark(g.start)
	g.end--
	s.unmark(g.end)
	for s.changed(g.start - 1) {
		g.start--
	}
	return true
}

func measureSplit(s *source, split int) splitMeasurement {
	m := splitMeasurement{preIndent: -1, postIndent: -1}
	if split >= s.count() {
		m.endOfFile = true
		m.indent = -1
	} else {
		m.indent = lineIndent(s.recs[split])
	}
	for at := split - 1; at >= 0; at-- {
		m.preIndent = lineIndent(s.recs[at])
		if m.preIndent != -1 {
			break
		}
		m.preBlank++
		if m.preBlank == maxBlanks {
			m.preIndent = 0
			break
		}
	}
	for at := split + 1; at < s.count(); at++ {
		m.postIndent = lineIndent(s.recs[at])
		if m.postIndent != -1 {
			break
		}
		m.postBlank++
		if m.postBlank == maxBlanks {
			m.postIndent = 0
			break
		}
	}
	return m
}

func scoreAddSplit(m splitMeasurement, s *splitScore) {
	if m.preIndent == -1 && m.preBlank == 0 {
		s.penalty += startOfFilePenalty
	}
	if m.endOfFile {
		s.penalty += endOfFilePenalty
	}

	postBlank := 0
	if m.indent == -1 {
		postBlank = 1 + m.postBlank
	}
	totalBlank := m.preBlank + postBlank
	s.penalty += totalBlankWeight * totalBlank
	s.penalty += postBlankWeight * postBlank

	indent := m.indent
	if indent == -1 {
		indent = m.postIndent
	}
	anyBlanks := totalBlank != 0
	s.effectiveIndent += indent

	switch {
	case indent == -1 || m.preIndent == -1 || indent == m.preIndent:
	case indent > m.preIndent:
		if anyBlanks {
			s.penalty += relativeIndentWithBlankPenalty
		} else {
			s.penalty += relativeIndentPenalty
		}
	case m.postIndent != -1 && m.postIndent > indent:
		if anyBlanks {
			s.penalty += relativeOutdentWithBlankPenalty
		} else {
			s.penalty += relativeOutdentPenalty
		}
	default:
		if anyBlanks {
			s.penalty += relativeDedentWithBlankPenalty
		} else {
			s.penalty += relativeDedentPenalty
		}
	}
}

func scoreCmp(a, b splitScore) int {
	indents := 0
	switch {
	case a.effectiveIndent > b.effectiveIndent:
		indents = 1
	case a.effectiveIndent < b.effectiveIndent:
		indents = -1
	}
	return indentWeight*indents + (a.penalty - b.penalty)
}

func compact(s, other *source, indentHeuristic bool) {
	g := groupInit(s)
	og := groupInit(other)

	for {
		if g.end != g.start {
			var groupsize, earliestEnd, endMatchingOther int
			for {
				groupsize = g.end - g.start
				endMatchingOther = -1

				for groupSlideUp(s, &g) {
					groupPrevious(other, &og)
				}
				earliestEnd = g.end
				if og.end > og.start {
					endMatchingOther = g.end
				}
				for groupSlideDown(s, &g) {
					groupNext(other, &og)
					if og.end > og.start {
						endMatchingOther = g.end
					}
				}
				if groupsize == g.end-g.start {
					break
				}
			}

			switch {
			case g.end == earliestEnd:
			case endMatchingOther != -1:
				for og.end == og.start {
					groupSlideUp(s, &g)
					groupPrevious(other, &og)
				}
			case indentHeuristic:
				slideToBestShift(s, other, &g, &og, groupsize, earliestEnd)
			}
		}

		if !groupNext(s, &g) {
			return
		}
		groupNext(other, &og)
	}
}

func slideToBestShift(s, other *source, g, og *group, groupsize, earliestEnd int) {
	shift := earliestEnd
	shift = max(shift, g.end-groupsize-1, g.end-indentHeuristicMaxSliding)

	bestShift := -1
	var bestScore splitScore
	for ; shift <= g.end; shift++ {
		var score splitScore
		scoreAddSplit(measureSplit(s, shift), &score)
		scoreAddSplit(measureSplit(s, shift-groupsize), &score)
		if bestShift == -1 || scoreCmp(score, bestScore) <= 0 {
			bestScore = score
			bestShift = shift
		}
	}
	for g.end > bestShift {
		groupSlideUp(s, g)
		groupPrevious(other, og)
	}
}
