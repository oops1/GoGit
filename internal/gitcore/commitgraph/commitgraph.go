package commitgraph

import (
	"crypto/sha1"
	"encoding/binary"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

const (
	FileName = "commit-graph"

	signature   = 0x43475048
	version     = 1
	hashVersion = 1

	chunkOIDFanout = 0x4f494446
	chunkOIDLookup = 0x4f49444c
	chunkData      = 0x43444154
	chunkEdges     = 0x45444745

	headerSize    = 8
	tocEntrySize  = 12
	fanoutEntries = 256
	wordSize      = 4
	dataTail      = 16

	noParent          = 0x70000000
	octopusFlag       = 0x80000000
	maxGeneration     = 0x3FFFFFFF
	maxOffset         = 0x7FFFFFFF
	timeHighMask      = 0x3
	maxLevelBeforeCap = maxGeneration - 1

	lockSuffix = ".lock"
	dirPerm    = 0o755
	filePerm   = 0o644
)

var (
	ErrUnsupportedFormat = errors.New("commitgraph: unsupported object format")
	ErrDuplicateCommit   = errors.New("commitgraph: commit listed twice")
	ErrMissingParent     = errors.New("commitgraph: parent is not in the graph")
	ErrCycle             = errors.New("commitgraph: commits form a cycle")
	ErrLocked            = errors.New("commitgraph: file is locked by another writer")
)

var (
	fsMkdirAll = os.MkdirAll
	fsOpenRoot = os.OpenRoot
	fsCreate   = func(root *os.Root, name string) (*os.File, error) {
		return root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, filePerm)
	}
	fsWrite  = func(file *os.File, data []byte) (int, error) { return file.Write(data) }
	fsClose  = func(file *os.File) error { return file.Close() }
	fsChmod  = func(root *os.Root, name string, mode fs.FileMode) error { return root.Chmod(name, mode) }
	fsRename = func(root *os.Root, from, to string) error { return root.Rename(from, to) }
	fsRemove = func(root *os.Root, name string) error { return root.Remove(name) }
)

type Commit struct {
	ID      hash.ObjectID
	Tree    hash.ObjectID
	Parents []hash.ObjectID
	Time    int64
	Changed *ChangedPaths
}

type EncodeOptions struct {
	TopologicalLevelsOnly bool
	ChangedPaths          bool
}

type chunk struct {
	id   uint32
	body []byte
}

type generation struct {
	level     uint32
	corrected uint64
}

func Encode(format hash.Format, commits []Commit, opts EncodeOptions) ([]byte, error) {
	if format != hash.SHA1 {
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedFormat, format)
	}
	sorted := slices.Clone(commits)
	slices.SortFunc(sorted, func(a, b Commit) int { return a.ID.Compare(b.ID) })
	positions := make(map[hash.ObjectID]uint32, len(sorted))
	for i, commit := range sorted {
		if _, seen := positions[commit.ID]; seen {
			return nil, fmt.Errorf("%w: %s", ErrDuplicateCommit, commit.ID)
		}
		positions[commit.ID] = uint32(i)
	}
	generations, err := computeGenerations(sorted, positions)
	if err != nil {
		return nil, err
	}
	data, edges := commitData(sorted, positions, generations)
	chunks := []chunk{
		{id: chunkOIDFanout, body: fanout(sorted)},
		{id: chunkOIDLookup, body: lookup(sorted)},
		{id: chunkData, body: data},
	}
	if !opts.TopologicalLevelsOnly {
		offsets, overflow := generationData(sorted, generations)
		chunks = append(chunks, chunk{id: chunkGenerationData, body: offsets})
		if len(overflow) > 0 {
			chunks = append(chunks, chunk{id: chunkGenerationOverflow, body: overflow})
		}
	}
	if len(edges) > 0 {
		chunks = append(chunks, chunk{id: chunkEdges, body: edges})
	}
	if opts.ChangedPaths {
		filters := make([][]byte, len(sorted))
		for at, commit := range sorted {
			filters[at] = buildFilter(commit.Changed)
		}
		index, bloom := bloomChunks(filters)
		chunks = append(chunks, chunk{id: chunkBloomIndexes, body: index}, chunk{id: chunkBloomData, body: bloom})
	}
	return assemble(chunks), nil
}

func computeGenerations(commits []Commit, positions map[hash.ObjectID]uint32) ([]generation, error) {
	generations := make([]generation, len(commits))
	onStack := make([]bool, len(commits))
	for start := range commits {
		if generations[start].level != 0 {
			continue
		}
		stack := []int{start}
		onStack[start] = true
		for len(stack) > 0 {
			top := stack[len(stack)-1]
			computed, pending, err := generationOf(commits[top], positions, generations)
			if err != nil {
				return nil, err
			}
			if pending >= 0 {
				if onStack[pending] {
					return nil, fmt.Errorf("%w: %s", ErrCycle, commits[pending].ID)
				}
				onStack[pending] = true
				stack = append(stack, pending)
				continue
			}
			generations[top] = computed
			onStack[top] = false
			stack = stack[:len(stack)-1]
		}
	}
	return generations, nil
}

func generationOf(commit Commit, positions map[hash.ObjectID]uint32, generations []generation) (generation, int, error) {
	highestLevel := uint32(0)
	highestDate := uint32(0)
	for _, parent := range commit.Parents {
		position, ok := positions[parent]
		if !ok {
			return generation{}, 0, fmt.Errorf("%w: %s of %s", ErrMissingParent, parent, commit.ID)
		}
		known := generations[position]
		if known.level == 0 {
			return generation{}, int(position), nil
		}
		highestLevel = max(highestLevel, known.level)
		if known.corrected > uint64(highestDate) {
			highestDate = uint32(known.corrected)
		}
	}
	corrected := uint64(highestDate)
	if when := uint64(max(commit.Time, 0)); when != 0 && when > corrected {
		corrected = when - 1
	}
	return generation{level: min(highestLevel, maxLevelBeforeCap) + 1, corrected: corrected + 1}, -1, nil
}

func commitData(commits []Commit, positions map[hash.ObjectID]uint32, generations []generation) ([]byte, []byte) {
	data := make([]byte, 0, len(commits)*(hash.Size+dataTail))
	var edges []byte
	for i, commit := range commits {
		data = append(data, commit.Tree[:]...)
		first, second := uint32(noParent), uint32(noParent)
		switch {
		case len(commit.Parents) > 2:
			first = positions[commit.Parents[0]]
			second = octopusFlag | uint32(len(edges)/wordSize)
			last := len(commit.Parents) - 2
			for j, parent := range commit.Parents[1:] {
				value := positions[parent]
				if j == last {
					value |= octopusFlag
				}
				edges = binary.BigEndian.AppendUint32(edges, value)
			}
		case len(commit.Parents) == 2:
			first, second = positions[commit.Parents[0]], positions[commit.Parents[1]]
		case len(commit.Parents) == 1:
			first = positions[commit.Parents[0]]
		}
		data = binary.BigEndian.AppendUint32(data, first)
		data = binary.BigEndian.AppendUint32(data, second)
		when := uint64(max(commit.Time, 0))
		data = binary.BigEndian.AppendUint32(data, generations[i].level<<levelShift|uint32(when>>timeLowBits)&timeHighMask)
		data = binary.BigEndian.AppendUint32(data, uint32(when))
	}
	return data, edges
}

func generationData(commits []Commit, generations []generation) ([]byte, []byte) {
	offsets := make([]byte, 0, len(commits)*wordSize)
	var overflow []byte
	for i, commit := range commits {
		offset := generations[i].corrected - uint64(max(commit.Time, 0))
		if offset > maxOffset {
			offsets = binary.BigEndian.AppendUint32(offsets, overflowFlag|uint32(len(overflow)/overflowEntrySize))
			overflow = binary.BigEndian.AppendUint64(overflow, offset)
			continue
		}
		offsets = binary.BigEndian.AppendUint32(offsets, uint32(offset))
	}
	return offsets, overflow
}

func fanout(commits []Commit) []byte {
	out := make([]byte, 0, fanoutEntries*wordSize)
	next := 0
	for first := range fanoutEntries {
		for next < len(commits) && int(commits[next].ID[0]) <= first {
			next++
		}
		out = binary.BigEndian.AppendUint32(out, uint32(next))
	}
	return out
}

func lookup(commits []Commit) []byte {
	out := make([]byte, 0, len(commits)*hash.Size)
	for _, commit := range commits {
		out = append(out, commit.ID[:]...)
	}
	return out
}

func assemble(chunks []chunk) []byte {
	offset := uint64(headerSize + (len(chunks)+1)*tocEntrySize)
	out := binary.BigEndian.AppendUint32(nil, signature)
	out = append(out, version, hashVersion, byte(len(chunks)), 0)
	for _, c := range chunks {
		out = binary.BigEndian.AppendUint32(out, c.id)
		out = binary.BigEndian.AppendUint64(out, offset)
		offset += uint64(len(c.body))
	}
	out = binary.BigEndian.AppendUint32(out, 0)
	out = binary.BigEndian.AppendUint64(out, offset)
	for _, c := range chunks {
		out = append(out, c.body...)
	}
	sum := sha1.Sum(out)
	return append(out, sum[:]...)
}

func WriteFile(infoDir string, format hash.Format, commits []Commit, opts EncodeOptions) error {
	data, err := Encode(format, commits, opts)
	if err != nil {
		return err
	}
	if err := fsMkdirAll(infoDir, dirPerm); err != nil {
		return fmt.Errorf("commitgraph: create %s: %w", infoDir, err)
	}
	root, err := fsOpenRoot(infoDir)
	if err != nil {
		return fmt.Errorf("commitgraph: open %s: %w", infoDir, err)
	}
	defer root.Close()
	lock := FileName + lockSuffix
	file, err := fsCreate(root, lock)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("%w: %s", ErrLocked, filepath.Join(infoDir, lock))
		}
		return fmt.Errorf("commitgraph: create %s: %w", filepath.Join(infoDir, lock), err)
	}
	if _, err := fsWrite(file, data); err != nil {
		_ = fsClose(file)
		_ = fsRemove(root, lock)
		return fmt.Errorf("commitgraph: write %s: %w", filepath.Join(infoDir, lock), err)
	}
	if err := fsClose(file); err != nil {
		_ = fsRemove(root, lock)
		return fmt.Errorf("commitgraph: close %s: %w", filepath.Join(infoDir, lock), err)
	}
	_ = fsChmod(root, FileName, filePerm)
	if err := fsRename(root, lock, FileName); err != nil {
		_ = fsRemove(root, lock)
		return fmt.Errorf("commitgraph: replace %s: %w", filepath.Join(infoDir, FileName), err)
	}
	return nil
}
