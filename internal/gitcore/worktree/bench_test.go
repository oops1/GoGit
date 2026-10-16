package worktree

import (
	"fmt"
	"os"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/index"
	"github.com/oops1/gogit/internal/gitcore/object"
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

func (r *testRepo) stageUnstored(rel, content string) {
	r.t.Helper()
	r.writeFile(rel, content)
	fi, err := os.Lstat(r.path(rel))
	if err != nil {
		r.t.Fatalf("Lstat returned error %v", err)
	}
	id, err := hash.Sum(hash.SHA1, "blob", []byte(content))
	if err != nil {
		r.t.Fatalf("Sum returned error %v", err)
	}
	r.idx.Add(index.Entry{
		Path: rel,
		Mode: object.ModeBlob,
		ID:   id,
		Stat: index.Stat{MTime: fi.ModTime(), CTime: fi.ModTime(), Size: uint32(fi.Size())},
	})
}

func newLargeBenchRepo(b *testing.B, fileCount int) *testRepo {
	b.Helper()
	tr := newTestRepo(b)
	for i := range fileCount {
		rel := fmt.Sprintf("src/module%03d/pkg%02d/file%05d.go", i%64, i%16, i)
		tr.stageUnstored(rel, fmt.Sprintf("package p\n\nconst N = %d\n", i))
	}
	tr.commit("initial")
	for i := range fileCount / 100 {
		rel := fmt.Sprintf("src/module%03d/pkg%02d/file%05d.go", i%64, i%16, i)
		tr.writeFile(rel, fmt.Sprintf("package p\n\nconst N = %d\n\nconst Changed = true\n", i))
	}
	return tr
}

func BenchmarkStatusOfFiftyThousandFiles(b *testing.B) {
	tr := newLargeBenchRepo(b, 50000)
	w := tr.open()
	ctx := b.Context()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := w.Status(ctx); err != nil {
			b.Fatalf("Status returned error %v", err)
		}
	}
}

func newUntrackedBenchRepo(b *testing.B, tracked, untracked int) *testRepo {
	b.Helper()
	tr := newTestRepo(b)
	for i := range tracked {
		tr.stageUnstored(fmt.Sprintf("src/module%03d/pkg%02d/file%05d.go", i%64, i%16, i), fmt.Sprintf("package p\n\nconst N = %d\n", i))
	}
	tr.commit("initial")
	for i := range untracked {
		tr.writeFile(fmt.Sprintf("scratch/area%02d/sub%02d/file%05d.tmp", i%32, i%8, i), "x\n")
	}
	return tr
}

func BenchmarkStatusWithTwentyThousandUntrackedFiles(b *testing.B) {
	tr := newUntrackedBenchRepo(b, 5000, 20000)
	w := tr.open()
	ctx := b.Context()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := w.Status(ctx); err != nil {
			b.Fatalf("Status returned error %v", err)
		}
	}
}

func BenchmarkStatusWithTwentyThousandIgnoredFiles(b *testing.B) {
	tr := newUntrackedBenchRepo(b, 5000, 0)
	tr.writeFile(".gitignore", "build/\n*.log\n")
	for i := range 20000 {
		tr.writeFile(fmt.Sprintf("build/area%02d/sub%02d/file%05d.o", i%32, i%8, i), "x\n")
	}
	for i := range 2000 {
		tr.writeFile(fmt.Sprintf("src/module%03d/pkg00/trace%04d.log", i%64, i), "x\n")
	}
	w := tr.open()
	ctx := b.Context()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := w.Status(ctx); err != nil {
			b.Fatalf("Status returned error %v", err)
		}
	}
}
