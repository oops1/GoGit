package diff

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

func benchmarkPair(lines int) (old, updated []byte) {
	var before, after strings.Builder
	for at := range lines {
		fmt.Fprintf(&before, "package line %d with some content\n", at)
		switch {
		case at%17 == 0:
			fmt.Fprintf(&after, "package line %d rewritten\n", at)
		case at%29 == 0:
			fmt.Fprintf(&after, "package line %d with some content\n", at)
			fmt.Fprintf(&after, "inserted after %d\n", at)
		case at%41 == 0:
		default:
			fmt.Fprintf(&after, "package line %d with some content\n", at)
		}
	}
	return []byte(before.String()), []byte(after.String())
}

func BenchmarkBlobsMyers(b *testing.B) {
	old, updated := benchmarkPair(10000)
	opts := Defaults()
	b.ReportAllocs()
	b.SetBytes(int64(len(old) + len(updated)))
	for b.Loop() {
		Blobs(old, updated, opts)
	}
}

func BenchmarkBlobsHistogram(b *testing.B) {
	old, updated := benchmarkPair(10000)
	opts := Defaults()
	opts.Algorithm = AlgorithmHistogram
	b.ReportAllocs()
	b.SetBytes(int64(len(old) + len(updated)))
	for b.Loop() {
		Blobs(old, updated, opts)
	}
}

func BenchmarkBlobsIgnoringWhitespace(b *testing.B) {
	old, updated := benchmarkPair(10000)
	opts := Defaults()
	opts.IgnoreWhitespace = IgnoreAllSpace | IgnoreBlankLines
	b.ReportAllocs()
	for b.Loop() {
		Blobs(old, updated, opts)
	}
}

func BenchmarkUnified(b *testing.B) {
	old, updated := benchmarkPair(10000)
	opts := Defaults()
	file := File{
		OldPath: "a.txt", NewPath: "a.txt", Status: StatusModified,
		OldID: idOf(1), NewID: idOf(2), Hunks: Blobs(old, updated, opts),
	}
	var buf bytes.Buffer
	b.ReportAllocs()
	for b.Loop() {
		buf.Reset()
		if err := Unified(&buf, file, opts); err != nil {
			b.Fatalf("Unified returned error %v", err)
		}
	}
}

func BenchmarkApply(b *testing.B) {
	old, updated := benchmarkPair(10000)
	hunks := Blobs(old, updated, Defaults())
	b.ReportAllocs()
	for b.Loop() {
		if _, err := Apply(old, hunks); err != nil {
			b.Fatalf("Apply returned error %v", err)
		}
	}
}

func BenchmarkInlineDiff(b *testing.B) {
	old := "	if err := runTheThing(ctx, name, options); err != nil {"
	updated := "	if err := runTheOtherThing(ctx, name, extra, options); err != nil {"
	b.ReportAllocs()
	for b.Loop() {
		InlineDiff(old, updated)
	}
}

func BenchmarkTrees(b *testing.B) {
	store := newMemoryStore()
	old := treeFiles{}
	updated := treeFiles{}
	for at := range 200 {
		body := poem(fmt.Sprintf("file %d", at))
		old[fmt.Sprintf("dir%d/file%d.txt", at%10, at)] = blobSpec(body)
		updated[fmt.Sprintf("dir%d/file%d.txt", at%10, at)] = blobSpec(strings.Replace(body, "line 3\n", "line three\n", 1))
	}
	oldTree, newTree := buildTree(store, old), buildTree(store, updated)
	opts := Defaults()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := Trees(b.Context(), store, oldTree, newTree, opts); err != nil {
			b.Fatalf("Trees returned error %v", err)
		}
	}
}
