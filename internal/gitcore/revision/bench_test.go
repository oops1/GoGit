package revision

import (
	"context"
	"strconv"
	"testing"
)

const benchmarkCommits = 10000

func benchmarkHistory(b *testing.B) (*builder, string) {
	b.Helper()
	built := newBuilder(b)
	previous := ""
	for index := range benchmarkCommits {
		name := "c" + strconv.Itoa(index)
		if previous == "" {
			built.commit(name)
		} else {
			built.commit(name, previous)
		}
		previous = name
	}
	return built, previous
}

func BenchmarkWalkTenThousandCommits(b *testing.B) {
	built, tip := benchmarkHistory(b)
	opts := built.options(tip)
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		count := 0
		for commit, err := range Walk(ctx, opts) {
			if err != nil {
				b.Fatalf("Walk returned error %v", err)
			}
			count += len(commit.Parents)
		}
		if count != benchmarkCommits-1 {
			b.Fatalf("Walk visited %d parents, want %d", count, benchmarkCommits-1)
		}
	}
}

func BenchmarkWalkTenThousandCommitsInTopologicalOrder(b *testing.B) {
	built, tip := benchmarkHistory(b)
	opts := built.options(tip)
	opts.Order = Topo
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		for _, err := range Walk(ctx, opts) {
			if err != nil {
				b.Fatalf("Walk returned error %v", err)
			}
		}
	}
}

func BenchmarkParseRevisions(b *testing.B) {
	built := parseFixture(b)
	ctx := built.context()
	specs := []string{"HEAD", "main~2", "HEAD^2", "v2^{}", "HEAD:dir/nested.txt", "@{-1}"}
	b.ReportAllocs()
	for b.Loop() {
		for _, spec := range specs {
			if _, err := Parse(spec, ctx); err != nil {
				b.Fatalf("Parse(%q) returned error %v", spec, err)
			}
		}
	}
}
