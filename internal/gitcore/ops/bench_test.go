package ops

import (
	"fmt"
	"testing"
)

const benchmarkStageFileCount = 1000

func benchmarkStageRepo(b *testing.B) (*testRepo, []string) {
	b.Helper()
	r := newTestRepo(b)
	paths := make([]string, 0, benchmarkStageFileCount)
	for at := range benchmarkStageFileCount {
		path := fmt.Sprintf("src/module%03d/file%04d.txt", at%32, at)
		r.writeFile(path, fmt.Sprintf("content of file %d\n", at))
		paths = append(paths, path)
	}
	return r, paths
}

func BenchmarkStageOneThousandFiles(b *testing.B) {
	r, paths := benchmarkStageRepo(b)
	ctx := b.Context()
	b.ReportAllocs()
	for b.Loop() {
		if err := Stage(ctx, r.repo, paths, StageOptions{}); err != nil {
			b.Fatalf("Stage returned error %v", err)
		}
	}
}
