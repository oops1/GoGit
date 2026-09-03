package revision

import (
	"errors"
	"fmt"
	"iter"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/refs"
)

type storedObject struct {
	kind object.Type
	data []byte
}

type objectStore struct {
	data     map[hash.ObjectID]storedObject
	fail     map[hash.ObjectID]error
	shortErr error
	gets     int
}

func newObjectStore() *objectStore {
	return &objectStore{data: make(map[hash.ObjectID]storedObject), fail: make(map[hash.ObjectID]error)}
}

func (s *objectStore) Get(id hash.ObjectID) (object.Type, []byte, error) {
	s.gets++
	if err, ok := s.fail[id]; ok {
		return 0, nil, err
	}
	stored, ok := s.data[id]
	if !ok {
		return 0, nil, fmt.Errorf("object %s is missing", id)
	}
	return stored.kind, stored.data, nil
}

func (s *objectStore) ResolveShort(prefix string) ([]hash.ObjectID, error) {
	if s.shortErr != nil {
		return nil, s.shortErr
	}
	var found []hash.ObjectID
	for id := range s.data {
		if strings.HasPrefix(id.String(), strings.ToLower(prefix)) {
			found = append(found, id)
		}
	}
	slices.SortFunc(found, func(a, b hash.ObjectID) int { return a.Compare(b) })
	return found, nil
}

func (s *objectStore) put(obj object.Object) hash.ObjectID {
	id := obj.ID()
	s.data[id] = storedObject{kind: obj.Type(), data: obj.Encode()}
	return id
}

func (s *objectStore) putRaw(kind object.Type, data []byte) hash.ObjectID {
	id := hash.SumSHA1(kind.String(), data)
	s.data[id] = storedObject{kind: kind, data: data}
	return id
}

type plainObjects struct {
	inner *objectStore
}

func (p plainObjects) Get(id hash.ObjectID) (object.Type, []byte, error) { return p.inner.Get(id) }

type fakeRefs struct {
	values  map[refs.Name]refs.Ref
	logs    map[refs.Name][]refs.ReflogEntry
	err     error
	headErr error
	logErr  error
}

func newFakeRefs() *fakeRefs {
	return &fakeRefs{values: make(map[refs.Name]refs.Ref), logs: make(map[refs.Name][]refs.ReflogEntry)}
}

func (f *fakeRefs) lookup(name refs.Name) (refs.Ref, error) {
	if f.err != nil {
		return refs.Ref{}, f.err
	}
	if f.headErr != nil && name == refs.HEAD {
		return refs.Ref{}, f.headErr
	}
	ref, ok := f.values[name]
	if !ok {
		return refs.Ref{}, fmt.Errorf("%w: %s", refs.ErrNotFound, name)
	}
	return ref, nil
}

func (f *fakeRefs) ResolveName(name refs.Name) (refs.Name, error) {
	current := name
	for range 5 {
		ref, err := f.lookup(current)
		if errors.Is(err, refs.ErrNotFound) {
			return current, nil
		}
		if err != nil {
			return "", err
		}
		if !ref.IsSymbolic() {
			return current, nil
		}
		current = ref.SymbolicTarget
	}
	return "", refs.ErrTooManySymlinks
}

func (f *fakeRefs) Resolve(name refs.Name) (refs.Ref, error) {
	final, err := f.ResolveName(name)
	if err != nil {
		return refs.Ref{}, err
	}
	return f.lookup(final)
}

func (f *fakeRefs) Prefix(prefix string) iter.Seq2[refs.Ref, error] {
	return func(yield func(refs.Ref, error) bool) {
		if f.err != nil {
			yield(refs.Ref{}, f.err)
			return
		}
		for _, name := range slices.Sorted(maps.Keys(f.values)) {
			if !strings.HasPrefix(string(name), prefix) {
				continue
			}
			ref, err := f.Resolve(name)
			if err != nil {
				yield(refs.Ref{}, err)
				return
			}
			if !yield(refs.Ref{Name: name, Target: ref.Target}, nil) {
				return
			}
		}
	}
}

func (f *fakeRefs) Reflog(name refs.Name) iter.Seq2[refs.ReflogEntry, error] {
	return func(yield func(refs.ReflogEntry, error) bool) {
		if f.logErr != nil {
			yield(refs.ReflogEntry{}, f.logErr)
			return
		}
		for _, entry := range f.logs[name] {
			if !yield(entry, nil) {
				return
			}
		}
	}
}

type fakeConfig struct {
	upstream map[string]string
	push     map[string]string
}

func (c fakeConfig) Upstream(branch string) (string, bool) {
	target, ok := c.upstream[branch]
	return target, ok
}

type pushConfig struct {
	fakeConfig
}

func (c pushConfig) Push(branch string) (string, bool) {
	target, ok := c.push[branch]
	return target, ok
}

type builder struct {
	t       testing.TB
	objects *objectStore
	refs    *fakeRefs
	ids     map[string]hash.ObjectID
	files   map[string]map[string]string
	clock   int64
	author  int64
}

func newBuilder(t testing.TB) *builder {
	t.Helper()
	return &builder{
		t:       t,
		objects: newObjectStore(),
		refs:    newFakeRefs(),
		ids:     make(map[string]hash.ObjectID),
		files:   make(map[string]map[string]string),
		clock:   1700000000,
	}
}

func (b *builder) context() Context {
	return Context{Objects: b.objects, Refs: b.refs}
}

func (b *builder) id(name string) hash.ObjectID {
	b.t.Helper()
	id, ok := b.ids[name]
	if !ok {
		b.t.Fatalf("commit %q was not built", name)
	}
	return id
}

func (b *builder) signature(name string, when int64) object.Signature {
	return object.Signature{Name: name, Email: name + "@example.com", When: time.Unix(when, 0).UTC()}
}

func (b *builder) blob(content string) hash.ObjectID {
	return b.objects.put(&object.Blob{Data: []byte(content)})
}

func (b *builder) tree(files map[string]string) hash.ObjectID {
	entries := make([]object.TreeEntry, 0, len(files))
	dirs := make(map[string]map[string]string)
	for _, path := range slices.Sorted(maps.Keys(files)) {
		dir, rest, nested := strings.Cut(path, "/")
		if !nested {
			entries = append(entries, object.TreeEntry{
				Mode: object.ModeBlob,
				Name: path,
				ID:   b.blob(files[path]),
			})
			continue
		}
		if dirs[dir] == nil {
			dirs[dir] = make(map[string]string)
		}
		dirs[dir][rest] = files[path]
	}
	for _, dir := range slices.Sorted(maps.Keys(dirs)) {
		entries = append(entries, object.TreeEntry{Mode: object.ModeTree, Name: dir, ID: b.tree(dirs[dir])})
	}
	tree := &object.Tree{Entries: entries}
	tree.Sort()
	return b.objects.put(tree)
}

func (b *builder) commit(name string, parents ...string) hash.ObjectID {
	return b.commitFiles(name, nil, parents...)
}

func (b *builder) commitFiles(name string, files map[string]string, parents ...string) hash.ObjectID {
	b.t.Helper()
	if files == nil && len(parents) > 0 {
		files = b.files[parents[0]]
	}
	if files == nil {
		files = map[string]string{}
	}
	b.clock += 60
	when := b.clock
	authored := when
	if b.author != 0 {
		authored = b.author
		b.author = 0
	}
	commit := &object.Commit{
		Tree:      b.tree(files),
		Author:    b.signature("ann", authored),
		Committer: b.signature("cody", when),
		Message:   name + "\n",
	}
	for _, parent := range parents {
		commit.Parents = append(commit.Parents, b.id(parent))
	}
	id := b.objects.put(commit)
	b.ids[name] = id
	b.files[name] = files
	return id
}

func (b *builder) message(name, message string, parents ...string) hash.ObjectID {
	b.t.Helper()
	b.clock += 60
	commit := &object.Commit{
		Tree:      b.tree(b.filesOf(parents)),
		Author:    b.signature("ann", b.clock),
		Committer: b.signature("cody", b.clock),
		Message:   message,
	}
	for _, parent := range parents {
		commit.Parents = append(commit.Parents, b.id(parent))
	}
	id := b.objects.put(commit)
	b.ids[name] = id
	b.files[name] = b.filesOf(parents)
	return id
}

func (b *builder) filesOf(parents []string) map[string]string {
	if len(parents) == 0 {
		return map[string]string{}
	}
	return b.files[parents[0]]
}

func (b *builder) branch(short, commit string) {
	b.t.Helper()
	b.refs.values[refs.BranchName(short)] = refs.Ref{
		Name:   refs.BranchName(short),
		Target: b.id(commit),
	}
}

func (b *builder) remoteBranch(remote, short, commit string) {
	b.t.Helper()
	name := refs.RemoteBranchName(remote, short)
	b.refs.values[name] = refs.Ref{Name: name, Target: b.id(commit)}
}

func (b *builder) lightTag(short, commit string) {
	b.t.Helper()
	b.refs.values[refs.TagName(short)] = refs.Ref{Name: refs.TagName(short), Target: b.id(commit)}
}

func (b *builder) annotatedTag(short, commit string) hash.ObjectID {
	b.t.Helper()
	tagger := b.signature("ann", b.clock)
	tag := &object.Tag{
		Object:     b.id(commit),
		ObjectType: object.TypeCommit,
		Name:       short,
		Tagger:     &tagger,
		Message:    "tag " + short + "\n",
	}
	id := b.objects.put(tag)
	b.ids["tag:"+short] = id
	b.refs.values[refs.TagName(short)] = refs.Ref{Name: refs.TagName(short), Target: id}
	return id
}

func (b *builder) head(target refs.Name) {
	b.refs.values[refs.HEAD] = refs.Ref{Name: refs.HEAD, SymbolicTarget: target}
}

func (b *builder) detach(commit string) {
	b.t.Helper()
	b.refs.values[refs.HEAD] = refs.Ref{Name: refs.HEAD, Target: b.id(commit)}
}

func (b *builder) reflog(name refs.Name, entries ...refs.ReflogEntry) {
	b.refs.logs[name] = append(b.refs.logs[name], entries...)
}

func (b *builder) logEntry(old, current, message string) refs.ReflogEntry {
	b.t.Helper()
	entry := refs.ReflogEntry{Committer: b.signature("cody", b.clock), Message: message}
	if old != "" {
		entry.Old = b.id(old)
	}
	if current != "" {
		entry.New = b.id(current)
	}
	return entry
}

func names(t testing.TB, b *builder, ids []hash.ObjectID) []string {
	t.Helper()
	reverse := make(map[hash.ObjectID]string, len(b.ids))
	for name, id := range b.ids {
		reverse[id] = name
	}
	if len(ids) == 0 {
		return nil
	}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		name, ok := reverse[id]
		if !ok {
			name = id.String()
		}
		out = append(out, name)
	}
	return out
}

func collect(t testing.TB, b *builder, sequence iter.Seq2[*Commit, error]) []string {
	t.Helper()
	var ids []hash.ObjectID
	for commit, err := range sequence {
		if err != nil {
			t.Fatalf("Walk returned error %v", err)
		}
		ids = append(ids, commit.ID)
	}
	return names(t, b, ids)
}

func timeAt(seconds int64) time.Time {
	return time.Unix(seconds, 0).UTC()
}

func zeroLine(current hash.ObjectID, message string) string {
	return updateLine(hash.Zero, current, message)
}

func updateLine(old, current hash.ObjectID, message string) string {
	stamp := object.Signature{Name: "cody", Email: "cody@example.com", When: timeAt(1700000000)}
	return old.String() + " " + current.String() + " " + stamp.String() + "\t" + message + "\n"
}
