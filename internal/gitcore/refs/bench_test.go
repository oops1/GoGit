package refs

import (
	"fmt"
	"strings"
	"testing"
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
