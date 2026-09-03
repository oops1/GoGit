package pack

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func fuzzCorpus(f *testing.F, pattern string) [][]byte {
	f.Helper()
	matches, err := filepath.Glob(pattern)
	if err != nil {
		f.Fatalf("Glob returned error %v", err)
	}
	var corpus [][]byte
	for _, match := range matches {
		data, err := os.ReadFile(match)
		if err != nil {
			f.Fatalf("ReadFile(%q) returned error %v", match, err)
		}
		corpus = append(corpus, data)
	}
	if len(corpus) == 0 {
		f.Fatalf("no seeds match %q", pattern)
	}
	return corpus
}

func FuzzApplyDelta(f *testing.F) {
	base := []byte("the quick brown fox jumps over the lazy dog")
	seeds := [][]byte{
		nil,
		{0},
		slices.Concat(deltaSizes(int64(len(base)), 5), insertOp([]byte("hello"))),
		slices.Concat(deltaSizes(int64(len(base)), 9), copyOp(4, 5), insertOp([]byte("!!!!"))),
		slices.Concat(deltaSizes(int64(len(base)), defaultCopySize), copyOp(0, 0)),
		bytes.Repeat([]byte{0x80}, 16),
	}
	for _, seed := range seeds {
		f.Add(base, seed)
	}
	f.Fuzz(func(t *testing.T, base, delta []byte) {
		out, err := ApplyDelta(base, delta)
		if err != nil {
			if out != nil {
				t.Fatalf("ApplyDelta returned %d bytes together with %v", len(out), err)
			}
			return
		}
		_, read, err := decodeDeltaSize(delta, "source")
		if err != nil {
			t.Fatalf("decodeDeltaSize returned error %v after ApplyDelta succeeded", err)
		}
		target, _, err := decodeDeltaSize(delta[read:], "target")
		if err != nil {
			t.Fatalf("decodeDeltaSize returned error %v after ApplyDelta succeeded", err)
		}
		if int64(len(out)) != target {
			t.Fatalf("ApplyDelta produced %d bytes, the delta declares %d", len(out), target)
		}
	})
}

func FuzzIndexParse(f *testing.F) {
	for _, seed := range fuzzCorpus(f, filepath.Join(packsDir, "*"+indexSuffix)) {
		f.Add(seed)
	}
	f.Add(buildIndex([]Entry{{ID: idOfByte(1), Offset: 12, CRC32: 7}}, idOfByte(9)))
	f.Fuzz(func(t *testing.T, data []byte) {
		index, err := NewIndex(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return
		}
		_ = index.Verify()
		for position := range min(index.Count(), 64) {
			entry, err := index.EntryAt(position)
			if err != nil {
				break
			}
			if found, ok := index.Find(entry.ID); ok && found != entry.Offset {
				t.Fatalf("Find(%s) = %d, EntryAt(%d) = %d", entry.ID, found, position, entry.Offset)
			}
		}
		seen := 0
		for range index.Objects() {
			seen++
			if seen == 64 {
				break
			}
		}
	})
}

func FuzzReader(f *testing.F) {
	for _, seed := range fuzzCorpus(f, filepath.Join(packsDir, "*"+packSuffix)) {
		f.Add(seed)
	}
	f.Add(readFixtureFile(f, thinPackPath))
	f.Fuzz(func(t *testing.T, data []byte) {
		reader, err := NewReader(bytes.NewReader(data))
		if err != nil {
			return
		}
		for range 256 {
			entry, err := reader.NextObject()
			if err != nil {
				if errors.Is(err, io.EOF) {
					return
				}
				return
			}
			if int64(len(entry.Data)) != entry.Header.Size {
				t.Fatalf("the object at %d holds %d bytes, its header declares %d",
					entry.Header.Offset, len(entry.Data), entry.Header.Size)
			}
		}
	})
}

func readFixtureFile(f *testing.F, path string) []byte {
	f.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		f.Fatalf("ReadFile(%q) returned error %v", path, err)
	}
	return data
}
