package diff

import "math"

type algoEnv struct {
	mxcost   int
	snakeCnt int
	heurMin  int
}

type splitPoint struct {
	i1    int
	i2    int
	minLo bool
	minHi bool
}

type kvector struct {
	values []int
	fbase  int
	bbase  int
}

func (k *kvector) forward(d int) int { return k.values[k.fbase+d] }

func (k *kvector) setForward(d, value int) { k.values[k.fbase+d] = value }

func (k *kvector) backward(d int) int { return k.values[k.bbase+d] }

func (k *kvector) setBackward(d, value int) { k.values[k.bbase+d] = value }

func (e *env) myers() {
	a, b := e.a, e.b
	ndiags := a.nreff + b.nreff + 3
	kvd := &kvector{
		values: make([]int, 2*ndiags+2),
		fbase:  b.nreff + 1,
		bbase:  ndiags + b.nreff + 1,
	}
	xenv := &algoEnv{
		mxcost:   max(bogosqrt(ndiags), maxCostMin),
		snakeCnt: snakeCount,
		heurMin:  heurMinCost,
	}
	e.recsCmp(0, a.nreff, 0, b.nreff, kvd, false, xenv)
}

func (e *env) recsCmp(off1, lim1, off2, lim2 int, kvd *kvector, needMin bool, xenv *algoEnv) {
	ha1, ha2 := e.a.ha, e.b.ha
	for off1 < lim1 && off2 < lim2 && ha1[off1] == ha2[off2] {
		off1++
		off2++
	}
	for off1 < lim1 && off2 < lim2 && ha1[lim1-1] == ha2[lim2-1] {
		lim1--
		lim2--
	}
	switch {
	case off1 == lim1:
		for ; off2 < lim2; off2++ {
			e.b.mark(e.b.rindex[off2])
		}
	case off2 == lim2:
		for ; off1 < lim1; off1++ {
			e.a.mark(e.a.rindex[off1])
		}
	default:
		spl := splitBox(ha1, off1, lim1, ha2, off2, lim2, kvd, needMin, xenv)
		e.recsCmp(off1, spl.i1, off2, spl.i2, kvd, spl.minLo, xenv)
		e.recsCmp(spl.i1, lim1, spl.i2, lim2, kvd, spl.minHi, xenv)
	}
}

func splitBox(ha1 []int, off1, lim1 int, ha2 []int, off2, lim2 int, kvd *kvector, needMin bool, xenv *algoEnv) splitPoint {
	dmin, dmax := off1-lim2, lim1-off2
	fmid, bmid := off1-off2, lim1-lim2
	odd := (fmid-bmid)&1 != 0
	fmin, fmax := fmid, fmid
	bmin, bmax := bmid, bmid

	kvd.setForward(fmid, off1)
	kvd.setBackward(bmid, lim1)

	for ec := 1; ; ec++ {
		gotSnake := false

		if fmin > dmin {
			fmin--
			kvd.setForward(fmin-1, -1)
		} else {
			fmin++
		}
		if fmax < dmax {
			fmax++
			kvd.setForward(fmax+1, -1)
		} else {
			fmax--
		}

		for d := fmax; d >= fmin; d -= 2 {
			var i1 int
			if kvd.forward(d-1) >= kvd.forward(d+1) {
				i1 = kvd.forward(d-1) + 1
			} else {
				i1 = kvd.forward(d + 1)
			}
			prev := i1
			i2 := i1 - d
			for i1 < lim1 && i2 < lim2 && ha1[i1] == ha2[i2] {
				i1++
				i2++
			}
			if i1-prev > xenv.snakeCnt {
				gotSnake = true
			}
			kvd.setForward(d, i1)
			if odd && bmin <= d && d <= bmax && kvd.backward(d) <= i1 {
				return splitPoint{i1: i1, i2: i2, minLo: true, minHi: true}
			}
		}

		if bmin > dmin {
			bmin--
			kvd.setBackward(bmin-1, math.MaxInt)
		} else {
			bmin++
		}
		if bmax < dmax {
			bmax++
			kvd.setBackward(bmax+1, math.MaxInt)
		} else {
			bmax--
		}

		for d := bmax; d >= bmin; d -= 2 {
			var i1 int
			if kvd.backward(d-1) < kvd.backward(d+1) {
				i1 = kvd.backward(d - 1)
			} else {
				i1 = kvd.backward(d+1) - 1
			}
			prev := i1
			i2 := i1 - d
			for i1 > off1 && i2 > off2 && ha1[i1-1] == ha2[i2-1] {
				i1--
				i2--
			}
			if prev-i1 > xenv.snakeCnt {
				gotSnake = true
			}
			kvd.setBackward(d, i1)
			if !odd && fmin <= d && d <= fmax && i1 <= kvd.forward(d) {
				return splitPoint{i1: i1, i2: i2, minLo: true, minHi: true}
			}
		}

		if needMin {
			continue
		}

		if gotSnake && ec > xenv.heurMin {
			if spl, found := heuristicForward(ha1, off1, lim1, ha2, off2, lim2, kvd, fmin, fmax, fmid, ec, xenv); found {
				return spl
			}
			if spl, found := heuristicBackward(ha1, off1, lim1, ha2, off2, lim2, kvd, bmin, bmax, bmid, ec, xenv); found {
				return spl
			}
		}

		if ec >= xenv.mxcost {
			return costlySplit(off1, lim1, off2, lim2, kvd, fmin, fmax, bmin, bmax)
		}
	}
}

func diagonalDistance(d, mid int) int {
	if d < mid {
		return mid - d
	}
	return d - mid
}

func snakeRoomBefore(off1, lim1, off2, lim2, i1, i2, snakeCnt int) bool {
	return i1-off1 >= snakeCnt && i1 < lim1 && i2-off2 >= snakeCnt && i2 < lim2
}

func snakeRoomAfter(off1, lim1, off2, lim2, i1, i2, snakeCnt int) bool {
	return i1 > off1 && lim1-i1 >= snakeCnt && i2 > off2 && lim2-i2 >= snakeCnt
}

func equalBefore(ha1, ha2 []int, i1, i2, count int) bool {
	for k := 1; k <= count; k++ {
		if ha1[i1-k] != ha2[i2-k] {
			return false
		}
	}
	return true
}

func equalAfter(ha1, ha2 []int, i1, i2, count int) bool {
	for k := range count {
		if ha1[i1+k] != ha2[i2+k] {
			return false
		}
	}
	return true
}

func heuristicForward(ha1 []int, off1, lim1 int, ha2 []int, off2, lim2 int, kvd *kvector, fmin, fmax, fmid, ec int, xenv *algoEnv) (splitPoint, bool) {
	best := 0
	spl := splitPoint{minLo: true}
	for d := fmax; d >= fmin; d -= 2 {
		i1 := kvd.forward(d)
		i2 := i1 - d
		v := (i1 - off1) + (i2 - off2) - diagonalDistance(d, fmid)
		if v > kHeuristic*ec && v > best &&
			snakeRoomBefore(off1, lim1, off2, lim2, i1, i2, xenv.snakeCnt) &&
			equalBefore(ha1, ha2, i1, i2, xenv.snakeCnt) {
			best = v
			spl.i1 = i1
			spl.i2 = i2
		}
	}
	return spl, best > 0
}

func heuristicBackward(ha1 []int, off1, lim1 int, ha2 []int, off2, lim2 int, kvd *kvector, bmin, bmax, bmid, ec int, xenv *algoEnv) (splitPoint, bool) {
	best := 0
	spl := splitPoint{minHi: true}
	for d := bmax; d >= bmin; d -= 2 {
		i1 := kvd.backward(d)
		i2 := i1 - d
		v := (lim1 - i1) + (lim2 - i2) - diagonalDistance(d, bmid)
		if v > kHeuristic*ec && v > best &&
			snakeRoomAfter(off1, lim1, off2, lim2, i1, i2, xenv.snakeCnt) &&
			equalAfter(ha1, ha2, i1, i2, xenv.snakeCnt) {
			best = v
			spl.i1 = i1
			spl.i2 = i2
		}
	}
	return spl, best > 0
}

func costlySplit(off1, lim1, off2, lim2 int, kvd *kvector, fmin, fmax, bmin, bmax int) splitPoint {
	fbest, fbest1 := -1, -1
	for d := fmax; d >= fmin; d -= 2 {
		i1 := min(kvd.forward(d), lim1)
		i2 := i1 - d
		if lim2 < i2 {
			i1, i2 = lim2+d, lim2
		}
		if fbest < i1+i2 {
			fbest = i1 + i2
			fbest1 = i1
		}
	}

	bbest, bbest1 := math.MaxInt, math.MaxInt
	for d := bmax; d >= bmin; d -= 2 {
		i1 := max(off1, kvd.backward(d))
		i2 := i1 - d
		if i2 < off2 {
			i1, i2 = off2+d, off2
		}
		if i1+i2 < bbest {
			bbest = i1 + i2
			bbest1 = i1
		}
	}

	if (lim1+lim2)-bbest < fbest-(off1+off2) {
		return splitPoint{i1: fbest1, i2: fbest - fbest1, minLo: true}
	}
	return splitPoint{i1: bbest1, i2: bbest - bbest1, minHi: true}
}
