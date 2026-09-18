package submodule

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/index"
	"github.com/oops1/gogit/internal/gitcore/object"
)

type fakeBlobs map[hash.ObjectID][]byte

var errMissingBlob = errors.New("missing blob")

func (f fakeBlobs) Blob(id hash.ObjectID) (*object.Blob, error) {
	data, ok := f[id]
	if !ok {
		return nil, errMissingBlob
	}
	return &object.Blob{Data: data}, nil
}

const indexText = "[submodule \"from-index\"]\n\tpath = index\n"
const headText = "[submodule \"from-head\"]\n\tpath = head\n"

func blobID(text string) hash.ObjectID {
	return hash.SumSHA1("blob", []byte(text))
}

func sourceWith(t *testing.T, stage index.Stage) Source {
	t.Helper()
	idx := index.New(index.Version2)
	idx.Add(index.Entry{Path: GitmodulesFile, Mode: object.ModeBlob, ID: blobID(indexText), Stage: stage})
	return Source{
		WorkTree: t.TempDir(),
		Index:    idx,
		Objects:  fakeBlobs{blobID(indexText): []byte(indexText), blobID(headText): []byte(headText)},
		HeadBlob: blobID(headText),
	}
}

func moduleNames(m *Modules) []string {
	var names []string
	for _, module := range m.All() {
		names = append(names, module.Name)
	}
	return names
}

func TestLoadPrefersTheWorkingTreeFile(t *testing.T) {
	src := sourceWith(t, index.StageMerged)
	if err := os.WriteFile(filepath.Join(src.WorkTree, GitmodulesFile), []byte("[submodule \"work\"]\n\tpath = w\n"), 0o666); err != nil {
		t.Fatal(err)
	}
	modules, err := Load(src)
	if err != nil || len(moduleNames(modules)) != 1 || moduleNames(modules)[0] != "work" {
		t.Fatalf("Load = %v, %v", moduleNames(modules), err)
	}
}

func TestLoadFallsBackToTheIndexThenToHead(t *testing.T) {
	src := sourceWith(t, index.StageMerged)
	modules, err := Load(src)
	if err != nil || moduleNames(modules)[0] != "from-index" {
		t.Fatalf("index fallback = %v, %v", moduleNames(modules), err)
	}
	src.Index = index.New(index.Version2)
	modules, err = Load(src)
	if err != nil || moduleNames(modules)[0] != "from-head" {
		t.Fatalf("head fallback = %v, %v", moduleNames(modules), err)
	}
	src.Index = nil
	modules, err = Load(src)
	if err != nil || moduleNames(modules)[0] != "from-head" {
		t.Fatalf("head fallback without an index = %v, %v", moduleNames(modules), err)
	}
}

func TestLoadFindsNothingWithoutAnySource(t *testing.T) {
	src := sourceWith(t, index.StageMerged)
	for _, change := range []func(*Source){
		func(s *Source) { s.WorkTree = "" },
		func(s *Source) { s.Index, s.HeadBlob = nil, hash.Zero },
		func(s *Source) { s.Objects = nil },
		func(s *Source) {
			if err := os.Mkdir(filepath.Join(s.WorkTree, GitmodulesFile), 0o777); err != nil {
				t.Fatal(err)
			}
		},
	} {
		copied := src
		copied.WorkTree = t.TempDir()
		change(&copied)
		modules, err := Load(copied)
		if err != nil || modules.Len() != 0 {
			t.Fatalf("Load = %v, %v; want no modules", moduleNames(modules), err)
		}
	}
}

func TestLoadIgnoresAnUnmergedGitmodules(t *testing.T) {
	src := sourceWith(t, index.StageOurs)
	if err := os.WriteFile(filepath.Join(src.WorkTree, GitmodulesFile), []byte(headText), 0o666); err != nil {
		t.Fatal(err)
	}
	modules, err := Load(src)
	if err != nil || modules.Len() != 0 {
		t.Fatalf("Load = %v, %v", moduleNames(modules), err)
	}
}

func TestLoadReportsReadFailures(t *testing.T) {
	boom := errors.New("boom")
	src := sourceWith(t, index.StageMerged)
	src.Objects = fakeBlobs{}
	if _, err := Load(src); !errors.Is(err, ErrReadGitmodules) || !errors.Is(err, errMissingBlob) {
		t.Fatalf("missing blob returned %v", err)
	}

	src = sourceWith(t, index.StageMerged)
	statGitmodulesFile = func(string) (fs.FileInfo, error) { return nil, boom }
	_, err := Load(src)
	statGitmodulesFile = os.Stat
	if !errors.Is(err, boom) {
		t.Fatalf("stat failure returned %v", err)
	}

	if err := os.WriteFile(filepath.Join(src.WorkTree, GitmodulesFile), nil, 0o666); err != nil {
		t.Fatal(err)
	}
	readGitmodulesFile = func(string) ([]byte, error) { return nil, boom }
	_, err = Load(src)
	readGitmodulesFile = os.ReadFile
	if !errors.Is(err, boom) {
		t.Fatalf("read failure returned %v", err)
	}

	if err := os.WriteFile(filepath.Join(src.WorkTree, GitmodulesFile), []byte("[submodule \"a\"]\n\tpath\n"), 0o666); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(src); !errors.Is(err, ErrInvalidGitmodules) {
		t.Fatalf("broken file returned %v", err)
	}
}
