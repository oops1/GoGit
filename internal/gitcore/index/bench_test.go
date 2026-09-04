package index

import (
	"bytes"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/oops1/gogit/internal/gitcore/object"
)

const benchmarkEntries = 10000

func benchmarkIndex(tb testing.TB) *Index {
	tb.Helper()
	idx := New(Version2)
	for at := range benchmarkEntries {
		idx.Add(Entry{
			Path: fmt.Sprintf("src/module%03d/package%02d/file%04d.go", at%128, at%64, at),
			Mode: object.ModeBlob,
			ID:   idOfByte(byte(at)),
			Stat: Stat{
				CTime: time.Unix(1700000000+int64(at), int64(at)),
				MTime: time.Unix(1700000000+int64(at), int64(at)),
				Size:  uint32(at * 3),
			},
		})
	}
	return idx
}

func BenchmarkReadIndexOfTenThousandEntries(b *testing.B) {
	data := encodeIndex(b, benchmarkIndex(b), Version2)
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := Read(bytes.NewReader(data)); err != nil {
			b.Fatalf("Read returned error %v", err)
		}
	}
}

func BenchmarkReadIndexOfTenThousandEntriesInVersionFour(b *testing.B) {
	data := encodeIndex(b, benchmarkIndex(b), Version4)
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := Read(bytes.NewReader(data)); err != nil {
			b.Fatalf("Read returned error %v", err)
		}
	}
}

func BenchmarkWriteIndexOfTenThousandEntries(b *testing.B) {
	idx := benchmarkIndex(b)
	b.SetBytes(int64(len(encodeIndex(b, idx, Version2))))
	b.ReportAllocs()
	for b.Loop() {
		if err := idx.Write(io.Discard, Version2); err != nil {
			b.Fatalf("Write returned error %v", err)
		}
	}
}

func BenchmarkWriteIndexOfTenThousandEntriesInVersionFour(b *testing.B) {
	idx := benchmarkIndex(b)
	b.SetBytes(int64(len(encodeIndex(b, idx, Version4))))
	b.ReportAllocs()
	for b.Loop() {
		if err := idx.Write(io.Discard, Version4); err != nil {
			b.Fatalf("Write returned error %v", err)
		}
	}
}

func BenchmarkWriteTreeOfTenThousandEntries(b *testing.B) {
	source := benchmarkIndex(b)
	b.ReportAllocs()
	for b.Loop() {
		idx := New(Version2)
		idx.entries = source.entries
		if _, err := idx.WriteTree(newMemoryObjects()); err != nil {
			b.Fatalf("WriteTree returned error %v", err)
		}
	}
}
