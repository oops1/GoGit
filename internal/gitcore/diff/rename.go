package diff

import (
	"slices"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

const hashBase = 107927

type spanEntry struct {
	hashval uint32
	count   int
}

func hashChars(data []byte) []spanEntry {
	text := !isBinary(data)
	counts := make(map[uint32]int)
	var accum1, accum2 uint32
	size := 0
	for at := range len(data) {
		c := data[at]
		if text && c == '\r' && at+1 < len(data) && data[at+1] == '\n' {
			continue
		}
		previous := accum1
		accum1 = (accum1 << 7) ^ (accum2 >> 25)
		accum2 = (accum2 << 7) ^ (previous >> 25)
		accum1 += uint32(c)
		size++
		if size < 64 && c != '\n' {
			continue
		}
		counts[(accum1+accum2*0x61)%hashBase] += size
		size = 0
		accum1, accum2 = 0, 0
	}
	if size > 0 {
		counts[(accum1+accum2*0x61)%hashBase] += size
	}
	spans := make([]spanEntry, 0, len(counts))
	for hashval, count := range counts {
		spans = append(spans, spanEntry{hashval: hashval, count: count})
	}
	slices.SortFunc(spans, func(a, b spanEntry) int { return int(a.hashval) - int(b.hashval) })
	return spans
}

func countChanges(srcData, dstData []byte) (copied, added int) {
	src, dst := hashChars(srcData), hashChars(dstData)
	at := 0
	for _, entry := range src {
		for at < len(dst) && dst[at].hashval < entry.hashval {
			added += dst[at].count
			at++
		}
		dstCount := 0
		if at < len(dst) && dst[at].hashval == entry.hashval {
			dstCount = dst[at].count
			at++
		}
		if entry.count < dstCount {
			added += dstCount - entry.count
			copied += entry.count
			continue
		}
		copied += dstCount
	}
	for ; at < len(dst); at++ {
		added += dst[at].count
	}
	return copied, added
}

func estimateSimilarity(src, dst pair, minScore int) int {
	if !src.file.OldMode.IsRegular() || !dst.file.NewMode.IsRegular() {
		return 0
	}
	srcSize, dstSize := len(src.oldData), len(dst.newData)
	maxSize, baseSize := max(srcSize, dstSize), min(srcSize, dstSize)
	if float64(maxSize)*(maxScore-float64(minScore)) < float64(maxSize-baseSize)*maxScore {
		return 0
	}
	if dstSize == 0 {
		return 0
	}
	copied, _ := countChanges(src.oldData, dst.newData)
	return int(float64(copied) * maxScore / float64(maxSize))
}

func basenameSame(srcPath, dstPath string) int {
	srcLen, dstLen := len(srcPath), len(dstPath)
	for srcLen > 0 && dstLen > 0 {
		srcLen--
		dstLen--
		if srcPath[srcLen] != dstPath[dstLen] {
			return 0
		}
		if srcPath[srcLen] == '/' {
			return 1
		}
	}
	if srcLen > 0 && srcPath[srcLen-1] != '/' {
		return 0
	}
	if dstLen > 0 && dstPath[dstLen-1] != '/' {
		return 0
	}
	return 1
}

func baseName(path string) string {
	if slash := strings.LastIndexByte(path, '/'); slash >= 0 {
		return path[slash+1:]
	}
	return path
}

type candidate struct {
	src       int
	dst       int
	score     int
	nameScore int
}

func candidateCompare(a, b candidate) int {
	if a.dst < 0 {
		if b.dst >= 0 {
			return 1
		}
		return 0
	}
	if b.dst < 0 {
		return -1
	}
	if a.score == b.score {
		return b.nameScore - a.nameScore
	}
	return b.score - a.score
}

type renameState struct {
	pairs    []pair
	srcs     []int
	dsts     []int
	used     []int
	matched  []int
	scores   []int
	isRename []bool
	minScore int
	copies   bool
}

func detectRenames(pairs []pair, opts Options) []pair {
	state := &renameState{
		pairs:    pairs,
		used:     make([]int, len(pairs)),
		matched:  make([]int, len(pairs)),
		scores:   make([]int, len(pairs)),
		isRename: make([]bool, len(pairs)),
		minScore: opts.minimumScore(),
		copies:   opts.DetectCopies,
	}
	for at := range pairs {
		switch {
		case pairs[at].file.Status == StatusAdded:
			state.dsts = append(state.dsts, at)
		case pairs[at].file.Status == StatusDeleted:
			state.srcs = append(state.srcs, at)
		case state.copies:
			state.used[at]++
			state.srcs = append(state.srcs, at)
		}
	}
	if len(state.dsts) == 0 || len(state.srcs) == 0 {
		return pairs
	}

	state.exactRenames()
	if state.minScore < int(maxScore) {
		if !state.copies {
			state.cullSources()
			state.basenameMatches(state.minScore + int(0.5*(maxScore-float64(state.minScore))))
			state.cullSources()
		}
		state.inexactRenames(opts.RenameLimit)
	}
	return state.rebuild()
}

func (s *renameState) record(dst, src, score int) {
	s.used[src]++
	s.matched[dst] = src
	s.scores[dst] = score
	s.isRename[dst] = true
}

func (s *renameState) cullSources() {
	s.srcs = slices.DeleteFunc(s.srcs, func(src int) bool { return s.used[src] > 0 })
}

func (s *renameState) exactRenames() {
	byID := make(map[hash.ObjectID][]int, len(s.srcs))
	for _, src := range s.srcs {
		id := s.pairs[src].file.OldID
		byID[id] = append(byID[id], src)
	}
	for _, dst := range s.dsts {
		target := s.pairs[dst].file
		best, bestScore, remaining := -1, -1, 100
		for _, src := range byID[target.NewID] {
			source := s.pairs[src].file
			if !source.OldMode.IsRegular() || !target.NewMode.IsRegular() {
				if source.OldMode != target.NewMode {
					continue
				}
			}
			score := 0
			if s.used[src] == 0 {
				score = 1
			} else if !s.copies {
				continue
			}
			score += basenameSame(source.OldPath, target.NewPath)
			if score > bestScore {
				best, bestScore = src, score
				if score == 2 {
					break
				}
			}
			remaining--
			if remaining == 0 {
				break
			}
		}
		if best >= 0 {
			s.record(dst, best, int(maxScore))
		}
	}
}

func uniqueByBaseName(paths map[string]int, name string, index int) {
	if _, seen := paths[name]; seen {
		paths[name] = -1
		return
	}
	paths[name] = index
}

func (s *renameState) basenameMatches(minBasenameScore int) {
	sources := make(map[string]int, len(s.srcs))
	dests := make(map[string]int, len(s.dsts))
	for _, src := range s.srcs {
		uniqueByBaseName(sources, baseName(s.pairs[src].file.OldPath), src)
	}
	for _, dst := range s.dsts {
		if s.isRename[dst] {
			continue
		}
		uniqueByBaseName(dests, baseName(s.pairs[dst].file.NewPath), dst)
	}
	for _, src := range s.srcs {
		base := baseName(s.pairs[src].file.OldPath)
		dst, listed := dests[base]
		if !listed {
			continue
		}
		if sources[base] == -1 || dst == -1 {
			continue
		}
		score := estimateSimilarity(s.pairs[src], s.pairs[dst], minBasenameScore)
		if score < minBasenameScore {
			continue
		}
		s.record(dst, src, score)
	}
}

func (s *renameState) inexactRenames(limit int) {
	var targets []int
	for _, dst := range s.dsts {
		if !s.isRename[dst] {
			targets = append(targets, dst)
		}
	}
	if len(targets) == 0 || len(s.srcs) == 0 {
		return
	}
	if limit > 0 && len(targets)*len(s.srcs) > limit*limit {
		return
	}

	matrix := make([]candidate, 0, len(targets)*numCandidatePerDst)
	for _, dst := range targets {
		slots := make([]candidate, numCandidatePerDst)
		for at := range slots {
			slots[at] = candidate{dst: -1}
		}
		for _, src := range s.srcs {
			entry := candidate{
				src:       src,
				dst:       dst,
				score:     estimateSimilarity(s.pairs[src], s.pairs[dst], s.minScore),
				nameScore: basenameSame(s.pairs[src].file.OldPath, s.pairs[dst].file.NewPath),
			}
			recordIfBetter(slots, entry)
		}
		matrix = append(matrix, slots...)
	}
	slices.SortStableFunc(matrix, candidateCompare)

	s.takeRenames(matrix, false)
	if s.copies {
		s.takeRenames(matrix, true)
	}
}

func recordIfBetter(slots []candidate, entry candidate) {
	worst := 0
	for at := 1; at < len(slots); at++ {
		if candidateCompare(slots[at], slots[worst]) > 0 {
			worst = at
		}
	}
	if candidateCompare(slots[worst], entry) > 0 {
		slots[worst] = entry
	}
}

func (s *renameState) takeRenames(matrix []candidate, copies bool) {
	for _, entry := range matrix {
		if entry.dst < 0 || entry.score < s.minScore {
			return
		}
		if s.isRename[entry.dst] {
			continue
		}
		if !copies && s.used[entry.src] > 0 {
			continue
		}
		s.record(entry.dst, entry.src, entry.score)
	}
}

func (s *renameState) rebuild() []pair {
	matches := slices.Clone(s.used)
	out := make([]pair, 0, len(s.pairs))
	for at := range s.pairs {
		if s.isRename[at] {
			out = append(out, s.renamed(at))
			continue
		}
		if s.pairs[at].file.Status == StatusDeleted && matches[at] > 0 {
			continue
		}
		out = append(out, s.pairs[at])
	}
	return out
}

func (s *renameState) renamed(dst int) pair {
	src := s.matched[dst]
	result := s.pairs[dst]
	source := s.pairs[src]
	result.oldData = source.oldData
	result.file.OldPath = source.file.OldPath
	result.file.OldMode = source.file.OldMode
	result.file.OldID = source.file.OldID
	result.file.Similarity = int(float64(s.scores[dst]) * 100 / maxScore)
	s.used[src]--
	result.file.Status = StatusRenamed
	if s.used[src] > 0 {
		result.file.Status = StatusCopied
	}
	return result
}
