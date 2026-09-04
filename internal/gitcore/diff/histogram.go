package diff

type histRecord struct {
	ptr int
	cnt int
}

type region struct {
	begin1 int
	end1   int
	begin2 int
	end2   int
}

type histSpace struct {
	slot    []int32
	stamp   []int32
	round   int32
	records []histRecord
	lineMap []int32
	nextPtr []int
}

func newHistSpace(ids, lines int) *histSpace {
	return &histSpace{
		slot:    make([]int32, ids),
		stamp:   make([]int32, ids),
		lineMap: make([]int32, lines+1),
		nextPtr: make([]int, lines+1),
	}
}

func (s *histSpace) reset() {
	s.round++
	s.records = s.records[:0]
}

func (s *histSpace) lookup(id int) (int32, bool) {
	if s.stamp[id] != s.round {
		return 0, false
	}
	return s.slot[id], true
}

func (s *histSpace) remember(id int, at int32) {
	s.stamp[id] = s.round
	s.slot[id] = at
}

func (s *histSpace) record(at int32) *histRecord {
	return &s.records[at]
}

func (s *histSpace) recordAtLine(ptr int) *histRecord {
	return &s.records[s.lineMap[ptr]]
}

type histIndex struct {
	space     *histSpace
	maxChain  int
	cnt       int
	hasCommon bool
}

func (e *env) histSpace() *histSpace {
	if e.hist == nil {
		e.hist = newHistSpace(len(e.cf.len1), e.a.count())
	}
	return e.hist
}

func (e *env) histogram(line1, count1, line2, count2 int) {
	for {
		if count1 <= 0 && count2 <= 0 {
			return
		}
		if count1 <= 0 {
			for ; count2 > 0; count2-- {
				e.b.mark(line2 - 1)
				line2++
			}
			return
		}
		if count2 <= 0 {
			for ; count1 > 0; count1-- {
				e.a.mark(line1 - 1)
				line1++
			}
			return
		}
		lcs, degraded := e.findLCS(line1, count1, line2, count2)
		if degraded {
			e.fallBack(line1, count1, line2, count2)
			return
		}
		if lcs.begin1 == 0 && lcs.begin2 == 0 {
			for ; count1 > 0; count1-- {
				e.a.mark(line1 - 1)
				line1++
			}
			for ; count2 > 0; count2-- {
				e.b.mark(line2 - 1)
				line2++
			}
			return
		}
		e.histogram(line1, lcs.begin1-line1, line2, lcs.begin2-line2)
		end1, end2 := line1+count1-1, line2+count2-1
		line1, count1 = lcs.end1+1, end1-lcs.end1
		line2, count2 = lcs.end2+1, end2-lcs.end2
	}
}

func (e *env) findLCS(line1, count1, line2, count2 int) (region, bool) {
	space := e.histSpace()
	space.reset()
	index := &histIndex{space: space, maxChain: histChainLimit, cnt: histChainLimit + 1}
	index.scanA(e, line1, count1)
	var lcs region
	for at := line2; at <= line2+count2-1; {
		at = index.tryLCS(e, &lcs, at, line1, count1, line2, count2)
	}
	return lcs, index.hasCommon && index.maxChain < index.cnt
}

func (x *histIndex) scanA(e *env, line1, count1 int) {
	space := x.space
	clear(space.nextPtr[line1 : line1+count1])
	for ptr := line1 + count1 - 1; ptr >= line1; ptr-- {
		id := e.a.ids[ptr-1]
		at, seen := space.lookup(id)
		if seen {
			rec := space.record(at)
			space.nextPtr[ptr] = rec.ptr
			rec.ptr = ptr
			rec.cnt++
			space.lineMap[ptr] = at
			continue
		}
		at = int32(len(space.records))
		space.records = append(space.records, histRecord{ptr: ptr, cnt: 1})
		space.remember(id, at)
		space.lineMap[ptr] = at
	}
}

func (x *histIndex) tryLCS(e *env, lcs *region, bPtr, line1, count1, line2, count2 int) int {
	space := x.space
	bNext := bPtr + 1
	at, seen := space.lookup(e.b.ids[bPtr-1])
	if !seen {
		return bNext
	}
	rec := space.record(at)
	x.hasCommon = true
	if rec.cnt > x.cnt {
		return bNext
	}

	end1, end2 := line1+count1-1, line2+count2-1
	as := rec.ptr
	for {
		next := space.nextPtr[as]
		bs, ae, be := bPtr, as, bPtr
		rc := rec.cnt

		for line1 < as && line2 < bs && e.a.ids[as-2] == e.b.ids[bs-2] {
			as--
			bs--
			if rc > 1 {
				rc = min(rc, space.recordAtLine(as).cnt)
			}
		}
		for ae < end1 && be < end2 && e.a.ids[ae] == e.b.ids[be] {
			ae++
			be++
			if rc > 1 {
				rc = min(rc, space.recordAtLine(ae).cnt)
			}
		}

		if bNext <= be {
			bNext = be + 1
		}
		if lcs.end1-lcs.begin1 < ae-as || rc < x.cnt {
			lcs.begin1, lcs.begin2 = as, bs
			lcs.end1, lcs.end2 = ae, be
			x.cnt = rc
		}

		if next == 0 {
			return bNext
		}
		for next <= ae {
			next = space.nextPtr[next]
			if next == 0 {
				return bNext
			}
		}
		as = next
	}
}

func (e *env) fallBack(line1, count1, line2, count2 int) {
	opts := e.opts
	opts.Algorithm = AlgorithmMyers
	sub := prepareEnv(e.a.recs[line1-1:line1-1+count1], e.b.recs[line2-1:line2-1+count2], opts)
	sub.myers()
	for at := range count1 {
		if sub.a.changed(at) {
			e.a.mark(line1 - 1 + at)
		}
	}
	for at := range count2 {
		if sub.b.changed(at) {
			e.b.mark(line2 - 1 + at)
		}
	}
}
