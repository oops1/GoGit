package revision

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/refs"
)

var _ Refs = (*refs.Store)(nil)

type looseObjects struct {
	dir string
}

func (l looseObjects) path(id hash.ObjectID) string {
	name := id.String()
	return filepath.Join(l.dir, name[:2], name[2:])
}

func (l looseObjects) Get(id hash.ObjectID) (object.Type, []byte, error) {
	obj, err := object.ReadLoose(l.path(id))
	if err != nil {
		return 0, nil, err
	}
	return obj.Type(), obj.Encode(), nil
}

func (l looseObjects) ResolveShort(prefix string) ([]hash.ObjectID, error) {
	var found []hash.ObjectID
	entries, err := os.ReadDir(l.dir)
	if err != nil {
		return nil, err
	}
	for _, fanout := range entries {
		names, err := os.ReadDir(filepath.Join(l.dir, fanout.Name()))
		if err != nil {
			return nil, err
		}
		for _, name := range names {
			id, err := hash.Parse(fanout.Name() + name.Name())
			if err != nil {
				return nil, err
			}
			if strings.HasPrefix(id.String(), prefix) {
				found = append(found, id)
			}
		}
	}
	return found, nil
}

type looseRepo struct {
	t       *testing.T
	dir     string
	objects looseObjects
	store   *refs.Store
	ids     map[string]hash.ObjectID
	clock   int64
}

func newLooseRepo(t *testing.T) *looseRepo {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "repo.git")
	objectsDir := filepath.Join(dir, "objects")
	if err := os.MkdirAll(objectsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll returned error %v", err)
	}
	repo := &looseRepo{
		t:       t,
		dir:     dir,
		objects: looseObjects{dir: objectsDir},
		ids:     make(map[string]hash.ObjectID),
		clock:   1700000000,
	}
	repo.writeFile("HEAD", "ref: refs/heads/main\n")
	store, err := refs.Open(refs.Options{GitDir: dir})
	if err != nil {
		t.Fatalf("refs.Open returned error %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	repo.store = store
	return repo
}

func (r *looseRepo) writeFile(name, content string) {
	r.t.Helper()
	path := filepath.Join(r.dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		r.t.Fatalf("MkdirAll returned error %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		r.t.Fatalf("WriteFile returned error %v", err)
	}
}

func (r *looseRepo) write(obj object.Object) hash.ObjectID {
	r.t.Helper()
	id, err := object.WriteLoose(r.objects.dir, obj)
	if err != nil {
		r.t.Fatalf("WriteLoose returned error %v", err)
	}
	return id
}

func (r *looseRepo) commit(name, content string, parents ...string) hash.ObjectID {
	r.t.Helper()
	blob := r.write(&object.Blob{Data: []byte(content)})
	tree := &object.Tree{Entries: []object.TreeEntry{{Mode: object.ModeBlob, Name: "file.txt", ID: blob}}}
	r.clock += 60
	when := object.Signature{Name: "ann", Email: "ann@example.com", When: timeAt(r.clock)}
	commit := &object.Commit{Tree: r.write(tree), Author: when, Committer: when, Message: name + "\n"}
	for _, parent := range parents {
		commit.Parents = append(commit.Parents, r.ids[parent])
	}
	id := r.write(commit)
	r.ids[name] = id
	return id
}

func (r *looseRepo) context() Context {
	return Context{Objects: r.objects, Refs: r.store}
}

func TestParseReadsARepositoryOnDisk(t *testing.T) {
	repo := newLooseRepo(t)
	repo.commit("one", "1")
	repo.commit("two", "2", "one")
	repo.commit("three", "3", "two")
	repo.writeFile("refs/heads/main", repo.ids["three"].String()+"\n")
	repo.writeFile("refs/heads/topic", repo.ids["two"].String()+"\n")
	tagger := object.Signature{Name: "ann", Email: "ann@example.com", When: timeAt(repo.clock)}
	tag := &object.Tag{
		Object:     repo.ids["two"],
		ObjectType: object.TypeCommit,
		Name:       "v1",
		Tagger:     &tagger,
		Message:    "release\n",
	}
	tagID := repo.write(tag)
	repo.writeFile("refs/tags/v1", tagID.String()+"\n")
	repo.writeFile("logs/HEAD", strings.Join([]string{
		zeroLine(repo.ids["one"], "commit (initial): one"),
		updateLine(repo.ids["one"], repo.ids["two"], "checkout: moving from main to topic"),
		updateLine(repo.ids["two"], repo.ids["three"], "checkout: moving from topic to main"),
	}, ""))
	ctx := repo.context()
	tests := []struct {
		spec string
		want hash.ObjectID
	}{
		{"HEAD", repo.ids["three"]},
		{"main", repo.ids["three"]},
		{"topic", repo.ids["two"]},
		{"v1", tagID},
		{"v1^{}", repo.ids["two"]},
		{"main~2", repo.ids["one"]},
		{"main^", repo.ids["two"]},
		{"@{-1}", repo.ids["two"]},
		{"HEAD@{1}", repo.ids["two"]},
		{":/two", repo.ids["two"]},
		{repo.ids["one"].String()[:7], repo.ids["one"]},
	}
	for _, test := range tests {
		t.Run(test.spec, func(t *testing.T) {
			rev, err := Parse(test.spec, ctx)
			if err != nil {
				t.Fatalf("Parse(%q) returned error %v", test.spec, err)
			}
			if rev.ID != test.want {
				t.Errorf("Parse(%q) resolved to %s, want %s", test.spec, rev.ID, test.want)
			}
		})
	}
	opts, err := Ranges([]string{"--all"}, ctx)
	if err != nil {
		t.Fatalf("Ranges returned error %v", err)
	}
	var walked []hash.ObjectID
	for commit, err := range Walk(t.Context(), opts) {
		if err != nil {
			t.Fatalf("Walk returned error %v", err)
		}
		walked = append(walked, commit.ID)
	}
	want := []hash.ObjectID{repo.ids["three"], repo.ids["two"], repo.ids["one"]}
	if !slices.Equal(walked, want) {
		t.Errorf("Walk visited %v, want %v", walked, want)
	}
}
