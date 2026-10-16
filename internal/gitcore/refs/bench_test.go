package refs

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func benchmarkPackedRefs(count int) []byte {
	var text strings.Builder
	text.WriteString(packedHeaderFull)
	for index := range count {
		fmt.Fprintf(&text, "%040x refs/remotes/origin/branch-%04d\n", index+1, index)
	}
	return []byte(text.String())
}

func BenchmarkParsePackedRefs(b *testing.B) {
	data := benchmarkPackedRefs(1000)
	for b.Loop() {
		if _, err := parsePackedRefs(data); err != nil {
			b.Fatalf("parsePackedRefs returned error %v", err)
		}
	}
}

func BenchmarkCheckFormat(b *testing.B) {
	for b.Loop() {
		if err := CheckFormat("refs/remotes/origin/feature/long/branch/name", 0); err != nil {
			b.Fatalf("CheckFormat returned error %v", err)
		}
	}
}

func BenchmarkAll(b *testing.B) {
	dir := b.TempDir()
	if err := writeBenchmarkRepository(dir); err != nil {
		b.Fatalf("writeBenchmarkRepository returned error %v", err)
	}
	store, err := Open(Options{GitDir: dir})
	if err != nil {
		b.Fatalf("Open returned error %v", err)
	}
	b.Cleanup(func() { _ = store.Close() })
	for b.Loop() {
		count := 0
		for _, err := range store.All() {
			if err != nil {
				b.Fatalf("All returned error %v", err)
			}
			count++
		}
		if count != 1100 {
			b.Fatalf("All returned %d references", count)
		}
	}
}

func BenchmarkLookupOfAPackedRef(b *testing.B) {
	dir := b.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "HEAD"), []byte("ref: refs/heads/main\n"), 0o666); err != nil {
		b.Fatalf("WriteFile returned error %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "refs"), 0o777); err != nil {
		b.Fatalf("MkdirAll returned error %v", err)
	}
	packed := filepath.Join(dir, packedRefsFile)
	if err := os.WriteFile(packed, benchmarkPackedRefs(1000), 0o666); err != nil {
		b.Fatalf("WriteFile returned error %v", err)
	}
	settled := time.Now().Add(-time.Hour)
	if err := os.Chtimes(packed, settled, settled); err != nil {
		b.Fatalf("Chtimes returned error %v", err)
	}
	store, err := Open(Options{GitDir: dir})
	if err != nil {
		b.Fatalf("Open returned error %v", err)
	}
	defer func() { _ = store.Close() }()
	name := RemoteBranchName("origin", "branch-0500")
	for b.Loop() {
		if _, err := store.Lookup(name); err != nil {
			b.Fatalf("Lookup returned error %v", err)
		}
	}
}
