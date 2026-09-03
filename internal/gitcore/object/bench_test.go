package object_test

import (
	"testing"

	"github.com/oops1/gogit/internal/gitcore/object"
)

func benchmarkContent(b *testing.B, name string) []byte {
	b.Helper()
	all, err := loadFixtures()
	if err != nil {
		b.Fatalf("load fixtures: %v", err)
	}
	for _, fixture := range all {
		if fixture.name != name {
			continue
		}
		raw, err := fixtureContent(fixture.path)
		if err != nil {
			b.Fatalf("read %s: %v", name, err)
		}
		return raw
	}
	b.Fatalf("fixture %q is missing", name)
	return nil
}

func BenchmarkParseTree(b *testing.B) {
	raw := benchmarkContent(b, "tree_root")
	b.SetBytes(int64(len(raw)))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := object.ParseTree(raw); err != nil {
			b.Fatalf("ParseTree: %v", err)
		}
	}
}

func BenchmarkParseTreeSeq(b *testing.B) {
	raw := benchmarkContent(b, "tree_root")
	b.SetBytes(int64(len(raw)))
	b.ReportAllocs()
	for b.Loop() {
		for _, err := range object.ParseTreeSeq(raw) {
			if err != nil {
				b.Fatalf("ParseTreeSeq: %v", err)
			}
		}
	}
}

func BenchmarkParseCommit(b *testing.B) {
	raw := benchmarkContent(b, "commit_merge")
	b.SetBytes(int64(len(raw)))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := object.ParseCommit(raw); err != nil {
			b.Fatalf("ParseCommit: %v", err)
		}
	}
}

func BenchmarkParseSignedCommit(b *testing.B) {
	raw := benchmarkContent(b, "commit_gpgsig")
	b.SetBytes(int64(len(raw)))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := object.ParseCommit(raw); err != nil {
			b.Fatalf("ParseCommit: %v", err)
		}
	}
}

func BenchmarkEncodeCommit(b *testing.B) {
	commit, err := object.ParseCommit(benchmarkContent(b, "commit_merge"))
	if err != nil {
		b.Fatalf("ParseCommit: %v", err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if len(commit.Encode()) == 0 {
			b.Fatal("Encode produced nothing")
		}
	}
}
