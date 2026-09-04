package diff

const (
	kpdisRun       = 4
	maxEqLimit     = 1024
	simScanWindow  = 100
	maxCostMin     = 256
	heurMinCost    = 256
	snakeCount     = 20
	kHeuristic     = 4
	histChainLimit = 64
)

type classifier struct {
	ids  map[string]int
	len1 []int
	len2 []int
}

func newClassifier(hint int) *classifier {
	return &classifier{ids: make(map[string]int, hint)}
}

func (c *classifier) classify(pass int, key string) int {
	id, seen := c.ids[key]
	if !seen {
		id = len(c.len1)
		c.ids[key] = id
		c.len1 = append(c.len1, 0)
		c.len2 = append(c.len2, 0)
	}
	if pass == 1 {
		c.len1[id]++
	} else {
		c.len2[id]++
	}
	return id
}

type source struct {
	recs   []string
	ids    []int
	rchg   []bool
	rindex []int
	ha     []int
	nreff  int
	dstart int
	dend   int
}

func (s *source) count() int { return len(s.recs) }

func (s *source) changed(at int) bool { return s.rchg[at+1] }

func (s *source) mark(at int) { s.rchg[at+1] = true }

func (s *source) unmark(at int) { s.rchg[at+1] = false }

type env struct {
	a    *source
	b    *source
	cf   *classifier
	opts Options
	hist *histSpace
}

func prepareSource(pass int, lines []string, cf *classifier, opts Options) *source {
	s := &source{
		recs:   lines,
		ids:    make([]int, len(lines)),
		rchg:   make([]bool, len(lines)+2),
		dstart: 0,
		dend:   len(lines) - 1,
	}
	for at, record := range lines {
		s.ids[at] = cf.classify(pass, lineKey(record, opts.IgnoreWhitespace))
	}
	if opts.Algorithm != AlgorithmHistogram {
		s.rindex = make([]int, len(lines))
		s.ha = make([]int, len(lines))
	}
	return s
}

func prepareEnv(linesA, linesB []string, opts Options) *env {
	cf := newClassifier(len(linesA) + len(linesB))
	e := &env{
		a:    prepareSource(1, linesA, cf, opts),
		b:    prepareSource(2, linesB, cf, opts),
		cf:   cf,
		opts: opts,
	}
	if opts.Algorithm != AlgorithmHistogram {
		e.trimEnds()
		e.cleanupRecords()
	}
	return e
}

func (e *env) trimEnds() {
	a, b := e.a, e.b
	limit := min(a.count(), b.count())
	at := 0
	for ; at < limit; at++ {
		if a.ids[at] != b.ids[at] {
			break
		}
	}
	a.dstart, b.dstart = at, at

	limit -= at
	at = 0
	for ; at < limit; at++ {
		if a.ids[a.count()-at-1] != b.ids[b.count()-at-1] {
			break
		}
	}
	a.dend = a.count() - at - 1
	b.dend = b.count() - at - 1
}

func bogosqrt(n int) int {
	root := 1
	for ; n > 0; n >>= 2 {
		root <<= 1
	}
	return root
}

func (e *env) cleanupRecords() {
	dis1 := e.discardMap(e.a, e.cf.len2)
	dis2 := e.discardMap(e.b, e.cf.len1)
	e.a.nreff = reduce(e.a, dis1)
	e.b.nreff = reduce(e.b, dis2)
}

func (e *env) discardMap(s *source, other []int) []int8 {
	dis := make([]int8, s.count()+1)
	limit := min(bogosqrt(s.count()), maxEqLimit)
	for at := s.dstart; at <= s.dend; at++ {
		matches := other[s.ids[at]]
		switch {
		case matches == 0:
			dis[at] = 0
		case matches >= limit:
			dis[at] = 2
		default:
			dis[at] = 1
		}
	}
	return dis
}

func reduce(s *source, dis []int8) int {
	nreff := 0
	for at := s.dstart; at <= s.dend; at++ {
		if dis[at] == 1 || (dis[at] == 2 && !cleanMultiMatch(dis, at, s.dstart, s.dend)) {
			s.rindex[nreff] = at
			s.ha[nreff] = s.ids[at]
			nreff++
			continue
		}
		s.mark(at)
	}
	return nreff
}

func runCounts(dis []int8, at, start, end, stride int) (nomatch, multi int) {
	multi = 1
	for step := 1; ; step++ {
		pos := at + step*stride
		if pos < start || pos > end {
			return nomatch, multi
		}
		switch dis[pos] {
		case 0:
			nomatch++
		case 2:
			multi++
		default:
			return nomatch, multi
		}
	}
}

func cleanMultiMatch(dis []int8, at, start, end int) bool {
	if at-start > simScanWindow {
		start = at - simScanWindow
	}
	if end-at > simScanWindow {
		end = at + simScanWindow
	}
	nomatchBefore, multiBefore := runCounts(dis, at, start, end, -1)
	if nomatchBefore == 0 {
		return false
	}
	nomatch, multi := runCounts(dis, at, start, end, 1)
	if nomatch == 0 {
		return false
	}
	nomatch += nomatchBefore
	multi += multiBefore
	return multi*kpdisRun < multi+nomatch
}
