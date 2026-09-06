package remote

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/pack"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/gitcore/transport"
)

func mustMkdirAll(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(path, 0o777); err != nil {
		t.Fatalf("MkdirAll(%q) returned error %v", path, err)
	}
	return path
}

func mustWriteFile(t *testing.T, path string, text string) string {
	t.Helper()
	mustMkdirAll(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(text), 0o666); err != nil {
		t.Fatalf("WriteFile(%q) returned error %v", path, err)
	}
	return path
}

func newTestRepo(t *testing.T, globalConfig string) *repo.Repository {
	t.Helper()
	base := t.TempDir()
	mustMkdirAll(t, filepath.Join(base, "objects", "pack"))
	mustMkdirAll(t, filepath.Join(base, "refs", "heads"))
	mustWriteFile(t, filepath.Join(base, "HEAD"), "ref: refs/heads/master\n")
	mustWriteFile(t, filepath.Join(base, "config"), "[core]\n\trepositoryformatversion = 0\n\tbare = true\n")
	global := mustWriteFile(t, filepath.Join(t.TempDir(), "gitconfig"), globalConfig)
	layout := repo.Layout{GitDir: base, CommonDir: base, Bare: true}
	r, err := repo.OpenLayout(layout, repo.OpenOptions{NoSystem: true, GlobalFile: global})
	if err != nil {
		t.Fatalf("OpenLayout returned error %v", err)
	}
	t.Cleanup(func() {
		if err := r.Close(); err != nil {
			t.Errorf("Close returned error %v", err)
		}
	})
	return r
}

func openTestODB(t *testing.T, r *repo.Repository) *odb.DB {
	t.Helper()
	db, err := odb.Open(r.ObjectsDir(), odb.Options{Format: r.ObjectFormat})
	if err != nil {
		t.Fatalf("odb.Open returned error %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("db.Close returned error %v", err)
		}
	})
	return db
}

func openTestRefs(t *testing.T, r *repo.Repository, db *odb.DB) *refs.Store {
	t.Helper()
	store, err := refs.Open(refs.Options{
		GitDir:    r.GitDir(),
		CommonDir: r.CommonDir(),
		Bare:      r.IsBare(),
		Peeler:    db,
		Committer: func() object.Signature { return testSignature(time.Unix(1_700_000_000, 0).UTC()) },
	})
	if err != nil {
		t.Fatalf("refs.Open returned error %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("store.Close returned error %v", err)
		}
	})
	return store
}

func testSignature(when time.Time) object.Signature {
	return object.Signature{Name: "Test", Email: "test@example.com", When: when}
}

func encodeCommit(when time.Time, message string, parents ...hash.ObjectID) (hash.ObjectID, []byte) {
	commit := &object.Commit{
		Tree:      hash.Zero,
		Parents:   parents,
		Author:    testSignature(when),
		Committer: testSignature(when),
		Message:   message,
	}
	data := commit.Encode()
	id := hash.SumSHA1(object.TypeCommit.String(), data)
	return id, data
}

func putLocalCommit(t *testing.T, db *odb.DB, when time.Time, message string, parents ...hash.ObjectID) hash.ObjectID {
	t.Helper()
	id, data := encodeCommit(when, message, parents...)
	stored, err := db.Put(object.TypeCommit, data)
	if err != nil {
		t.Fatalf("db.Put returned error %v", err)
	}
	if stored != id {
		t.Fatalf("db.Put stored %s, want %s", stored, id)
	}
	return id
}

func setLocalBranch(t *testing.T, store *refs.Store, name refs.Name, id hash.ObjectID) {
	t.Helper()
	tx := store.Begin()
	if err := tx.Set(name, id); err != nil {
		t.Fatalf("Set(%s) returned error %v", name, err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit returned error %v", err)
	}
}

func setLocalHead(t *testing.T, store *refs.Store, target refs.Name) {
	t.Helper()
	tx := store.Begin()
	if err := tx.SetSymbolic(refs.HEAD, target); err != nil {
		t.Fatalf("SetSymbolic(HEAD) returned error %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit returned error %v", err)
	}
}

type storedObject struct {
	kind object.Type
	data []byte
}

type fakeObjectStore struct {
	objs map[hash.ObjectID]storedObject
}

func newFakeObjectStore() *fakeObjectStore {
	return &fakeObjectStore{objs: make(map[hash.ObjectID]storedObject)}
}

func (s *fakeObjectStore) Get(id hash.ObjectID) (object.Type, []byte, error) {
	obj, ok := s.objs[id]
	if !ok {
		return 0, nil, errors.New("fakeObjectStore: unknown object " + id.String())
	}
	return obj.kind, obj.data, nil
}

func (s *fakeObjectStore) putCommit(when time.Time, message string, parents ...hash.ObjectID) hash.ObjectID {
	id, data := encodeCommit(when, message, parents...)
	s.objs[id] = storedObject{kind: object.TypeCommit, data: data}
	return id
}

func (s *fakeObjectStore) putTag(when time.Time, name string, target hash.ObjectID) hash.ObjectID {
	tag := &object.Tag{
		Object:     target,
		ObjectType: object.TypeCommit,
		Name:       name,
		Tagger:     &object.Signature{Name: "Test", Email: "test@example.com", When: when},
		Message:    name,
	}
	data := tag.Encode()
	id := hash.SumSHA1(object.TypeTag.String(), data)
	s.objs[id] = storedObject{kind: object.TypeTag, data: data}
	return id
}

func (s *fakeObjectStore) buildPack(t *testing.T, ids []hash.ObjectID) []byte {
	t.Helper()
	var buf bytes.Buffer
	if _, err := pack.WritePack(context.Background(), &buf, s, ids, pack.WriteOptions{}); err != nil {
		t.Fatalf("WritePack returned error %v", err)
	}
	return buf.Bytes()
}

type fakeSession struct {
	adv       transport.Advertisement
	advErr    error
	fetchErr  error
	fetchFunc func(context.Context, transport.FetchRequest, transport.Negotiator) (*transport.FetchResponse, error)
	pushFunc  func(context.Context, transport.PushRequest) (*transport.PushResult, error)
	closed    bool
	lastReq   transport.FetchRequest
	haves     []hash.ObjectID
}

func (s *fakeSession) Advertise(context.Context) (transport.Advertisement, error) {
	if s.advErr != nil {
		return transport.Advertisement{}, s.advErr
	}
	return s.adv, nil
}

func (s *fakeSession) Fetch(ctx context.Context, req transport.FetchRequest, neg transport.Negotiator) (*transport.FetchResponse, error) {
	s.lastReq = req
	if neg != nil {
		for id, err := range neg.Haves(ctx) {
			if err != nil {
				break
			}
			s.haves = append(s.haves, id)
		}
	}
	if s.fetchErr != nil {
		return nil, s.fetchErr
	}
	if s.fetchFunc != nil {
		return s.fetchFunc(ctx, req, neg)
	}
	return nil, errors.New("fakeSession: no Fetch behavior configured")
}

func (s *fakeSession) Push(ctx context.Context, req transport.PushRequest) (*transport.PushResult, error) {
	if s.pushFunc != nil {
		return s.pushFunc(ctx, req)
	}
	return nil, errors.New("fakeSession: Push not supported")
}

func (s *fakeSession) Close() error {
	s.closed = true
	return nil
}

func withDial(t *testing.T, session transport.Session, err error) *fakeSession {
	t.Helper()
	previous := dial
	dial = func(context.Context, string, transport.Service, transport.Options) (transport.Session, error) {
		if err != nil {
			return nil, err
		}
		return session, nil
	}
	t.Cleanup(func() { dial = previous })
	fake, _ := session.(*fakeSession)
	return fake
}

func withOdbOpenError(t *testing.T, err error) {
	t.Helper()
	previous := odbOpen
	odbOpen = func(string, odb.Options) (*odb.DB, error) { return nil, err }
	t.Cleanup(func() { odbOpen = previous })
}

func withRefsOpenError(t *testing.T, err error) {
	t.Helper()
	previous := refsOpen
	refsOpen = func(refs.Options) (*refs.Store, error) { return nil, err }
	t.Cleanup(func() { refsOpen = previous })
}

type fakeStore struct {
	has        map[hash.ObjectID]bool
	hasErr     error
	peels      map[hash.ObjectID]hash.ObjectID
	types      map[hash.ObjectID]object.Type
	peelErr    error
	peelErrFor map[hash.ObjectID]error
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		has:        make(map[hash.ObjectID]bool),
		peels:      make(map[hash.ObjectID]hash.ObjectID),
		types:      make(map[hash.ObjectID]object.Type),
		peelErrFor: make(map[hash.ObjectID]error),
	}
}

func (s *fakeStore) Get(hash.ObjectID) (object.Type, []byte, error) {
	return 0, nil, errors.New("fakeStore: Get not supported")
}

func (s *fakeStore) Has(id hash.ObjectID) (bool, error) {
	if s.hasErr != nil {
		return false, s.hasErr
	}
	return s.has[id], nil
}

func (s *fakeStore) Peel(id hash.ObjectID) (object.Type, hash.ObjectID, error) {
	if s.peelErr != nil {
		return 0, hash.Zero, s.peelErr
	}
	if err, ok := s.peelErrFor[id]; ok {
		return 0, hash.Zero, err
	}
	kind, ok := s.types[id]
	if !ok {
		kind = object.TypeCommit
	}
	target, ok := s.peels[id]
	if !ok {
		target = id
	}
	return kind, target, nil
}
