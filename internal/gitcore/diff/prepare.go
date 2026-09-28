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
	ids    map[string]int
	len1   []int
	len2   []int
	keybuf []byte
}

func newClassifier(hint int) *classifier {
	return &classifier{ids: make(map[string]int, hint)}
}

type LineTable struct {
	cf      classifier
	scratch [2]scratch
	kvalues []int
	changes []change
}

type scratch struct {
	ids    []int
	rchg   []bool
	rindex []int
	ha     []int
	dis    []int8
}

func NewLineTable() *LineTable {
	return &LineTable{cf: classifier{ids: map[string]int{}}}
}

func (t *LineTable) reuse() *classifier {
	clear(t.cf.len1)
	clear(t.cf.len2)
	return &t.cf
}

func grown[T any](kept []T, n int) []T {
	if cap(kept) >= n {
		return kept[:n]
	}
	return make([]T, n, max(n, 2*cap(kept)))
}

func (s *scratch) ints(kept *[]int, n int) []int {
	*kept = grown(*kept, n)
	out := *kept
	clear(out)
	return out
}

func (s *scratch) int8s(n int) []int8 {
	s.dis = grown(s.dis, n)
	out := s.dis
	clear(out)
	return out
}

func (t *LineTable) vector(n int) []int {
	t.kvalues = grown(t.kvalues, n)
	out := t.kvalues
	clear(out)
	return out
}

func (t *LineTable) script() []change {
	return t.changes[:0]
}

func (s *scratch) flags(n int) []bool {
	s.rchg = grown(s.rchg, n)
	out := s.rchg
	clear(out)
	return out
}

func (c *classifier) classify(pass int, key []byte) int {
	id, seen := c.ids[string(key)]
	if !seen {
		return c.count(pass, c.add(string(key)))
	}
	return c.count(pass, id)
}

func (c *classifier) classifyShared(pass int, key string) int {
	id, seen := c.ids[key]
	if !seen {
		return c.count(pass, c.add(key))
	}
	return c.count(pass, id)
}

func (c *classifier) add(key string) int {
	id := len(c.len1)
	c.ids[key] = id
	c.len1 = append(c.len1, 0)
	c.len2 = append(c.len2, 0)
	return id
}

func (c *classifier) count(pass, id int) int {
	if pass == 1 {
		c.len1[id]++
	} else {
		c.len2[id]++
	}
	return id
}

type source struct {
	reuse  *scratch
	recs   Text
	ids    []int
	rchg   []bool
	rindex []int
	ha     []int
	nreff  int
	dstart int
	dend   int
}

func (s *source) count() int { return s.recs.Count() }

func (s *source) record(at int) (text string, newline bool) {
	s.recs = s.recs.shared()
	return lineTextOf(s.recs.line(at))
}

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

func prepareSource(pass int, text Text, cf *classifier, opts Options, reuse *scratch) *source {
	count := text.Count()
	s := &source{
		reuse:  reuse,
		recs:   text,
		ids:    newInts(reuse, func(r *scratch) *[]int { return &r.ids }, count),
		rchg:   newFlags(reuse, count+2),
		dstart: 0,
		dend:   count - 1,
	}
	s.classifyLines(pass, cf, opts)
	if opts.Algorithm.classic() {
		s.rindex = newInts(reuse, func(r *scratch) *[]int { return &r.rindex }, count)
		s.ha = newInts(reuse, func(r *scratch) *[]int { return &r.ha }, count)
	}
	return s
}

func (s *source) classifyLines(pass int, cf *classifier, opts Options) {
	if s.recs.whole != "" && opts.IgnoreWhitespace == 0 {
		for at := range s.count() {
			s.ids[at] = cf.classifyShared(pass, s.recs.line(at))
		}
		return
	}
	for at := range s.count() {
		var key []byte
		key, cf.keybuf = lineKey(s.recs.at(at), cf.keybuf, opts.IgnoreWhitespace)
		s.ids[at] = cf.classify(pass, key)
	}
}

func newInts(reuse *scratch, pick func(*scratch) *[]int, n int) []int {
	if reuse == nil {
		return make([]int, n)
	}
	return reuse.ints(pick(reuse), n)
}

func newFlags(reuse *scratch, n int) []bool {
	if reuse == nil {
		return make([]bool, n)
	}
	return reuse.flags(n)
}

func prepareEnv(textA, textB Text, opts Options) *env {
	var (
		cf             *classifier
		reuseA, reuseB *scratch
	)
	if opts.Lines != nil {
		cf = opts.Lines.reuse()
		reuseA, reuseB = &opts.Lines.scratch[0], &opts.Lines.scratch[1]
	} else {
		cf = newClassifier(textA.Count() + textB.Count())
	}
	if opts.Lines == nil {
		textA, textB = textA.shared(), textB.shared()
	}
	e := &env{
		a:    prepareSource(1, textA, cf, opts, reuseA),
		b:    prepareSource(2, textB, cf, opts, reuseB),
		cf:   cf,
		opts: opts,
	}
	if opts.Algorithm.classic() {
		e.trimEnds()
		e.cleanupRecords()
	}
	return e
}

func (e *env) vectorFor(n int) []int {
	if e.opts.Lines == nil {
		return make([]int, n)
	}
	return e.opts.Lines.vector(n)
}

func (e *env) newScript() []change {
	if e.opts.Lines == nil {
		return nil
	}
	return e.opts.Lines.script()
}

func (e *env) keepScript(changes []change) {
	if e.opts.Lines != nil {
		e.opts.Lines.changes = changes
	}
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

func newInt8s(reuse *scratch, n int) []int8 {
	if reuse == nil {
		return make([]int8, n)
	}
	return reuse.int8s(n)
}

func (e *env) discardMap(s *source, other []int) []int8 {
	dis := newInt8s(s.reuse, s.count()+1)
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
