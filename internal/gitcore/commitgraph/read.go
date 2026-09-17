package commitgraph

import (
	"bytes"
	"crypto/sha1"
	"encoding/binary"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

const (
	ChainFileName = "commit-graph-chain"
	GraphsDir     = "commit-graphs"
	InfoDir       = "info"

	chunkGenerationData     = 0x47444132
	chunkGenerationOverflow = 0x47444f32
	chunkBloomIndexes       = 0x42494458
	chunkBloomData          = 0x42444154
	chunkBase               = 0x42415345

	edgeMask          = 0x7fffffff
	overflowFlag      = 0x80000000
	overflowEntrySize = 8
	dataWidth         = hash.Size + dataTail
	levelShift        = 2
	baseCountOffset   = 7
	chunkCountOffset  = 6
	hashVersionOffset = 5
	versionOffset     = 4
	offsetSize        = 8
	lastFanout        = fanoutEntries - 1
)

var (
	ErrCorrupt       = errors.New("commitgraph: corrupt file")
	ErrChainMismatch = errors.New("commitgraph: chain does not match its layers")
)

type OpenOptions struct {
	SkipGenerationData bool
	SkipChangedPaths   bool
}

type layer struct {
	path        string
	count       uint32
	base        uint32
	fanout      []byte
	oids        []byte
	data        []byte
	edges       []byte
	generations []byte
	overflow    []byte
	bloomIndex  []byte
	bloomData   []byte
	bloom       *bloomSettings
	bases       []byte
	checksum    hash.ObjectID
}

func Open(objectDirs []string, opts OpenOptions) (*Graph, error) {
	var problems []error
	for _, dir := range objectDirs {
		single, err := loadSingle(dir, opts)
		if single != nil {
			return newGraph([]*layer{single}), nil
		}
		problems = appendProblem(problems, err)
		layers, err := loadChain(dir, objectDirs, opts)
		if len(layers) > 0 {
			return newGraph(layers), nil
		}
		problems = appendProblem(problems, err)
	}
	return nil, errors.Join(problems...)
}

func appendProblem(problems []error, err error) []error {
	if err == nil || errors.Is(err, fs.ErrNotExist) {
		return problems
	}
	return append(problems, err)
}

func loadSingle(dir string, opts OpenOptions) (*layer, error) {
	path := filepath.Join(dir, InfoDir, FileName)
	l, err := loadFile(path, opts)
	if err != nil {
		return nil, err
	}
	if err := l.validateEntries(); err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrCorrupt, path, err)
	}
	return l, nil
}

func loadFile(path string, opts OpenOptions) (*layer, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	l, err := parseLayer(data, opts)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrCorrupt, path, err)
	}
	l.path = path
	return l, nil
}

func loadChain(dir string, objectDirs []string, opts OpenOptions) ([]*layer, error) {
	chainPath := filepath.Join(dir, InfoDir, GraphsDir, ChainFileName)
	data, err := os.ReadFile(chainPath)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, fs.ErrNotExist
	}
	if len(data) < hash.HexSize {
		return nil, fmt.Errorf("%w: %s is too small", ErrCorrupt, chainPath)
	}
	var layers []*layer
	ids := make([]hash.ObjectID, 0, len(data)/(hash.HexSize+1))
	for line := range strings.Lines(string(data)) {
		if len(ids) == cap(ids) {
			break
		}
		id, err := hash.Parse(line[:min(len(line), hash.HexSize)])
		if err != nil {
			return layers, fmt.Errorf("%w: %s: %w", ErrCorrupt, chainPath, err)
		}
		ids = append(ids, id)
		next, err := findLayer(id, objectDirs, opts)
		if err != nil {
			return layers, err
		}
		if err := attach(next, layers, ids); err != nil {
			return layers, fmt.Errorf("%w: %s", err, next.path)
		}
		if err := next.validateEntries(); err != nil {
			return layers, fmt.Errorf("%w: %s: %w", ErrCorrupt, next.path, err)
		}
		layers = append(layers, next)
	}
	return layers, nil
}

func findLayer(id hash.ObjectID, objectDirs []string, opts OpenOptions) (*layer, error) {
	name := "graph-" + id.String() + ".graph"
	var problems []error
	for _, dir := range objectDirs {
		l, err := loadFile(filepath.Join(dir, InfoDir, GraphsDir, name), opts)
		if err == nil {
			return l, nil
		}
		problems = append(problems, err)
	}
	return nil, errors.Join(problems...)
}

func attach(next *layer, below []*layer, ids []hash.ObjectID) error {
	depth := len(ids) - 1
	if depth > 0 && next.bases == nil || len(next.bases)/hash.Size < depth {
		return ErrChainMismatch
	}
	for at := range depth {
		if !bytes.Equal(ids[at][:], next.bases[at*hash.Size:(at+1)*hash.Size]) || ids[at] != below[at].checksum {
			return ErrChainMismatch
		}
	}
	if depth > 0 {
		top := below[depth-1]
		next.base = top.base + top.count
	}
	return nil
}

type chunkTable map[uint32][]byte

func parseLayer(data []byte, opts OpenOptions) (*layer, error) {
	minSize := headerSize + 4*tocEntrySize + fanoutEntries*wordSize + hash.Size
	if len(data) < minSize {
		return nil, errors.New("file is too small")
	}
	if binary.BigEndian.Uint32(data) != signature {
		return nil, errors.New("signature does not match")
	}
	if data[versionOffset] != version {
		return nil, fmt.Errorf("version %d is not supported", data[versionOffset])
	}
	if data[hashVersionOffset] != hashVersion {
		return nil, fmt.Errorf("hash version %d is not supported", data[hashVersionOffset])
	}
	body, sum := data[:len(data)-hash.Size], data[len(data)-hash.Size:]
	if digest := sha1.Sum(body); !bytes.Equal(digest[:], sum) {
		return nil, errors.New("checksum does not match")
	}
	chunks, err := readChunks(data)
	if err != nil {
		return nil, err
	}
	l := &layer{}
	copy(l.checksum[:], sum)
	if err := l.readRequired(chunks); err != nil {
		return nil, err
	}
	l.edges, l.bases = chunks[chunkEdges], chunks[chunkBase]
	if generations := chunks[chunkGenerationData]; !opts.SkipGenerationData && generations != nil && uint32(len(generations)/wordSize) == l.count {
		l.generations, l.overflow = generations, chunks[chunkGenerationOverflow]
	}
	if !opts.SkipChangedPaths {
		l.readBloom(chunks)
	}
	if err := l.validateOrder(); err != nil {
		return nil, err
	}
	return l, nil
}

func readChunks(data []byte) (chunkTable, error) {
	count := int(data[chunkCountOffset])
	if len(data) < headerSize+(count+1)*tocEntrySize+fanoutEntries*wordSize+hash.Size {
		return nil, fmt.Errorf("file is too small to hold %d chunks", count)
	}
	chunks := chunkTable{}
	limit := uint64(len(data) - hash.Size)
	for at := range count {
		entry := data[headerSize+at*tocEntrySize:]
		id := binary.BigEndian.Uint32(entry)
		start := binary.BigEndian.Uint64(entry[wordSize:])
		end := binary.BigEndian.Uint64(entry[tocEntrySize+wordSize:])
		switch {
		case id == 0:
			return nil, errors.New("terminating chunk id appears early")
		case end < start || end > limit:
			return nil, fmt.Errorf("chunk %08x has improper offsets", id)
		case chunks[id] != nil:
			return nil, fmt.Errorf("chunk %08x appears twice", id)
		}
		chunks[id] = data[int(start):int(end):int(end)]
	}
	if binary.BigEndian.Uint32(data[headerSize+count*tocEntrySize:]) != 0 {
		return nil, errors.New("final chunk has a non-zero id")
	}
	return chunks, nil
}

func (l *layer) readRequired(chunks chunkTable) error {
	l.fanout = chunks[chunkOIDFanout]
	if len(l.fanout) != fanoutEntries*wordSize {
		return errors.New("fanout chunk is missing or has the wrong size")
	}
	for at := range lastFanout {
		if binary.BigEndian.Uint32(l.fanout[at*wordSize:]) > binary.BigEndian.Uint32(l.fanout[(at+1)*wordSize:]) {
			return errors.New("fanout values are out of order")
		}
	}
	l.count = binary.BigEndian.Uint32(l.fanout[lastFanout*wordSize:])
	l.oids = chunks[chunkOIDLookup]
	if l.oids == nil || uint32(len(l.oids)/hash.Size) != l.count {
		return errors.New("oid lookup chunk is missing or has the wrong size")
	}
	l.data = chunks[chunkData]
	if l.data == nil || uint32(len(l.data)/dataWidth) != l.count {
		return errors.New("commit data chunk is missing or has the wrong size")
	}
	return nil
}

func (l *layer) readBloom(chunks chunkTable) {
	index, data := chunks[chunkBloomIndexes], chunks[chunkBloomData]
	if index == nil || uint32(len(index)/wordSize) != l.count || len(data) < bloomHeaderSize {
		return
	}
	if binary.BigEndian.Uint32(data) != bloomHashVersion {
		return
	}
	l.bloomIndex, l.bloomData = index, data
	l.bloom = &bloomSettings{
		hashVersion:  bloomHashVersion,
		numHashes:    binary.BigEndian.Uint32(data[wordSize:]),
		bitsPerEntry: binary.BigEndian.Uint32(data[2*wordSize:]),
	}
}

func (l *layer) validateOrder() error {
	var previous []byte
	next := 0
	for at := range l.count {
		id := l.oids[at*hash.Size : (at+1)*hash.Size]
		if previous != nil && bytes.Compare(previous, id) >= 0 {
			return errors.New("object ids are out of order")
		}
		previous = id
		for ; next < int(id[0]); next++ {
			if binary.BigEndian.Uint32(l.fanout[next*wordSize:]) != at {
				return errors.New("fanout does not match the object ids")
			}
		}
	}
	for ; next < fanoutEntries; next++ {
		if binary.BigEndian.Uint32(l.fanout[next*wordSize:]) != l.count {
			return errors.New("fanout does not match the object ids")
		}
	}
	return nil
}

func (l *layer) validateEntries() error {
	limit := l.base + l.count
	edgeCount := uint32(len(l.edges) / wordSize)
	for at := range l.count {
		entry := l.data[at*dataWidth+hash.Size:]
		first, second := binary.BigEndian.Uint32(entry), binary.BigEndian.Uint32(entry[wordSize:])
		if first != noParent && first >= limit {
			return fmt.Errorf("parent position %d is out of range", first)
		}
		switch {
		case second == noParent:
		case second&octopusFlag == 0 && second >= limit:
			return fmt.Errorf("parent position %d is out of range", second)
		case second&octopusFlag != 0:
			if err := l.validateEdges(second&edgeMask, edgeCount, limit); err != nil {
				return err
			}
		}
		if l.generations != nil {
			offset := binary.BigEndian.Uint32(l.generations[at*wordSize:])
			if offset&overflowFlag != 0 && uint64(offset&edgeMask) >= uint64(len(l.overflow)/overflowEntrySize) {
				return errors.New("generation overflow is out of range")
			}
		}
	}
	return nil
}

func (l *layer) validateEdges(start, edgeCount, limit uint32) error {
	for at := start; ; at++ {
		if at >= edgeCount {
			return errors.New("extra edge is out of range")
		}
		value := binary.BigEndian.Uint32(l.edges[at*wordSize:])
		if value&edgeMask >= limit {
			return fmt.Errorf("parent position %d is out of range", value&edgeMask)
		}
		if value&octopusFlag != 0 {
			return nil
		}
	}
}
