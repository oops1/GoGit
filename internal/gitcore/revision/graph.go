package revision

import (
	"container/heap"

	"github.com/oops1/gogit/internal/gitcore/commitgraph"
	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
)

type nodeFlags uint16

const (
	flagSeen nodeFlags = 1 << iota
	flagUninteresting
	flagTreeSame
	flagShown
	flagParent1
	flagParent2
	flagStale
	flagResult
	flagShallow
	flagExplored
	flagIndegree
)

type node struct {
	id         hash.ObjectID
	commit     *object.Commit
	original   []hash.ObjectID
	parents    []hash.ObjectID
	tree       hash.ObjectID
	when       int64
	generation uint64
	position   commitgraph.Position
	inGraph    bool
	loaded     bool
	degree     int
	flags      nodeFlags
}

type graph struct {
	store *store
	nodes map[hash.ObjectID]*node
}

func newGraph(objects *store) *graph {
	return &graph{store: objects, nodes: make(map[hash.ObjectID]*node)}
}

func (g *graph) node(id hash.ObjectID) *node {
	if existing, ok := g.nodes[id]; ok {
		return existing
	}
	created := &node{id: id}
	g.nodes[id] = created
	return created
}

func (g *graph) load(n *node) error {
	if n.loaded {
		return nil
	}
	if pos, ok := g.store.inGraph(n.id); ok {
		entry := g.store.graph.Entry(pos)
		n.tree, n.when, n.generation = entry.Tree, entry.Time, entry.Generation
		n.original = make([]hash.ObjectID, len(entry.Parents))
		for at, parent := range entry.Parents {
			n.original[at] = g.store.graph.ID(parent)
		}
		n.position, n.inGraph = pos, true
	} else {
		commit, err := g.store.commit(n.id)
		if err != nil {
			return err
		}
		n.commit = commit
		n.tree, n.when, n.original = commit.Tree, commit.Committer.When.Unix(), commit.Parents
		n.generation = commitgraph.GenerationInfinity
	}
	n.parents = n.original
	if g.store.isShallow(n.id) {
		n.parents = nil
		n.flags |= flagShallow
	}
	n.loaded = true
	return nil
}

func (g *graph) full(n *node) (*object.Commit, error) {
	if n.commit != nil {
		return n.commit, nil
	}
	commit, err := g.store.commit(n.id)
	if err != nil {
		return nil, err
	}
	n.commit = commit
	return commit, nil
}

func (g *graph) commit(id hash.ObjectID) (*node, error) {
	n := g.node(id)
	if err := g.load(n); err != nil {
		return nil, err
	}
	return n, nil
}

type entry struct {
	node *node
	seq  uint64
}

type queue struct {
	items  []entry
	before func(a, b entry) bool
	seq    uint64
}

func newQueue(before func(a, b entry) bool) *queue {
	return &queue{before: before}
}

func (q *queue) Len() int { return len(q.items) }

func (q *queue) Less(i, j int) bool { return q.before(q.items[i], q.items[j]) }

func (q *queue) Swap(i, j int) { q.items[i], q.items[j] = q.items[j], q.items[i] }

func (q *queue) Push(item any) {
	added, _ := item.(entry)
	q.items = append(q.items, added)
}

func (q *queue) Pop() any {
	last := len(q.items) - 1
	removed := q.items[last]
	q.items = q.items[:last]
	return removed
}

func (q *queue) push(n *node) {
	q.seq++
	heap.Push(q, entry{node: n, seq: q.seq})
}

func (q *queue) pop() *node {
	removed, _ := heap.Pop(q).(entry)
	return removed.node
}

func (q *queue) head() *node { return q.items[0].node }

func byCommitDate(a, b entry) bool {
	if a.node.when != b.node.when {
		return a.node.when > b.node.when
	}
	return a.seq < b.seq
}

func byGenerationThenDate(a, b entry) bool {
	if a.node.generation != b.node.generation {
		return a.node.generation > b.node.generation
	}
	return byCommitDate(a, b)
}

func byAuthorDate(a, b entry) bool {
	first, second := a.node.commit.Author.When, b.node.commit.Author.When
	if !first.Equal(second) {
		return first.After(second)
	}
	return a.seq < b.seq
}

func byInsertion(a, b entry) bool { return a.seq > b.seq }
