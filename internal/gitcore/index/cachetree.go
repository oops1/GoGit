package index

import (
	"bytes"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

type CacheTree struct {
	Path       string
	EntryCount int
	ID         hash.ObjectID
	Subtrees   []*CacheTree
}

func (t *CacheTree) Valid() bool {
	return t.EntryCount >= 0
}

func (t *CacheTree) Invalidate() {
	t.EntryCount = -1
}

func (t *CacheTree) Find(name string) *CacheTree {
	for _, sub := range t.Subtrees {
		if sub.Path == name {
			return sub
		}
	}
	return nil
}

func (t *CacheTree) Lookup(path string) *CacheTree {
	node := t
	for _, name := range strings.Split(path, "/") {
		if node = node.Find(name); node == nil {
			return nil
		}
	}
	return node
}

func (t *CacheTree) invalidatePath(path string) {
	t.Invalidate()
	name, rest, nested := strings.Cut(path, "/")
	if !nested {
		if at := slices.IndexFunc(t.Subtrees, func(sub *CacheTree) bool { return sub.Path == name }); at >= 0 {
			t.Subtrees = slices.Delete(t.Subtrees, at, at+1)
		}
		return
	}
	if sub := t.Find(name); sub != nil {
		sub.invalidatePath(rest)
	}
}

func (t *CacheTree) sortSubtrees() {
	slices.SortStableFunc(t.Subtrees, func(a, b *CacheTree) int {
		if len(a.Path) != len(b.Path) {
			return len(a.Path) - len(b.Path)
		}
		return strings.Compare(a.Path, b.Path)
	})
}

func parseCacheTree(data []byte) (*CacheTree, error) {
	root, pos, pending, err := parseCacheTreeNode(data, 0)
	if err != nil {
		return nil, err
	}
	if root.Path != "" {
		return nil, fmt.Errorf("%w: the cache tree root is named %q", ErrMalformed, root.Path)
	}
	type frame struct {
		node *CacheTree
		left int
	}
	stack := []frame{{node: root, left: pending}}
	for len(stack) > 0 {
		top := &stack[len(stack)-1]
		if top.left == 0 {
			stack = stack[:len(stack)-1]
			continue
		}
		top.left--
		child, next, subtrees, err := parseCacheTreeNode(data, pos)
		if err != nil {
			return nil, err
		}
		pos = next
		top.node.Subtrees = append(top.node.Subtrees, child)
		stack = append(stack, frame{node: child, left: subtrees})
	}
	if pos != len(data) {
		return nil, fmt.Errorf("%w: the cache tree leaves %d unread bytes", ErrMalformed, len(data)-pos)
	}
	return root, nil
}

func parseCacheTreeNode(data []byte, pos int) (*CacheTree, int, int, error) {
	name, next, err := readCString(data, pos)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("%w: cache tree path", err)
	}
	line, next, err := readLine(data, next)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("%w: cache tree counters of %q", err, name)
	}
	counts, subtreeText, found := strings.Cut(line, " ")
	if !found {
		return nil, 0, 0, fmt.Errorf("%w: cache tree counters %q of %q", ErrMalformed, line, name)
	}
	entries, err := strconv.Atoi(counts)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("%w: cache tree entry count %q of %q", ErrMalformed, counts, name)
	}
	subtrees, err := strconv.Atoi(subtreeText)
	if err != nil || subtrees < 0 {
		return nil, 0, 0, fmt.Errorf("%w: cache tree subtree count %q of %q", ErrMalformed, subtreeText, name)
	}
	node := &CacheTree{Path: name, EntryCount: entries}
	if entries < 0 {
		return node, next, subtrees, nil
	}
	if len(data)-next < hash.Size {
		return nil, 0, 0, fmt.Errorf("%w: cache tree object id of %q", ErrTruncated, name)
	}
	node.ID = hash.ObjectID(data[next : next+hash.Size])
	return node, next + hash.Size, subtrees, nil
}

func encodeCacheTree(root *CacheTree) []byte {
	var buf bytes.Buffer
	pending := []*CacheTree{root}
	for len(pending) > 0 {
		node := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		buf.WriteString(node.Path)
		buf.WriteByte(0)
		buf.WriteString(strconv.Itoa(node.EntryCount))
		buf.WriteByte(' ')
		buf.WriteString(strconv.Itoa(len(node.Subtrees)))
		buf.WriteByte('\n')
		if node.Valid() {
			buf.Write(node.ID[:])
		}
		for at := len(node.Subtrees) - 1; at >= 0; at-- {
			pending = append(pending, node.Subtrees[at])
		}
	}
	return buf.Bytes()
}

func readCString(data []byte, pos int) (string, int, error) {
	return readUntil(data, pos, 0)
}

func readLine(data []byte, pos int) (string, int, error) {
	return readUntil(data, pos, '\n')
}

func readUntil(data []byte, pos int, terminator byte) (string, int, error) {
	end := -1
	if pos <= len(data) {
		end = bytes.IndexByte(data[pos:], terminator)
	}
	if end < 0 {
		return "", 0, ErrTruncated
	}
	return string(data[pos : pos+end]), pos + end + 1, nil
}
