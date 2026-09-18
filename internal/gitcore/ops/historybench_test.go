package ops

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oops1/gogit/internal/gitcore/commitgraph"
	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/linelog"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/pack"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/revision"
)

const (
	largeHistoryCommits = 50000
	largeHistoryDirs    = 20
	largeHistoryFiles   = 10
	largeHistoryLines   = 100
	largeHistoryTarget  = "target.go"
	largeHistoryPath    = "pkg07/file3.go"
	largeHistoryRange   = "40,60"
)

type memoryObjects map[hash.ObjectID]memoryObject

type memoryObject struct {
	kind object.Type
	data []byte
}

func (m memoryObjects) Get(id hash.ObjectID) (object.Type, []byte, error) {
	stored := m[id]
	return stored.kind, stored.data, nil
}

func (m memoryObjects) put(obj object.Object) hash.ObjectID {
	id := obj.ID()
	m[id] = memoryObject{kind: obj.Type(), data: obj.Encode()}
	return id
}

type largeHistory struct {
	repo    *testRepo
	head    hash.ObjectID
	commits []commitgraph.Commit
}

type largeHistoryState struct {
	objects memoryObjects
	files   [largeHistoryDirs][largeHistoryFiles]hash.ObjectID
	dirs    [largeHistoryDirs]hash.ObjectID
	lines   []string
	target  hash.ObjectID
}

func (s *largeHistoryState) dir(at int) hash.ObjectID {
	entries := make([]object.TreeEntry, 0, largeHistoryFiles)
	for file := range largeHistoryFiles {
		entries = append(entries, object.TreeEntry{Mode: object.ModeBlob, Name: fmt.Sprintf("file%d.go", file), ID: s.files[at][file]})
	}
	s.dirs[at] = s.objects.put(&object.Tree{Entries: entries})
	return s.dirs[at]
}

func (s *largeHistoryState) root(extra ...object.TreeEntry) hash.ObjectID {
	entries := make([]object.TreeEntry, 0, largeHistoryDirs+1+len(extra))
	for dir := range largeHistoryDirs {
		entries = append(entries, object.TreeEntry{Mode: object.ModeTree, Name: fmt.Sprintf("pkg%02d", dir), ID: s.dirs[dir]})
	}
	entries = append(entries, object.TreeEntry{Mode: object.ModeBlob, Name: largeHistoryTarget, ID: s.target})
	entries = append(entries, extra...)
	tree := &object.Tree{Entries: entries}
	tree.Sort()
	return s.objects.put(tree)
}

func buildLargeHistory(b *testing.B) *largeHistory {
	b.Helper()
	s := &largeHistoryState{objects: memoryObjects{}}
	for dir := range largeHistoryDirs {
		for file := range largeHistoryFiles {
			s.files[dir][file] = s.objects.put(&object.Blob{Data: fmt.Appendf(nil, "package p%d\n", dir)})
		}
		s.dir(dir)
	}
	for line := range largeHistoryLines {
		s.lines = append(s.lines, fmt.Sprintf("line %d", line))
	}
	s.target = s.objects.put(&object.Blob{Data: []byte(strings.Join(s.lines, "\n") + "\n")})
	history := &largeHistory{}
	var parents []hash.ObjectID
	trees := map[hash.ObjectID]hash.ObjectID{}
	commit := func(tree hash.ObjectID, step int, parents []hash.ObjectID) hash.ObjectID {
		sig := object.Signature{Name: "ann", Email: "ann@example.com", When: time.Unix(1600000000+int64(step)*60, 0)}
		id := s.objects.put(&object.Commit{Tree: tree, Parents: parents, Author: sig, Committer: sig, Message: fmt.Sprintf("change %d\n", step)})
		history.commits = append(history.commits, commitgraph.Commit{ID: id, Tree: tree, Parents: parents, Time: sig.When.Unix()})
		trees[id] = tree
		return id
	}
	for step := range largeHistoryCommits {
		dir, file := step%largeHistoryDirs, (step/largeHistoryDirs)%largeHistoryFiles
		s.files[dir][file] = s.objects.put(&object.Blob{Data: fmt.Appendf(nil, "package p%d\nvar v = %d\n", dir, step)})
		s.dir(dir)
		if step%25 == 0 {
			s.lines[(step/25)%largeHistoryLines] = fmt.Sprintf("line changed at %d", step)
			s.target = s.objects.put(&object.Blob{Data: []byte(strings.Join(s.lines, "\n") + "\n")})
		}
		head := commit(s.root(), step, parents)
		if step%100 == 99 && len(history.commits) > 4 {
			base := history.commits[len(history.commits)-4].ID
			side := s.objects.put(&object.Blob{Data: fmt.Appendf(nil, "side %d\n", step)})
			sideTree := s.root(object.TreeEntry{Mode: object.ModeBlob, Name: "side.go", ID: side})
			sideCommit := commit(sideTree, step, []hash.ObjectID{base})
			head = commit(sideTree, step, []hash.ObjectID{head, sideCommit})
		}
		parents = []hash.ObjectID{head}
	}
	history.head = parents[0]
	history.repo = newTestRepo(b)
	ids := make([]hash.ObjectID, 0, len(s.objects))
	for id := range s.objects {
		ids = append(ids, id)
	}
	if _, err := pack.WritePackFile(b.Context(), history.repo.repo.PackDir(), s.objects, ids, pack.WriteOptions{}); err != nil {
		b.Fatalf("WritePackFile returned error %v", err)
	}
	store, err := refs.Open(refs.Options{GitDir: history.repo.repo.GitDir(), CommonDir: history.repo.repo.CommonDir(), Committer: testSignature})
	if err != nil {
		b.Fatalf("refs.Open returned error %v", err)
	}
	defer func() { _ = store.Close() }()
	tx := store.Begin()
	if err := tx.Set(refs.BranchName("main"), history.head); err != nil {
		b.Fatalf("Set returned error %v", err)
	}
	if err := tx.Commit(); err != nil {
		b.Fatalf("Commit returned error %v", err)
	}
	return history
}

func (h *largeHistory) writeGraph(b *testing.B) {
	b.Helper()
	db, err := odb.Open(h.repo.repo.ObjectsDir(), odb.Options{})
	if err != nil {
		b.Fatalf("odb.Open returned error %v", err)
	}
	defer func() { _ = db.Close() }()
	if err := fillChangedPaths(b.Context(), db, h.commits); err != nil {
		b.Fatalf("fillChangedPaths returned error %v", err)
	}
	if err := commitgraph.WriteFile(filepath.Join(h.repo.repo.ObjectsDir(), objectsInfoDir), hash.SHA1, h.commits, commitgraph.EncodeOptions{ChangedPaths: true}); err != nil {
		b.Fatalf("WriteFile returned error %v", err)
	}
}

func (h *largeHistory) modes(b *testing.B, run func(b *testing.B)) {
	b.Helper()
	b.Run("objects", run)
	h.writeGraph(b)
	b.Run("commit-graph", run)
}

func (h *largeHistory) source(b *testing.B) revision.Context {
	b.Helper()
	db, err := odb.Open(h.repo.repo.ObjectsDir(), odb.Options{})
	if err != nil {
		b.Fatalf("odb.Open returned error %v", err)
	}
	b.Cleanup(func() { _ = db.Close() })
	graph, err := OpenCommitGraph(h.repo.repo, db)
	if err != nil {
		b.Fatalf("OpenCommitGraph returned error %v", err)
	}
	return revision.Context{Objects: db, Graph: graph}
}

func BenchmarkLogOfFiftyThousandCommits(b *testing.B) {
	h := buildLargeHistory(b)
	h.modes(b, func(b *testing.B) {
		source := h.source(b)
		for b.Loop() {
			count := 0
			for _, err := range revision.Walk(context.Background(), revision.Options{Context: source, Include: []hash.ObjectID{h.head}}) {
				if err != nil {
					b.Fatalf("Walk returned error %v", err)
				}
				count++
			}
			if count != len(h.commits) {
				b.Fatalf("Walk visited %d commits, want %d", count, len(h.commits))
			}
		}
	})
}

func BenchmarkTopologicalLogFirstPageOfFiftyThousandCommits(b *testing.B) {
	h := buildLargeHistory(b)
	h.modes(b, func(b *testing.B) {
		source := h.source(b)
		for b.Loop() {
			count := 0
			for _, err := range revision.Walk(context.Background(), revision.Options{Context: source, Include: []hash.ObjectID{h.head}, Order: revision.Topo, MaxCount: 200}) {
				if err != nil {
					b.Fatalf("Walk returned error %v", err)
				}
				count++
			}
			if count != 200 {
				b.Fatalf("Walk visited %d commits", count)
			}
		}
	})
}

func BenchmarkFileHistoryOfFiftyThousandCommits(b *testing.B) {
	h := buildLargeHistory(b)
	h.modes(b, func(b *testing.B) {
		for b.Loop() {
			entries, err := FileHistory(context.Background(), h.repo.repo, "HEAD", largeHistoryPath, HistoryOptions{})
			if err != nil || len(entries) != largeHistoryCommits/(largeHistoryDirs*largeHistoryFiles)+1 {
				b.Fatalf("FileHistory returned %d entries, %v", len(entries), err)
			}
		}
	})
}

func BenchmarkLineHistoryOfFiftyThousandCommits(b *testing.B) {
	h := buildLargeHistory(b)
	h.modes(b, func(b *testing.B) {
		for b.Loop() {
			count := 0
			for _, err := range LineHistory(context.Background(), h.repo.repo, "HEAD", []linelog.Spec{{Range: largeHistoryRange, Path: largeHistoryTarget}}, LineHistoryOptions{}) {
				if err != nil {
					b.Fatalf("LineHistory returned error %v", err)
				}
				count++
			}
			if count == 0 {
				b.Fatal("LineHistory found nothing")
			}
		}
	})
}

func BenchmarkLineHistoryFirstEntryOfFiftyThousandCommits(b *testing.B) {
	h := buildLargeHistory(b)
	h.modes(b, func(b *testing.B) {
		for b.Loop() {
			for _, err := range LineHistory(context.Background(), h.repo.repo, "HEAD", []linelog.Spec{{Range: largeHistoryRange, Path: largeHistoryTarget}}, LineHistoryOptions{}) {
				if err != nil {
					b.Fatalf("LineHistory returned error %v", err)
				}
				break
			}
		}
	})
}
