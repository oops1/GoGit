package blame

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
)

func benchmarkHistory(b *testing.B, commits, lines int) (*store, hash.ObjectID) {
	b.Helper()
	s := newStore()
	text := make([]string, lines)
	for at := range text {
		text[at] = fmt.Sprintf("line %d of the file\n", at)
	}
	siblings := map[string]hash.ObjectID{}
	for at := range 40 {
		siblings[fmt.Sprintf("other%02d.go", at)] = s.blob(fmt.Sprintf("other %d\n", at))
	}
	var head hash.ObjectID
	for at := range commits {
		text[(at*7919)%lines] = fmt.Sprintf("line changed by commit %d\n", at)
		files := map[string]hash.ObjectID{}
		for name, id := range siblings {
			files[name] = id
		}
		files["target.go"] = s.blob(strings.Join(text, ""))
		if at%3 == 0 {
			files[fmt.Sprintf("other%02d.go", at%40)] = s.blob(fmt.Sprintf("other touched by %d\n", at))
			siblings[fmt.Sprintf("other%02d.go", at%40)] = files[fmt.Sprintf("other%02d.go", at%40)]
		}
		inner := s.tree(files)
		root := s.put(&object.Tree{Entries: []object.TreeEntry{
			{Mode: object.ModeBlob, Name: "README", ID: s.blob("readme\n")},
			{Mode: object.ModeTree, Name: "src", ID: inner},
		}})
		if head.IsZero() {
			head = s.commit(fmt.Sprintf("commit %d", at), root, int64(1000+at))
			continue
		}
		head = s.commit(fmt.Sprintf("commit %d", at), root, int64(1000+at), head)
	}
	return s, head
}

func benchmarkWideHistory(b *testing.B, branches, lines int) (*store, hash.ObjectID) {
	b.Helper()
	s := newStore()
	text := make([]string, lines)
	for at := range text {
		text[at] = fmt.Sprintf("line %d of the file\n", at)
	}
	fileTree := func(content []string) hash.ObjectID {
		return s.tree(map[string]hash.ObjectID{"target.go": s.blob(strings.Join(content, ""))})
	}
	original := slices.Clone(text)
	root := s.commit("root", fileTree(text), 1000)
	head := root
	for at := range branches {
		line := (at * 7919) % lines
		side := slices.Clone(original)
		side[line] = fmt.Sprintf("line changed by branch %d\n", at)
		tip := s.commit(fmt.Sprintf("branch %d", at), fileTree(side), int64(2000+at), root)
		text[line] = side[line]
		head = s.commit(fmt.Sprintf("merge %d", at), fileTree(text), int64(2000+branches+at), head, tip)
	}
	return s, head
}

func BenchmarkBlameWideHistory(b *testing.B) {
	s, head := benchmarkWideHistory(b, 1500, 3000)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := File(b.Context(), s, head, "target.go", Options{}); err != nil {
			b.Fatalf("File returned error %v", err)
		}
	}
}

func BenchmarkBlameLongHistory(b *testing.B) {
	s, head := benchmarkHistory(b, 3000, 3000)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := File(b.Context(), s, head, "src/target.go", Options{}); err != nil {
			b.Fatalf("File returned error %v", err)
		}
	}
}
