package commitgraph

import (
	"bytes"
	"encoding/binary"
	"math"
	"sort"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

const (
	GenerationInfinity uint64 = math.MaxUint64
	timeLowBits               = 32
)

type Position uint32

type Entry struct {
	Tree       hash.ObjectID
	Parents    []Position
	Time       int64
	Generation uint64
}

type Graph struct {
	layers    []*layer
	total     uint32
	corrected bool
	bloom     *bloomSettings
}

func newGraph(layers []*layer) *Graph {
	g := &Graph{layers: layers, corrected: true}
	for _, l := range layers {
		g.total += l.count
		g.corrected = g.corrected && l.generations != nil
		if l.bloom != nil {
			g.bloom = l.bloom
		}
	}
	return g
}

func (g *Graph) Len() int { return int(g.total) }

func (g *Graph) Layers() int { return len(g.layers) }

func (g *Graph) CorrectedDates() bool { return g.corrected }

func (g *Graph) ChangedPaths() bool { return g.bloom != nil }

func (g *Graph) Lookup(id hash.ObjectID) (Position, bool) {
	for at := len(g.layers) - 1; at >= 0; at-- {
		l := g.layers[at]
		if lex, ok := l.search(id); ok {
			return Position(l.base + lex), true
		}
	}
	return 0, false
}

func (l *layer) search(id hash.ObjectID) (uint32, bool) {
	lo := uint32(0)
	if id[0] > 0 {
		lo = binary.BigEndian.Uint32(l.fanout[(int(id[0])-1)*wordSize:])
	}
	hi := binary.BigEndian.Uint32(l.fanout[int(id[0])*wordSize:])
	span := int(hi - lo)
	found := sort.Search(span, func(at int) bool {
		start := (int(lo) + at) * hash.Size
		return bytes.Compare(l.oids[start:start+hash.Size], id[:]) >= 0
	})
	lex := lo + uint32(found)
	if found == span || !bytes.Equal(l.oids[lex*hash.Size:(lex+1)*hash.Size], id[:]) {
		return 0, false
	}
	return lex, true
}

func (g *Graph) layerOf(p Position) (*layer, uint32) {
	at := len(g.layers) - 1
	for uint32(p) < g.layers[at].base {
		at--
	}
	l := g.layers[at]
	return l, uint32(p) - l.base
}

func (g *Graph) ID(p Position) hash.ObjectID {
	l, lex := g.layerOf(p)
	var id hash.ObjectID
	copy(id[:], l.oids[lex*hash.Size:])
	return id
}

func (g *Graph) Entry(p Position) Entry {
	l, lex := g.layerOf(p)
	record := l.data[lex*dataWidth : (lex+1)*dataWidth]
	var entry Entry
	copy(entry.Tree[:], record)
	entry.Parents = l.parents(record)
	entry.Time, entry.Generation = g.timeAndGeneration(l, lex)
	return entry
}

func (g *Graph) Generation(p Position) uint64 {
	l, lex := g.layerOf(p)
	_, generation := g.timeAndGeneration(l, lex)
	return generation
}

func (g *Graph) timeAndGeneration(l *layer, lex uint32) (int64, uint64) {
	record := l.data[lex*dataWidth+hash.Size+2*wordSize:]
	levelWord := binary.BigEndian.Uint32(record)
	when := int64(uint64(levelWord&timeHighMask)<<timeLowBits | uint64(binary.BigEndian.Uint32(record[wordSize:])))
	return when, g.generation(l, lex, when, levelWord)
}

func (l *layer) parents(record []byte) []Position {
	first := binary.BigEndian.Uint32(record[hash.Size:])
	if first == noParent {
		return nil
	}
	second := binary.BigEndian.Uint32(record[hash.Size+wordSize:])
	switch {
	case second == noParent:
		return []Position{Position(first)}
	case second&octopusFlag == 0:
		return []Position{Position(first), Position(second)}
	}
	parents := []Position{Position(first)}
	for at := second & edgeMask; ; at++ {
		value := binary.BigEndian.Uint32(l.edges[at*wordSize:])
		parents = append(parents, Position(value&edgeMask))
		if value&octopusFlag != 0 {
			return parents
		}
	}
}

func (g *Graph) generation(l *layer, lex uint32, when int64, levelWord uint32) uint64 {
	value := uint64(levelWord >> levelShift)
	if g.corrected {
		offset := binary.BigEndian.Uint32(l.generations[lex*wordSize:])
		value = uint64(when) + uint64(offset)
		if offset&overflowFlag != 0 {
			value = uint64(when) + binary.BigEndian.Uint64(l.overflow[(offset&edgeMask)*overflowEntrySize:])
		}
	}
	if value == 0 {
		return GenerationInfinity
	}
	return value
}

func (g *Graph) BloomKeys(path string) []BloomKey {
	if g.bloom == nil {
		return nil
	}
	return pathKeys(path, g.bloom.numHashes)
}

func (g *Graph) MaybeChanged(p Position, keys []BloomKey) bool {
	if len(keys) == 0 {
		return true
	}
	l, lex := g.layerOf(p)
	filter, ok := l.filter(lex)
	if _, generation := g.timeAndGeneration(l, lex); !ok || generation == GenerationInfinity {
		return true
	}
	for _, key := range keys {
		if !filterContains(filter, key) {
			return false
		}
	}
	return true
}

func (l *layer) filter(lex uint32) ([]byte, bool) {
	if l.bloom == nil {
		return nil, false
	}
	limit := uint32(len(l.bloomData) - bloomHeaderSize)
	end := binary.BigEndian.Uint32(l.bloomIndex[lex*wordSize:])
	start := uint32(0)
	if lex > 0 {
		start = binary.BigEndian.Uint32(l.bloomIndex[(lex-1)*wordSize:])
	}
	if end > limit || start > limit || end <= start {
		return nil, false
	}
	return l.bloomData[bloomHeaderSize+start : bloomHeaderSize+end], true
}
