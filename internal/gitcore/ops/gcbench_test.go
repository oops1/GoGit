package ops

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/refs"
)

const (
	benchmarkHistoryCommits = 2000
	benchmarkHistoryDirs    = 8
	benchmarkHistoryFiles   = 5
)

type benchmarkHistory struct {
	db    *odb.DB
	blobs [][][]byte
	ids   [][]hash.ObjectID
	dirs  []hash.ObjectID
}

func (h *benchmarkHistory) put(b *testing.B, obj object.Object) hash.ObjectID {
	b.Helper()
	id, err := h.db.PutObject(obj)
	if err != nil {
		b.Fatalf("PutObject returned error %v", err)
	}
	return id
}

func (h *benchmarkHistory) putBlob(b *testing.B, dir, file int) {
	b.Helper()
	id, err := h.db.Put(object.TypeBlob, h.blobs[dir][file])
	if err != nil {
		b.Fatalf("Put returned error %v", err)
	}
	h.ids[dir][file] = id
}

func (h *benchmarkHistory) putDir(b *testing.B, dir int) {
	b.Helper()
	entries := make([]object.TreeEntry, 0, benchmarkHistoryFiles)
	for file := range benchmarkHistoryFiles {
		entries = append(entries, object.TreeEntry{Mode: object.ModeBlob, Name: fmt.Sprintf("file%d.go", file), ID: h.ids[dir][file]})
	}
	h.dirs[dir] = h.put(b, &object.Tree{Entries: entries})
}

func (h *benchmarkHistory) putRoot(b *testing.B) hash.ObjectID {
	b.Helper()
	entries := make([]object.TreeEntry, 0, benchmarkHistoryDirs)
	for dir := range benchmarkHistoryDirs {
		entries = append(entries, object.TreeEntry{Mode: object.ModeTree, Name: fmt.Sprintf("pkg%d", dir), ID: h.dirs[dir]})
	}
	return h.put(b, &object.Tree{Entries: entries})
}

func buildBenchmarkHistory(b *testing.B) *testRepo {
	b.Helper()
	r := newTestRepo(b)
	db, err := odb.Open(r.repo.ObjectsDir(), odb.Options{})
	if err != nil {
		b.Fatalf("odb.Open returned error %v", err)
	}
	defer func() { _ = db.Close() }()
	h := &benchmarkHistory{db: db, blobs: make([][][]byte, benchmarkHistoryDirs), ids: make([][]hash.ObjectID, benchmarkHistoryDirs), dirs: make([]hash.ObjectID, benchmarkHistoryDirs)}
	for dir := range benchmarkHistoryDirs {
		h.blobs[dir] = make([][]byte, benchmarkHistoryFiles)
		h.ids[dir] = make([]hash.ObjectID, benchmarkHistoryFiles)
		for file := range benchmarkHistoryFiles {
			var text strings.Builder
			for line := range 200 {
				fmt.Fprintf(&text, "func pkg%dFile%dLine%03d() int { return %d }\n", dir, file, line, line*file)
			}
			h.blobs[dir][file] = []byte(text.String())
			h.putBlob(b, dir, file)
		}
		h.putDir(b, dir)
	}
	var parent []hash.ObjectID
	var head hash.ObjectID
	for step := range benchmarkHistoryCommits {
		dir, file := step%benchmarkHistoryDirs, (step/benchmarkHistoryDirs)%benchmarkHistoryFiles
		h.blobs[dir][file] = fmt.Appendf(h.blobs[dir][file], "var change%05d = %d\n", step, step)
		h.putBlob(b, dir, file)
		h.putDir(b, dir)
		sig := object.Signature{Name: "ann", Email: "ann@example.com", When: time.Unix(1700000000+int64(step)*60, 0)}
		head = h.put(b, &object.Commit{Tree: h.putRoot(b), Parents: parent, Author: sig, Committer: sig, Message: fmt.Sprintf("change %d\n", step)})
		parent = []hash.ObjectID{head}
	}
	store, err := refs.Open(refs.Options{GitDir: r.repo.GitDir(), CommonDir: r.repo.CommonDir(), Committer: testSignature})
	if err != nil {
		b.Fatalf("refs.Open returned error %v", err)
	}
	defer func() { _ = store.Close() }()
	tx := store.Begin()
	if err := tx.Set(refs.BranchName("main"), head); err != nil {
		b.Fatalf("Set returned error %v", err)
	}
	if err := tx.Commit(); err != nil {
		b.Fatalf("Commit returned error %v", err)
	}
	return r
}

func copyBenchmarkTree(b *testing.B, from, to string) {
	b.Helper()
	entries, err := os.ReadDir(to)
	if err != nil && !os.IsNotExist(err) {
		b.Fatalf("ReadDir returned error %v", err)
	}
	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(to, entry.Name())); err != nil {
			b.Fatalf("RemoveAll returned error %v", err)
		}
	}
	if err := os.CopyFS(to, os.DirFS(from)); err != nil {
		b.Fatalf("CopyFS returned error %v", err)
	}
	root, err := os.OpenRoot(to)
	if err != nil {
		b.Fatalf("OpenRoot returned error %v", err)
	}
	defer func() { _ = root.Close() }()
	err = fs.WalkDir(root.FS(), ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		return root.Chmod(path, 0o644)
	})
	if err != nil {
		b.Fatalf("WalkDir returned error %v", err)
	}
}

func BenchmarkGCLooseHistory(b *testing.B) {
	r := buildBenchmarkHistory(b)
	template := filepath.Join(b.TempDir(), "objects")
	copyBenchmarkTree(b, r.repo.ObjectsDir(), template)
	var packed int64
	for b.Loop() {
		b.StopTimer()
		copyBenchmarkTree(b, template, r.repo.ObjectsDir())
		b.StartTimer()
		result, err := GC(b.Context(), r.repo)
		if err != nil {
			b.Fatalf("GC returned error %v", err)
		}
		packed = result.Repack.Bytes
	}
	b.ReportMetric(float64(packed), "pack-bytes")
}

func BenchmarkGCPackedHistory(b *testing.B) {
	r := buildBenchmarkHistory(b)
	if _, err := GC(b.Context(), r.repo); err != nil {
		b.Fatalf("GC returned error %v", err)
	}
	var packed int64
	for b.Loop() {
		result, err := GC(b.Context(), r.repo)
		if err != nil {
			b.Fatalf("GC returned error %v", err)
		}
		packed = result.Repack.Bytes
	}
	b.ReportMetric(float64(packed), "pack-bytes")
}
