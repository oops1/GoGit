package commitgraph

import (
	"crypto/sha1"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

func sampleHistory() []Commit {
	a, b, c, d := id(0x10, 1), id(0x20, 2), id(0x30, 3), id(0x40, 4)
	merge, octopus, future := id(0x50, 5), id(0x60, 6), id(0x70, 7)
	return []Commit{
		{ID: a, Tree: id(0xA1, 0), Time: 100, Changed: &ChangedPaths{Paths: []string{"dir/sub/file.txt", "top.txt"}}},
		{ID: b, Tree: id(0xA2, 0), Parents: []hash.ObjectID{a}, Time: 200, Changed: &ChangedPaths{Paths: []string{"dir/other.txt"}}},
		{ID: c, Tree: id(0xA3, 0), Parents: []hash.ObjectID{a}, Time: 150, Changed: &ChangedPaths{Large: true}},
		{ID: d, Tree: id(0xA4, 0), Parents: []hash.ObjectID{a}, Time: 5000000000},
		{ID: merge, Tree: id(0xA5, 0), Parents: []hash.ObjectID{b, c}, Time: 250, Changed: &ChangedPaths{}},
		{ID: octopus, Tree: id(0xA6, 0), Parents: []hash.ObjectID{merge, b, c, d}, Time: 10, Changed: &ChangedPaths{Paths: []string{"top.txt"}}},
		{ID: future, Tree: id(0xA7, 0), Parents: []hash.ObjectID{octopus}, Time: 20},
	}
}

func writeGraphFile(t *testing.T, dir string, data []byte) {
	t.Helper()
	path := filepath.Join(dir, InfoDir, FileName)
	if err := os.MkdirAll(filepath.Dir(path), dirPerm); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, filePerm); err != nil {
		t.Fatal(err)
	}
}

func encodeSample(t *testing.T, opts EncodeOptions) []byte {
	t.Helper()
	data, err := Encode(hash.SHA1, sampleHistory(), opts)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func openData(t *testing.T, data []byte, opts OpenOptions) (*Graph, error) {
	t.Helper()
	dir := t.TempDir()
	writeGraphFile(t, dir, data)
	return Open([]string{dir}, opts)
}

func resum(data []byte) []byte {
	out := slices.Clone(data)
	sum := sha1.Sum(out[:len(out)-hash.Size])
	copy(out[len(out)-hash.Size:], sum[:])
	return out
}

func mustOpen(t *testing.T, data []byte, opts OpenOptions) *Graph {
	t.Helper()
	g, err := openData(t, data, opts)
	if err != nil || g == nil {
		t.Fatalf("Open returned %v, %v", g, err)
	}
	return g
}

func TestOpenFindsNothingWithoutAGraph(t *testing.T) {
	g, err := Open([]string{t.TempDir(), filepath.Join(t.TempDir(), "missing")}, OpenOptions{})
	if g != nil || err != nil {
		t.Fatalf("Open returned %v, %v", g, err)
	}
	if _, found := g.Lookup(id(1, 1)); found {
		t.Fatal("a missing graph found a commit")
	}
}

func TestOpenReadsEntriesWithCorrectedDates(t *testing.T) {
	g := mustOpen(t, encodeSample(t, EncodeOptions{ChangedPaths: true}), OpenOptions{})
	if g.Len() != 7 || g.Layers() != 1 || !g.CorrectedDates() || !g.ChangedPaths() {
		t.Fatalf("graph = %d commits, %d layers, corrected %v, paths %v", g.Len(), g.Layers(), g.CorrectedDates(), g.ChangedPaths())
	}
	byID := map[hash.ObjectID]Entry{}
	for _, c := range sampleHistory() {
		pos, ok := g.Lookup(c.ID)
		if !ok || g.ID(pos) != c.ID {
			t.Fatalf("Lookup(%s) = %d, %v", c.ID, pos, ok)
		}
		entry := g.Entry(pos)
		var parents []hash.ObjectID
		for _, parent := range entry.Parents {
			parents = append(parents, g.ID(parent))
		}
		if entry.Tree != c.Tree || entry.Time != c.Time || !slices.Equal(parents, c.Parents) || g.Generation(pos) != entry.Generation {
			t.Fatalf("Entry(%s) = %+v", c.ID, entry)
		}
		byID[c.ID] = entry
	}
	want := map[byte]uint64{0x10: 100, 0x20: 200, 0x30: 150, 0x40: 5000000000, 0x50: 250, 0x60: 705032705, 0x70: 705032706}
	for first, generation := range want {
		if got := byID[id(first, first>>4)].Generation; got != generation {
			t.Errorf("generation of %#x = %d, want %d", first, got, generation)
		}
	}
	for _, missing := range []hash.ObjectID{id(0, 0), id(0x10, 2), id(0x15, 0), id(0xFF, 0xFF)} {
		if _, ok := g.Lookup(missing); ok {
			t.Errorf("Lookup(%s) found a commit", missing)
		}
	}
}

func TestOpenFallsBackToTopologicalLevels(t *testing.T) {
	for _, tt := range []struct {
		name string
		data []byte
		opts OpenOptions
	}{
		{"levels only", encodeSample(t, levelsOnly), OpenOptions{}},
		{"generation data skipped", encodeSample(t, EncodeOptions{}), OpenOptions{SkipGenerationData: true}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			g := mustOpen(t, tt.data, tt.opts)
			pos, _ := g.Lookup(id(0x70, 7))
			if g.CorrectedDates() || g.Generation(pos) != 5 {
				t.Fatalf("corrected %v, generation %d", g.CorrectedDates(), g.Generation(pos))
			}
		})
	}
}

func TestZeroGenerationsReadAsInfinity(t *testing.T) {
	commits := []Commit{{ID: id(1, 1), Tree: id(2, 2), Changed: &ChangedPaths{Paths: []string{"a"}}}}
	data, err := Encode(hash.SHA1, commits, EncodeOptions{ChangedPaths: true})
	if err != nil {
		t.Fatal(err)
	}
	zeroed := replaceChunk(t, data, chunkGenerationData, []byte{0, 0, 0, 0})
	g := mustOpen(t, zeroed, OpenOptions{})
	if g.Generation(0) != GenerationInfinity || !g.MaybeChanged(0, g.BloomKeys("b")) {
		t.Fatalf("generation %d", g.Generation(0))
	}
	levels := mustOpen(t, zeroed, OpenOptions{SkipGenerationData: true})
	if levels.Generation(0) != 1 || levels.MaybeChanged(0, levels.BloomKeys("b")) {
		t.Fatalf("levels generation %d", levels.Generation(0))
	}
}

func TestChangedPathFiltersRuleOutUntouchedPaths(t *testing.T) {
	g := mustOpen(t, encodeSample(t, EncodeOptions{ChangedPaths: true}), OpenOptions{})
	position := func(first byte) Position {
		pos, _ := g.Lookup(id(first, first>>4))
		return pos
	}
	cases := []struct {
		commit byte
		path   string
		maybe  bool
	}{
		{0x10, "dir/sub/file.txt", true},
		{0x10, "dir/sub/", true},
		{0x10, "dir", true},
		{0x10, "top.txt", true},
		{0x10, "elsewhere.txt", false},
		{0x10, "dir/sub/other.txt", false},
		{0x20, "dir/sub", false},
		{0x30, "anything", true},
		{0x40, "anything", true},
		{0x50, "anything", false},
		{0x60, "top.txt", true},
		{0x10, "", true},
		{0x10, "/", true},
	}
	for _, c := range cases {
		if got := g.MaybeChanged(position(c.commit), g.BloomKeys(c.path)); got != c.maybe {
			t.Errorf("MaybeChanged(%#x, %q) = %v", c.commit, c.path, got)
		}
	}
	if keys := g.BloomKeys("a/b/c"); len(keys) != 3 || len(keys[0].hashes) != bloomNumHashes {
		t.Errorf("BloomKeys = %v", keys)
	}
	skipped := mustOpen(t, encodeSample(t, EncodeOptions{ChangedPaths: true}), OpenOptions{SkipChangedPaths: true})
	if skipped.ChangedPaths() || skipped.BloomKeys("top.txt") != nil || !skipped.MaybeChanged(0, nil) {
		t.Error("skipped changed paths were still used")
	}
}

func replaceChunk(t *testing.T, data []byte, chunkID uint32, body []byte) []byte {
	t.Helper()
	var chunks []chunk
	count := int(data[chunkCountOffset])
	replaced := false
	for at := range count {
		entry := data[headerSize+at*tocEntrySize:]
		current := binary.BigEndian.Uint32(entry)
		start := binary.BigEndian.Uint64(entry[wordSize:])
		end := binary.BigEndian.Uint64(entry[tocEntrySize+wordSize:])
		switch {
		case current != chunkID:
			chunks = append(chunks, chunk{id: current, body: data[start:end]})
		case body != nil:
			chunks = append(chunks, chunk{id: current, body: body})
			replaced = true
		}
	}
	if !replaced && body != nil {
		chunks = append(chunks, chunk{id: chunkID, body: body})
	}
	return assemble(chunks)
}

func TestFiltersWithBrokenIndexesAreIgnored(t *testing.T) {
	data := encodeSample(t, EncodeOptions{ChangedPaths: true})
	index := slices.Clone(chunkBody(t, data, chunkBloomIndexes))
	binary.BigEndian.PutUint32(index[0:], 1<<30)
	binary.BigEndian.PutUint32(index[1*wordSize:], 1<<30)
	binary.BigEndian.PutUint32(index[3*wordSize:], 0)
	g := mustOpen(t, replaceChunk(t, data, chunkBloomIndexes, index), OpenOptions{})
	for pos := range Position(4) {
		if !g.MaybeChanged(pos, g.BloomKeys("elsewhere.txt")) {
			t.Errorf("a broken index entry %d ruled out a path", pos)
		}
	}
	version := slices.Clone(chunkBody(t, data, chunkBloomData))
	binary.BigEndian.PutUint32(version, 2)
	for name, broken := range map[string][]byte{
		"hash version": replaceChunk(t, data, chunkBloomData, version),
		"short index":  replaceChunk(t, data, chunkBloomIndexes, index[:wordSize]),
		"short data":   replaceChunk(t, data, chunkBloomData, version[:bloomHeaderSize-1]),
		"no data":      replaceChunk(t, data, chunkBloomData, nil),
	} {
		if g := mustOpen(t, broken, OpenOptions{}); g.ChangedPaths() {
			t.Errorf("%s: filters were used", name)
		}
	}
}

func TestOpenIgnoresBrokenGenerationData(t *testing.T) {
	data := encodeSample(t, EncodeOptions{})
	g := mustOpen(t, replaceChunk(t, data, chunkGenerationData, []byte{0, 0, 0, 1}), OpenOptions{})
	if g.CorrectedDates() {
		t.Fatal("generation data of the wrong size was used")
	}
}

func TestOpenReadsOverflowingGenerations(t *testing.T) {
	commits := []Commit{
		{ID: id(1, 1), Tree: id(9, 1), Time: 4000000000},
		{ID: id(2, 2), Tree: id(9, 2), Parents: []hash.ObjectID{id(1, 1)}, Time: 10},
	}
	data, err := Encode(hash.SHA1, commits, EncodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if chunkBody(t, data, chunkGenerationOverflow) == nil {
		t.Fatal("no overflow chunk was written")
	}
	g := mustOpen(t, data, OpenOptions{})
	if got := g.Generation(1); got != 4000000001 {
		t.Fatalf("overflowing generation = %d", got)
	}
}

func corruptions(t *testing.T) map[string][]byte {
	t.Helper()
	padding := chunk{id: 0x50414444, body: make([]byte, fanoutEntries*wordSize)}
	data := replaceChunk(t, encodeSample(t, EncodeOptions{ChangedPaths: true}), padding.id, padding.body)
	twins := []Commit{{ID: id(0x10, 1)}, {ID: id(0x10, 2)}}
	twinData, err := Encode(hash.SHA1, twins, EncodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	twinLookup := chunkBody(t, twinData, chunkOIDLookup)
	withEdit := func(edit func(out []byte)) []byte {
		out := slices.Clone(data)
		edit(out)
		return resum(out)
	}
	putTOC := func(at, field int, value uint64) func([]byte) {
		return func(out []byte) {
			entry := out[headerSize+at*tocEntrySize:]
			if field == 0 {
				binary.BigEndian.PutUint32(entry, uint32(value))
				return
			}
			binary.BigEndian.PutUint64(entry[wordSize:], value)
		}
	}
	dataChunk := chunkBody(t, data, chunkData)
	cdat := func(edit func(rows []byte)) []byte {
		rows := slices.Clone(dataChunk)
		edit(rows)
		return replaceChunk(t, data, chunkData, rows)
	}
	lookupChunk := chunkBody(t, data, chunkOIDLookup)
	fanoutChunk := chunkBody(t, data, chunkOIDFanout)
	edgesChunk := chunkBody(t, data, chunkEdges)
	generationChunk := chunkBody(t, data, chunkGenerationData)
	return map[string][]byte{
		"too small":           data[:100],
		"signature":           withEdit(func(out []byte) { out[0] = 'X' }),
		"version":             withEdit(func(out []byte) { out[versionOffset] = 2 }),
		"hash version":        withEdit(func(out []byte) { out[hashVersionOffset] = 2 }),
		"checksum":            append(slices.Clone(data[:len(data)-1]), data[len(data)-1]^1),
		"too many chunks":     withEdit(func(out []byte) { out[chunkCountOffset] = 200 }),
		"early terminator":    withEdit(putTOC(1, 0, 0)),
		"backward offsets":    withEdit(putTOC(2, 1, 10)),
		"offset past the end": withEdit(putTOC(1, 1, uint64(len(data)))),
		"duplicate chunk":     withEdit(putTOC(1, 0, chunkOIDFanout)),
		"final id":            withEdit(func(out []byte) { out[headerSize+int(out[chunkCountOffset])*tocEntrySize] = 1 }),
		"missing fanout":      replaceChunk(t, data, chunkOIDFanout, nil),
		"short fanout":        replaceChunk(t, data, chunkOIDFanout, fanoutChunk[:len(fanoutChunk)-wordSize]),
		"unordered fanout":    replaceChunk(t, data, chunkOIDFanout, append([]byte{0, 0, 0, 9}, fanoutChunk[wordSize:]...)),
		"missing lookup":      replaceChunk(t, data, chunkOIDLookup, nil),
		"short lookup":        replaceChunk(t, data, chunkOIDLookup, lookupChunk[:hash.Size]),
		"missing data":        replaceChunk(t, data, chunkData, nil),
		"short data":          replaceChunk(t, data, chunkData, dataChunk[:dataWidth]),
		"unsorted lookup":     replaceChunk(t, twinData, chunkOIDLookup, append(slices.Clone(twinLookup[hash.Size:]), twinLookup[:hash.Size]...)),
		"fanout before ids":   replaceChunk(t, data, chunkOIDFanout, append(slices.Clone(fanoutChunk[:0x10*wordSize]), append([]byte{0, 0, 0, 0}, fanoutChunk[0x11*wordSize:]...)...)),
		"fanout after ids":    replaceChunk(t, data, chunkOIDFanout, append(slices.Clone(fanoutChunk[:0x70*wordSize]), append([]byte{0, 0, 0, 6}, fanoutChunk[0x71*wordSize:]...)...)),
		"first parent":        cdat(func(rows []byte) { binary.BigEndian.PutUint32(rows[dataWidth+hash.Size:], 7) }),
		"second parent":       cdat(func(rows []byte) { binary.BigEndian.PutUint32(rows[4*dataWidth+hash.Size+wordSize:], 70) }),
		"edge index":          cdat(func(rows []byte) { binary.BigEndian.PutUint32(rows[5*dataWidth+hash.Size+wordSize:], octopusFlag|9) }),
		"edge value":          replaceChunk(t, data, chunkEdges, append(slices.Clone(edgesChunk[:wordSize]), 0, 0, 0, 99)),
		"unterminated edges":  replaceChunk(t, data, chunkEdges, edgesChunk[:2*wordSize]),
		"generation overflow": replaceChunk(t, data, chunkGenerationData, append([]byte{0x80, 0, 0, 3}, generationChunk[wordSize:]...)),
	}
}

func TestOpenRejectsCorruptFiles(t *testing.T) {
	for name, data := range corruptions(t) {
		t.Run(name, func(t *testing.T) {
			g, err := openData(t, data, OpenOptions{})
			if g != nil || !errors.Is(err, ErrCorrupt) {
				t.Fatalf("Open returned %v, %v", g, err)
			}
		})
	}
}

func TestOpenUsesTheNextObjectDirectoryWhenOneIsCorrupt(t *testing.T) {
	broken, good := t.TempDir(), t.TempDir()
	writeGraphFile(t, broken, []byte("broken"))
	writeGraphFile(t, good, encodeSample(t, EncodeOptions{}))
	g, err := Open([]string{broken, good}, OpenOptions{})
	if g == nil || err != nil || g.Len() != 7 {
		t.Fatalf("Open returned %v, %v", g, err)
	}
}

func topLayer(t *testing.T, below []hash.ObjectID, bases []hash.ObjectID, commits []Commit, extra ...chunk) []byte {
	t.Helper()
	sorted := slices.Clone(commits)
	slices.SortFunc(sorted, func(a, b Commit) int { return a.ID.Compare(b.ID) })
	positions := map[hash.ObjectID]uint32{}
	for at, known := range below {
		positions[known] = uint32(at)
	}
	for at, c := range sorted {
		positions[c.ID] = uint32(len(below) + at)
	}
	levels := make([]generation, len(sorted))
	for at := range levels {
		levels[at] = generation{level: 1, corrected: uint64(sorted[at].Time)}
	}
	rows, edges := commitData(sorted, positions, levels)
	chunks := []chunk{{id: chunkOIDFanout, body: fanout(sorted)}, {id: chunkOIDLookup, body: lookup(sorted)}, {id: chunkData, body: rows}}
	if len(edges) > 0 {
		chunks = append(chunks, chunk{id: chunkEdges, body: edges})
	}
	offsets, _ := generationData(sorted, levels)
	chunks = append(chunks, chunk{id: chunkGenerationData, body: offsets})
	var baseIDs []byte
	for _, base := range bases {
		baseIDs = append(baseIDs, base[:]...)
	}
	if bases != nil {
		chunks = append(chunks, chunk{id: chunkBase, body: baseIDs})
	}
	return assemble(append(chunks, extra...))
}

func checksumOf(data []byte) hash.ObjectID {
	var out hash.ObjectID
	copy(out[:], data[len(data)-hash.Size:])
	return out
}

func writeChain(t *testing.T, dir string, chain string, layers ...[]byte) {
	t.Helper()
	graphs := filepath.Join(dir, InfoDir, GraphsDir)
	if err := os.MkdirAll(graphs, dirPerm); err != nil {
		t.Fatal(err)
	}
	for _, data := range layers {
		name := "graph-" + checksumOf(data).String() + ".graph"
		if err := os.WriteFile(filepath.Join(graphs, name), data, filePerm); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(graphs, ChainFileName), []byte(chain), filePerm); err != nil {
		t.Fatal(err)
	}
}

func chainText(layers ...[]byte) string {
	var out strings.Builder
	for _, data := range layers {
		out.WriteString(checksumOf(data).String() + "\n")
	}
	return out.String()
}

func sampleChain(t *testing.T) ([]byte, []byte, []Commit) {
	t.Helper()
	baseCommits := []Commit{
		{ID: id(0x10, 1), Tree: id(0xB1, 0), Time: 100},
		{ID: id(0x20, 2), Tree: id(0xB2, 0), Parents: []hash.ObjectID{id(0x10, 1)}, Time: 200},
		{ID: id(0x30, 3), Tree: id(0xB3, 0), Parents: []hash.ObjectID{id(0x10, 1)}, Time: 300},
	}
	base, err := Encode(hash.SHA1, baseCommits, EncodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	topCommits := []Commit{
		{ID: id(0x05, 4), Tree: id(0xB4, 0), Parents: []hash.ObjectID{id(0x20, 2), id(0x30, 3), id(0x10, 1)}, Time: 400},
		{ID: id(0x25, 5), Tree: id(0xB5, 0), Parents: []hash.ObjectID{id(0x05, 4)}, Time: 500},
	}
	top := topLayer(t, []hash.ObjectID{id(0x10, 1), id(0x20, 2), id(0x30, 3)}, []hash.ObjectID{checksumOf(base)}, topCommits)
	return base, top, append(baseCommits, topCommits...)
}

func TestOpenReadsASplitChain(t *testing.T) {
	base, top, commits := sampleChain(t)
	primary, alternate := t.TempDir(), t.TempDir()
	writeChain(t, primary, chainText(base, top), top)
	writeChain(t, alternate, "", base)
	g, err := Open([]string{primary, alternate}, OpenOptions{})
	if err != nil || g == nil || g.Layers() != 2 || g.Len() != 5 || !g.CorrectedDates() {
		t.Fatalf("Open returned %v, %v", g, err)
	}
	for _, c := range commits {
		pos, ok := g.Lookup(c.ID)
		entry := g.Entry(pos)
		var parents []hash.ObjectID
		for _, parent := range entry.Parents {
			parents = append(parents, g.ID(parent))
		}
		if !ok || entry.Tree != c.Tree || !slices.Equal(parents, c.Parents) {
			t.Fatalf("commit %s reads as %+v at %d", c.ID, entry, pos)
		}
	}
	levels, err := Encode(hash.SHA1, commits[:3], levelsOnly)
	if err != nil {
		t.Fatal(err)
	}
	mixed := topLayer(t, []hash.ObjectID{id(0x10, 1), id(0x20, 2), id(0x30, 3)}, []hash.ObjectID{checksumOf(levels)}, commits[3:4])
	dir := t.TempDir()
	writeChain(t, dir, chainText(levels, mixed), levels, mixed)
	if g, err := Open([]string{dir}, OpenOptions{}); g == nil || err != nil || g.CorrectedDates() || g.Layers() != 2 {
		t.Fatalf("a mixed chain returned %v, %v", g, err)
	}
}

func TestChainLayersWithoutFiltersKeepEveryPathPossible(t *testing.T) {
	base, _, _ := sampleChain(t)
	index, bloom := bloomChunks([][]byte{buildFilter(&ChangedPaths{Paths: []string{"top.txt"}})})
	top := topLayer(t, []hash.ObjectID{id(0x10, 1), id(0x20, 2), id(0x30, 3)}, []hash.ObjectID{checksumOf(base)},
		[]Commit{{ID: id(0x05, 4), Parents: []hash.ObjectID{id(0x20, 2)}, Time: 400}},
		chunk{id: chunkBloomIndexes, body: index}, chunk{id: chunkBloomData, body: bloom})
	dir := t.TempDir()
	writeChain(t, dir, chainText(base, top), base, top)
	g, err := Open([]string{dir}, OpenOptions{})
	if err != nil || g == nil || !g.ChangedPaths() {
		t.Fatalf("Open returned %v, %v", g, err)
	}
	keys := g.BloomKeys("elsewhere.txt")
	if !g.MaybeChanged(0, keys) || g.MaybeChanged(3, keys) {
		t.Fatal("filters were read from the wrong layer")
	}
}

func TestFiltersForManyDirectoriesAreLarge(t *testing.T) {
	var paths []string
	for at := range 300 {
		paths = append(paths, "d"+strconv.Itoa(at)+"/f")
	}
	if filter := buildFilter(&ChangedPaths{Paths: paths}); len(filter) != 1 || filter[0] != bloomLargeFilter {
		t.Fatalf("filter = %d bytes", len(filter))
	}
	if filter := buildFilter(&ChangedPaths{Paths: paths[:10]}); len(filter) != 25 {
		t.Fatalf("filter = %d bytes", len(filter))
	}
	if buildFilter(nil) != nil || len(buildFilter(&ChangedPaths{})) != 1 {
		t.Fatal("missing or empty change lists encode wrongly")
	}
}

func TestOpenKeepsTheValidPrefixOfABrokenChain(t *testing.T) {
	base, top, _ := sampleChain(t)
	noBase := topLayer(t, []hash.ObjectID{id(0x10, 1), id(0x20, 2), id(0x30, 3)}, nil, []Commit{{ID: id(0x06, 6), Parents: []hash.ObjectID{id(0x10, 1)}}})
	wrongBase := topLayer(t, []hash.ObjectID{id(0x10, 1), id(0x20, 2), id(0x30, 3)}, []hash.ObjectID{id(1, 1)}, []Commit{{ID: id(0x06, 6)}})
	badParent := topLayer(t, nil, []hash.ObjectID{checksumOf(base)}, []Commit{{ID: id(0x06, 6)}, {ID: id(0x07, 7), Parents: []hash.ObjectID{id(0x06, 6)}}})
	binary.BigEndian.PutUint32(chunkBody(t, badParent, chunkData)[dataWidth+hash.Size:], 50)
	badParent = resum(badParent)
	other, err := Encode(hash.SHA1, []Commit{{ID: id(0x10, 1)}, {ID: id(0x20, 2)}, {ID: id(0x30, 3)}}, levelsOnly)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		chain  string
		files  [][]byte
		layers int
	}{
		{"missing top layer", chainText(base, top), [][]byte{base}, 1},
		{"no base chunk", chainText(base, noBase), [][]byte{base, noBase}, 1},
		{"short base chunk", chainText(base, base, top), [][]byte{base, top}, 1},
		{"wrong base", chainText(base, wrongBase), [][]byte{base, wrongBase}, 1},
		{"bad parent in a layer", chainText(base, badParent), [][]byte{base, badParent}, 1},
		{"bad hash line", chainText(base) + strings.Repeat("z", hash.HexSize) + "\n", [][]byte{base}, 1},
		{"partial last line", chainText(base, top)[:hash.HexSize*2+1], [][]byte{base, top}, 1},
		{"base file with other content", chainText(base, top), [][]byte{other, top}, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			writeChain(t, dir, c.chain, c.files...)
			if c.name == "base file with other content" {
				graphs := filepath.Join(dir, InfoDir, GraphsDir)
				if err := os.Rename(filepath.Join(graphs, "graph-"+checksumOf(other).String()+".graph"), filepath.Join(graphs, "graph-"+checksumOf(base).String()+".graph")); err != nil {
					t.Fatal(err)
				}
			}
			g, _ := Open([]string{dir}, OpenOptions{})
			layers := 0
			if g != nil {
				layers = g.Layers()
			}
			if layers != c.layers {
				t.Fatalf("Open kept %d layers, want %d", layers, c.layers)
			}
		})
	}
}

func TestOpenReportsBrokenChainFiles(t *testing.T) {
	for name, chain := range map[string]string{
		"too small": "abc\n",
		"not hex":   strings.Repeat("x", hash.HexSize) + "\n",
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			writeChain(t, dir, chain)
			if g, err := Open([]string{dir}, OpenOptions{}); g != nil || !errors.Is(err, ErrCorrupt) {
				t.Fatalf("Open returned %v, %v", g, err)
			}
		})
	}
	empty := t.TempDir()
	writeChain(t, empty, "")
	if g, err := Open([]string{empty}, OpenOptions{}); g != nil || err != nil {
		t.Fatalf("an empty chain returned %v, %v", g, err)
	}
	unreadable := t.TempDir()
	writeChain(t, unreadable, "")
	chainPath := filepath.Join(unreadable, InfoDir, GraphsDir, ChainFileName)
	if err := os.Remove(chainPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(chainPath, dirPerm); err != nil {
		t.Fatal(err)
	}
	if g, err := Open([]string{unreadable}, OpenOptions{}); g != nil || err == nil {
		t.Fatalf("an unreadable chain returned %v, %v", g, err)
	}
}
