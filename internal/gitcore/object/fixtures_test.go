package object_test

import (
	"bytes"
	"compress/zlib"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
)

type fixture struct {
	name string
	id   hash.ObjectID
	kind object.Type
	path string
}

func loadFixtures() ([]fixture, error) {
	manifest, err := os.ReadFile(filepath.Join("testdata", "manifest.tsv"))
	if err != nil {
		return nil, err
	}
	var all []fixture
	for _, line := range strings.Split(strings.TrimSpace(string(manifest)), "\n") {
		fields := strings.Split(strings.TrimSpace(line), "\t")
		if len(fields) != 3 {
			return nil, fmt.Errorf("malformed manifest line %q", line)
		}
		id, err := hash.Parse(fields[1])
		if err != nil {
			return nil, err
		}
		kind, err := object.ParseType(fields[2])
		if err != nil {
			return nil, err
		}
		all = append(all, fixture{
			name: fields[0],
			id:   id,
			kind: kind,
			path: filepath.Join("testdata", "objects", fields[1][:2], fields[1][2:]),
		})
	}
	if len(all) == 0 {
		return nil, errors.New("manifest is empty")
	}
	return all, nil
}

func inflateLoose(compressed []byte) ([]byte, error) {
	decompressor, err := zlib.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return nil, err
	}
	defer func() { _ = decompressor.Close() }()
	return io.ReadAll(decompressor)
}

func fixtureContent(path string) ([]byte, error) {
	compressed, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	inflated, err := inflateLoose(compressed)
	if err != nil {
		return nil, err
	}
	_, body, found := bytes.Cut(inflated, []byte{0})
	if !found {
		return nil, fmt.Errorf("%s has no header terminator", path)
	}
	return body, nil
}

func fixtures(t *testing.T) []fixture {
	t.Helper()
	all, err := loadFixtures()
	if err != nil {
		t.Fatalf("load fixtures: %v", err)
	}
	return all
}

func namedFixture(t *testing.T, name string) fixture {
	t.Helper()
	for _, f := range fixtures(t) {
		if f.name == name {
			return f
		}
	}
	t.Fatalf("fixture %q is missing", name)
	return fixture{}
}

func (f fixture) loose(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(f.path)
	if err != nil {
		t.Fatalf("read %s: %v", f.path, err)
	}
	return data
}

func (f fixture) raw(t *testing.T) []byte {
	t.Helper()
	body, err := fixtureContent(f.path)
	if err != nil {
		t.Fatalf("read %s: %v", f.path, err)
	}
	return body
}

func (f fixture) object(t *testing.T) object.Object {
	t.Helper()
	obj, err := object.Parse(f.kind, f.raw(t))
	if err != nil {
		t.Fatalf("parse %s: %v", f.name, err)
	}
	return obj
}

func (f fixture) commit(t *testing.T) *object.Commit {
	t.Helper()
	commit, err := object.ParseCommit(f.raw(t))
	if err != nil {
		t.Fatalf("parse commit %s: %v", f.name, err)
	}
	return commit
}

func (f fixture) tree(t *testing.T) *object.Tree {
	t.Helper()
	tree, err := object.ParseTree(f.raw(t))
	if err != nil {
		t.Fatalf("parse tree %s: %v", f.name, err)
	}
	return tree
}

func (f fixture) tag(t *testing.T) *object.Tag {
	t.Helper()
	tag, err := object.ParseTag(f.raw(t))
	if err != nil {
		t.Fatalf("parse tag %s: %v", f.name, err)
	}
	return tag
}
