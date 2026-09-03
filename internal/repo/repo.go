package repo

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"iter"
	"path/filepath"
	"slices"
	"strings"

	"github.com/oops1/gogit/internal/config"
)

type Kind int

const (
	KindGroup Kind = iota
	KindRepository
	KindWorktree
)

type Node struct {
	Kind     Kind
	ID       string
	Name     string
	Path     string
	Children []*Node
}

var (
	ErrNotFound      = errors.New("repo: not found")
	ErrCycle         = errors.New("repo: cycle")
	ErrDuplicatePath = errors.New("repo: duplicate path")
	ErrDuplicateID   = errors.New("repo: duplicate id")
	ErrKindMismatch  = errors.New("repo: kind mismatch")
)

var randRead = rand.Read

func newID() (string, error) {
	b := make([]byte, 8)
	if _, err := randRead(b); err != nil {
		return "", fmt.Errorf("repo: generate id: %w", err)
	}
	return hex.EncodeToString(b), nil
}

var absPath = filepath.Abs

func normalizePath(p string) (string, error) {
	abs, err := absPath(filepath.Clean(p))
	if err != nil {
		return "", fmt.Errorf("repo: normalize path: %w", err)
	}
	return abs, nil
}

type Registry struct {
	cfg      *config.Config
	roots    []*Node
	byID     map[string]*Node
	activeID string
}

func New(cfg *config.Config) *Registry {
	r := &Registry{cfg: cfg}
	r.Rebuild()
	return r
}

func (r *Registry) Rebuild() {
	r.roots, r.byID = buildTree(r.cfg)
}

func (r *Registry) Roots() []*Node { return r.roots }

func (r *Registry) Find(id string) (*Node, bool) {
	n, ok := r.byID[id]
	return n, ok
}

func (r *Registry) FindByPath(path string) (*Node, bool) {
	norm, err := normalizePath(path)
	if err != nil {
		return nil, false
	}
	for _, n := range r.byID {
		if n.Kind != KindGroup && n.Path == norm {
			return n, true
		}
	}
	return nil, false
}

func (r *Registry) Walk() iter.Seq[*Node] {
	return func(yield func(*Node) bool) {
		var walk func([]*Node) bool
		walk = func(nodes []*Node) bool {
			for _, n := range nodes {
				if !yield(n) {
					return false
				}
				if !walk(n.Children) {
					return false
				}
			}
			return true
		}
		walk(r.roots)
	}
}

func (r *Registry) SetActive(id string) error {
	if _, ok := r.byID[id]; !ok {
		return ErrNotFound
	}
	r.activeID = id
	return nil
}

func (r *Registry) Active() (*Node, bool) {
	if r.activeID == "" {
		return nil, false
	}
	n, ok := r.byID[r.activeID]
	return n, ok
}

func (r *Registry) ClearActive() {
	r.activeID = ""
}

func findGroupIndex(cfg *config.Config, id string) int {
	for i := range cfg.Groups {
		if cfg.Groups[i].ID == id {
			return i
		}
	}
	return -1
}

func findRepoIndex(cfg *config.Config, id string) int {
	for i := range cfg.Repositories {
		if cfg.Repositories[i].ID == id {
			return i
		}
	}
	return -1
}

func nodeContains(n *Node, id string) bool {
	for _, c := range n.Children {
		if c.ID == id || nodeContains(c, id) {
			return true
		}
	}
	return false
}

func (r *Registry) AddGroup(name, parent string) (*Node, error) {
	id, err := newID()
	if err != nil {
		return nil, err
	}
	g := config.Group{ID: id, Name: name, Parent: parent}
	if !r.cfg.AddGroup(g) {
		return nil, ErrDuplicateID
	}
	r.Rebuild()
	n, _ := r.Find(id)
	return n, nil
}

func (r *Registry) RenameGroup(id, name string) error {
	idx := findGroupIndex(r.cfg, id)
	if idx < 0 {
		return ErrNotFound
	}
	r.cfg.Groups[idx].Name = name
	r.Rebuild()
	return nil
}

func (r *Registry) MoveGroup(id, newParent string) error {
	idx := findGroupIndex(r.cfg, id)
	if idx < 0 {
		return ErrNotFound
	}
	if newParent != "" {
		if findGroupIndex(r.cfg, newParent) < 0 {
			return ErrNotFound
		}
	}
	if newParent == id {
		return ErrCycle
	}
	if node, ok := r.Find(id); ok && nodeContains(node, newParent) {
		return ErrCycle
	}
	r.cfg.Groups[idx].Parent = newParent
	r.Rebuild()
	return nil
}

func (r *Registry) RemoveGroup(id string) error {
	idx := findGroupIndex(r.cfg, id)
	if idx < 0 {
		return ErrNotFound
	}
	parent := r.cfg.Groups[idx].Parent
	for i := range r.cfg.Groups {
		if r.cfg.Groups[i].Parent == id {
			r.cfg.Groups[i].Parent = parent
		}
	}
	for i := range r.cfg.Repositories {
		if !r.cfg.Repositories[i].Worktree && r.cfg.Repositories[i].Group == id {
			r.cfg.Repositories[i].Group = parent
		}
	}
	r.cfg.RemoveGroup(id)
	r.Rebuild()
	return nil
}

func (r *Registry) AddRepository(name, path, group string) (*Node, error) {
	norm, err := normalizePath(path)
	if err != nil {
		return nil, err
	}
	if _, ok := r.FindByPath(norm); ok {
		return nil, ErrDuplicatePath
	}
	id, err := newID()
	if err != nil {
		return nil, err
	}
	rep := config.Repository{ID: id, Name: name, Path: norm, Group: group}
	if !r.cfg.AddRepository(rep) {
		return nil, ErrDuplicateID
	}
	r.Rebuild()
	n, _ := r.Find(id)
	return n, nil
}

func (r *Registry) RenameRepository(id, name string) error {
	idx := findRepoIndex(r.cfg, id)
	if idx < 0 {
		return ErrNotFound
	}
	r.cfg.Repositories[idx].Name = name
	r.Rebuild()
	return nil
}

func (r *Registry) MoveRepository(id, group string) error {
	idx := findRepoIndex(r.cfg, id)
	if idx < 0 {
		return ErrNotFound
	}
	if r.cfg.Repositories[idx].Worktree {
		return ErrKindMismatch
	}
	r.cfg.Repositories[idx].Group = group
	r.Rebuild()
	return nil
}

func (r *Registry) RemoveRepository(id string) error {
	node, ok := r.Find(id)
	if !ok {
		return ErrNotFound
	}
	if node.Kind == KindGroup {
		return ErrKindMismatch
	}
	ids := []string{id}
	var collect func(*Node)
	collect = func(n *Node) {
		for _, c := range n.Children {
			ids = append(ids, c.ID)
			collect(c)
		}
	}
	collect(node)
	for _, rid := range ids {
		r.cfg.RemoveRepository(rid)
	}
	r.Rebuild()
	return nil
}

func (r *Registry) AddWorktree(parentID, name, path string) (*Node, error) {
	parentNode, ok := r.Find(parentID)
	if !ok {
		return nil, ErrNotFound
	}
	if parentNode.Kind == KindGroup {
		return nil, ErrKindMismatch
	}
	norm, err := normalizePath(path)
	if err != nil {
		return nil, err
	}
	if _, ok := r.FindByPath(norm); ok {
		return nil, ErrDuplicatePath
	}
	id, err := newID()
	if err != nil {
		return nil, err
	}
	rep := config.Repository{ID: id, Name: name, Path: norm, Worktree: true, Parent: parentID}
	if !r.cfg.AddRepository(rep) {
		return nil, ErrDuplicateID
	}
	r.Rebuild()
	n, _ := r.Find(id)
	return n, nil
}

type treeEntry struct {
	node      *Node
	parentRef string
}

func buildTree(cfg *config.Config) ([]*Node, map[string]*Node) {
	entries := make(map[string]treeEntry, len(cfg.Groups)+len(cfg.Repositories))
	for _, g := range cfg.Groups {
		entries[g.ID] = treeEntry{
			node:      &Node{Kind: KindGroup, ID: g.ID, Name: g.Name},
			parentRef: g.Parent,
		}
	}
	for _, rp := range cfg.Repositories {
		kind := KindRepository
		parentRef := rp.Group
		if rp.Worktree {
			kind = KindWorktree
			parentRef = rp.Parent
		}
		entries[rp.ID] = treeEntry{
			node:      &Node{Kind: kind, ID: rp.ID, Name: rp.Name, Path: rp.Path},
			parentRef: parentRef,
		}
	}

	state := make(map[string]int, len(entries))
	actualParent := make(map[string]string, len(entries))
	var resolve func(id string)
	resolve = func(id string) {
		if state[id] == 2 {
			return
		}
		state[id] = 1
		ref := entries[id].parentRef
		_, known := entries[ref]
		if ref == "" || !known || state[ref] == 1 {
			actualParent[id] = ""
		} else {
			resolve(ref)
			actualParent[id] = ref
		}
		state[id] = 2
	}
	for id := range entries {
		resolve(id)
	}

	byID := make(map[string]*Node, len(entries))
	for id, e := range entries {
		byID[id] = e.node
	}

	var roots []*Node
	for id, e := range entries {
		parent := actualParent[id]
		if parent == "" {
			roots = append(roots, e.node)
		} else {
			byID[parent].Children = append(byID[parent].Children, e.node)
		}
	}

	sortNodes(roots)
	return roots, byID
}

func sortNodes(nodes []*Node) {
	slices.SortFunc(nodes, compareNodes)
	for _, n := range nodes {
		sortNodes(n.Children)
	}
}

func compareNodes(a, b *Node) int {
	if (a.Kind == KindGroup) != (b.Kind == KindGroup) {
		if a.Kind == KindGroup {
			return -1
		}
		return 1
	}
	if c := strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)); c != 0 {
		return c
	}
	return strings.Compare(a.ID, b.ID)
}
