package commitgraph

import (
	"bytes"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

func fuzzSeeds(f *testing.F) [][]byte {
	f.Helper()
	seeds := make([][]byte, 0, 3)
	for _, opts := range []EncodeOptions{levelsOnly, {}, {ChangedPaths: true}} {
		data, err := Encode(hash.SHA1, linearHistory(), opts)
		if err != nil {
			f.Fatalf("Encode returned error %v", err)
		}
		seeds = append(seeds, data)
	}
	return seeds
}

func FuzzParseLayer(f *testing.F) {
	for _, seed := range fuzzSeeds(f) {
		f.Add(seed)
	}
	f.Add([]byte{})
	f.Add([]byte("CGPH"))
	f.Add(append([]byte("CGPH"), bytes.Repeat([]byte{0xFF}, 32)...))

	f.Fuzz(func(t *testing.T, data []byte) {
		parsed, err := parseLayer(data, OpenOptions{})
		if err != nil {
			if parsed != nil {
				t.Fatalf("parseLayer returned a layer together with %v", err)
			}
			return
		}
		if parsed.count > uint32(len(data)) {
			t.Fatalf("the layer claims %d commits inside %d bytes", parsed.count, len(data))
		}
		graph := newGraph([]*layer{parsed})
		for position := range min(int(parsed.count), 64) {
			at := Position(position)
			id := graph.ID(at)
			entry := graph.Entry(at)
			for _, parent := range entry.Parents {
				if uint32(parent) >= uint32(graph.Len()) {
					t.Fatalf("commit %d names parent %d outside the %d it holds", at, parent, graph.Len())
				}
			}
			graph.Generation(at)
			if found, ok := graph.Lookup(id); ok && found != at {
				t.Fatalf("Lookup(%s) = %d, the graph holds it at %d", id, found, at)
			}
		}
	})
}
