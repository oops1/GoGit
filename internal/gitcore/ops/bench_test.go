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

const benchmarkResetFileCount = 2000

func benchmarkResetRepo(b *testing.B) (*testRepo, [2]string) {
	b.Helper()
	r := newTestRepo(b)
	paths := make([]string, 0, benchmarkResetFileCount)
	for at := range benchmarkResetFileCount {
		paths = append(paths, fmt.Sprintf("src/module%03d/file%05d.txt", at%64, at))
	}
	var commits [2]string
	for version, label := range []string{"first", "second"} {
		for at, path := range paths {
			r.writeFile(path, fmt.Sprintf("%s version of file %d\n", label, at))
		}
		if err := Stage(b.Context(), r.repo, paths, StageOptions{}); err != nil {
			b.Fatalf("Stage returned error %v", err)
		}
		commits[version] = r.commitAll(label).String()
	}
	return r, commits
}

func benchmarkResetMode(b *testing.B, mode ResetMode) {
	r, commits := benchmarkResetRepo(b)
	ctx := b.Context()
	b.ReportAllocs()
	turn := 0
	for b.Loop() {
		turn++
		if _, err := Reset(ctx, r.repo, commits[turn%2], ResetOptions{Mode: mode}); err != nil {
			b.Fatalf("Reset returned error %v", err)
		}
	}
}

func BenchmarkHardResetAcrossTwoThousandChangedFiles(b *testing.B) {
	benchmarkResetMode(b, ResetHard)
}

func BenchmarkMixedResetAcrossTwoThousandChangedFiles(b *testing.B) {
	benchmarkResetMode(b, ResetMixed)
}

func BenchmarkFastForwardMergeOfTwoThousandChangedFiles(b *testing.B) {
	r, commits := benchmarkResetRepo(b)
	ctx := b.Context()
	b.ReportAllocs()
	for b.Loop() {
		b.StopTimer()
		if _, err := Reset(ctx, r.repo, commits[0], ResetOptions{Mode: ResetHard}); err != nil {
			b.Fatalf("Reset returned error %v", err)
		}
		b.StartTimer()
		if _, err := Merge(ctx, r.repo, commits[1], MergeOptions{}); err != nil {
			b.Fatalf("Merge returned error %v", err)
		}
	}
}
