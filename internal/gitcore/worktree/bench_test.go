package worktree

import (
	"fmt"
	"testing"
)

func newBenchRepo(b *testing.B, fileCount int) *testRepo {
	b.Helper()
	tr := newTestRepo(b)
	for i := range fileCount {
		rel := fmt.Sprintf("src/module%03d/file%04d.go", i%64, i)
		tr.stage(rel, fmt.Sprintf("package p\n\nconst N = %d\n", i))
	}
	tr.commit("initial")
	for i := range fileCount / 10 {
		rel := fmt.Sprintf("src/module%03d/file%04d.go", i%64, i)
		tr.writeFile(rel, fmt.Sprintf("package p\n\nconst N = %d\n\nconst Changed = true\n", i))
	}
	return tr
}

func BenchmarkStatusOfFiveThousandFiles(b *testing.B) {
	tr := newBenchRepo(b, 5000)
	w := tr.open()
	ctx := b.Context()
	b.ResetTimer()
	for b.Loop() {
		if _, err := w.Status(ctx); err != nil {
			b.Fatalf("Status returned error %v", err)
		}
	}
}
