package revision

import (
	"fmt"
	"iter"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/refs"
)

type Objects interface {
	Get(id hash.ObjectID) (object.Type, []byte, error)
}

type PrefixResolver interface {
	ResolveShort(prefix string) ([]hash.ObjectID, error)
}

type Refs interface {
	Resolve(name refs.Name) (refs.Ref, error)
	ResolveName(name refs.Name) (refs.Name, error)
	Prefix(prefix string) iter.Seq2[refs.Ref, error]
	Reflog(name refs.Name) iter.Seq2[refs.ReflogEntry, error]
}

type Config interface {
	Upstream(branch string) (string, bool)
}

type PushRemotes interface {
	Push(branch string) (string, bool)
}

type Context struct {
	Objects Objects
	Refs    Refs
	Config  Config
	Head    refs.Name
}

type Rev struct {
	ID   hash.ObjectID
	Type object.Type
	Ref  refs.Name
}

type pathKey struct {
	tree hash.ObjectID
	path string
}

type pathValue struct {
	mode  object.Mode
	id    hash.ObjectID
	found bool
}

type store struct {
	objects Objects
	commits map[hash.ObjectID]*object.Commit
	trees   map[hash.ObjectID]*object.Tree
	paths   map[pathKey]pathValue
}

func newStore(objects Objects) *store {
	return &store{
		objects: objects,
		commits: make(map[hash.ObjectID]*object.Commit),
		trees:   make(map[hash.ObjectID]*object.Tree),
		paths:   make(map[pathKey]pathValue),
	}
}

func (s *store) object(id hash.ObjectID) (object.Type, object.Object, error) {
	if cached, ok := s.commits[id]; ok {
		return object.TypeCommit, cached, nil
	}
	if cached, ok := s.trees[id]; ok {
		return object.TypeTree, cached, nil
	}
	kind, data, err := s.objects.Get(id)
	if err != nil {
		return 0, nil, fmt.Errorf("%w: object %s: %w", ErrNotFound, id, err)
	}
	parsed, err := object.Parse(kind, data)
	if err != nil {
		return 0, nil, fmt.Errorf("revision: object %s: %w", id, err)
	}
	switch typed := parsed.(type) {
	case *object.Commit:
		s.commits[id] = typed
	case *object.Tree:
		s.trees[id] = typed
	}
	return kind, parsed, nil
}

func (s *store) commit(id hash.ObjectID) (*object.Commit, error) {
	kind, parsed, err := s.object(id)
	if err != nil {
		return nil, err
	}
	commit, ok := parsed.(*object.Commit)
	if !ok {
		return nil, fmt.Errorf("%w: %s is a %s", ErrNotCommit, id, kind)
	}
	return commit, nil
}

func (s *store) tree(id hash.ObjectID) (*object.Tree, error) {
	kind, parsed, err := s.object(id)
	if err != nil {
		return nil, err
	}
	tree, ok := parsed.(*object.Tree)
	if !ok {
		return nil, fmt.Errorf("%w: %s is a %s, not a tree", ErrNotFound, id, kind)
	}
	return tree, nil
}

func (s *store) lookup(root hash.ObjectID, path string) (pathValue, error) {
	key := pathKey{tree: root, path: path}
	if cached, ok := s.paths[key]; ok {
		return cached, nil
	}
	value := pathValue{mode: object.ModeTree, id: root, found: true}
	for _, part := range strings.Split(path, "/") {
		if part == "" {
			continue
		}
		if !value.mode.IsTree() {
			value = pathValue{}
			break
		}
		tree, err := s.tree(value.id)
		if err != nil {
			return pathValue{}, err
		}
		entry, ok := tree.Find(part)
		if !ok {
			value = pathValue{}
			break
		}
		value = pathValue{mode: entry.Mode, id: entry.ID, found: true}
	}
	s.paths[key] = value
	return value, nil
}
