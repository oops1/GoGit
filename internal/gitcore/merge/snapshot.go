package merge

import (
	"path"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
)

type TreeReader interface {
	Tree(id hash.ObjectID) (*object.Tree, error)
}

func Read(trees TreeReader, root hash.ObjectID) (Snapshot, error) {
	out := Snapshot{}
	if root.IsZero() {
		return out, nil
	}
	return out, readInto(trees, root, "", out)
}

func readInto(trees TreeReader, id hash.ObjectID, prefix string, out Snapshot) error {
	tree, err := trees.Tree(id)
	if err != nil {
		return err
	}
	for _, entry := range tree.Entries {
		name := path.Join(prefix, entry.Name)
		if entry.Mode.IsTree() {
			if err := readInto(trees, entry.ID, name, out); err != nil {
				return err
			}
			continue
		}
		out[name] = Entry{Mode: entry.Mode, ID: entry.ID}
	}
	return nil
}

func (s Snapshot) Write(objects Objects) (hash.ObjectID, error) {
	return writeDirectory(s.directories(), "", objects)
}

type directory struct {
	files   map[string]Entry
	subdirs map[string]bool
}

func (s Snapshot) directories() map[string]*directory {
	dirs := map[string]*directory{"": {files: map[string]Entry{}, subdirs: map[string]bool{}}}
	ensure := func(dir string) *directory {
		d, ok := dirs[dir]
		if !ok {
			d = &directory{files: map[string]Entry{}, subdirs: map[string]bool{}}
			dirs[dir] = d
		}
		return d
	}
	for name, entry := range s {
		dir, base := splitPath(name)
		ensure(dir).files[base] = entry
		for dir != "" {
			parent, child := splitPath(dir)
			ensure(parent).subdirs[child] = true
			dir = parent
		}
	}
	return dirs
}

func splitPath(name string) (string, string) {
	at := strings.LastIndexByte(name, '/')
	if at < 0 {
		return "", name
	}
	return name[:at], name[at+1:]
}

func writeDirectory(dirs map[string]*directory, dir string, objects Objects) (hash.ObjectID, error) {
	d := dirs[dir]
	tree := &object.Tree{}
	for name, entry := range d.files {
		tree.Entries = append(tree.Entries, object.TreeEntry{Mode: entry.Mode, Name: name, ID: entry.ID})
	}
	for name := range d.subdirs {
		id, err := writeDirectory(dirs, path.Join(dir, name), objects)
		if err != nil {
			return hash.Zero, err
		}
		tree.Entries = append(tree.Entries, object.TreeEntry{Mode: object.ModeTree, Name: name, ID: id})
	}
	tree.Sort()
	return objects.Put(object.TypeTree, tree.Encode())
}
