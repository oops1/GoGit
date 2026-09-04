package index

import (
	"bytes"
	"slices"
	"testing"
)

func FuzzRead(f *testing.F) {
	for _, name := range append(fixtureNames(), splitV2) {
		f.Add(readFixture(f, name))
	}
	f.Add([]byte(nil))
	f.Add([]byte("DIRC"))
	f.Add(buildIndex(f, Version4, blobEntry("a", StageMerged), blobEntry("ab", StageOurs)))
	f.Fuzz(func(t *testing.T, data []byte) {
		idx, err := Read(bytes.NewReader(data))
		if err != nil {
			if idx != nil {
				t.Fatalf("Read returned an index together with %v", err)
			}
			return
		}
		encoded, err := idx.encode(idx.Version)
		if err != nil {
			t.Fatalf("encode returned error %v for an index that was read", err)
		}
		again, err := Read(bytes.NewReader(encoded))
		if err != nil {
			t.Fatalf("Read rejected the index it had just written: %v", err)
		}
		if !slices.Equal(paths(again), paths(idx)) {
			t.Fatalf("the paths changed while the index was rewritten: %v", paths(again))
		}
		for entry := range idx.Entries() {
			if _, ok := idx.Get(entry.Path, entry.Stage); !ok {
				t.Fatalf("Get(%q, %d) found nothing after Read", entry.Path, entry.Stage)
			}
		}
		for path := range idx.Paths("") {
			for _, conflict := range idx.Conflicts(path) {
				if conflict.Stage == StageMerged {
					t.Fatalf("Conflicts(%q) reported a merged entry", path)
				}
			}
		}
	})
}

func FuzzCacheTree(f *testing.F) {
	f.Add([]byte("\x004 1\x00" + string(make([]byte, 20))))
	f.Add(encodeCacheTree(&CacheTree{EntryCount: -1}))
	f.Add(encodeCacheTree(&CacheTree{EntryCount: 2, Subtrees: []*CacheTree{{Path: "lib", EntryCount: 1}}}))
	f.Fuzz(func(t *testing.T, data []byte) {
		tree, err := parseCacheTree(data)
		if err != nil {
			if tree != nil {
				t.Fatalf("parseCacheTree returned a tree together with %v", err)
			}
			return
		}
		encoded := encodeCacheTree(tree)
		again, err := parseCacheTree(encoded)
		if err != nil {
			t.Fatalf("parseCacheTree rejected the cache tree it had just encoded: %v", err)
		}
		if !bytes.Equal(encodeCacheTree(again), encoded) {
			t.Fatal("encoding a cache tree twice produces different bytes")
		}
	})
}

func FuzzResolveUndo(f *testing.F) {
	f.Add([]byte("a\x00100644\x000\x000\x00" + string(make([]byte, 20))))
	f.Add([]byte("a\x000\x000\x000\x00"))
	f.Fuzz(func(t *testing.T, data []byte) {
		entries, err := parseResolveUndo(data)
		if err != nil {
			if entries != nil {
				t.Fatalf("parseResolveUndo returned entries together with %v", err)
			}
			return
		}
		encoded := encodeResolveUndo(entries)
		again, err := parseResolveUndo(encoded)
		if err != nil {
			t.Fatalf("parseResolveUndo rejected the data it had just encoded: %v", err)
		}
		if !slices.Equal(again, entries) {
			t.Fatal("the resolve undo entries changed while they were rewritten")
		}
	})
}
