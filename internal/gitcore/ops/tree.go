package ops

import (
	"errors"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/refs"
)

type treeEntry struct {
	mode object.Mode
	id   hash.ObjectID
}

func resolveHeadCommit(store *refs.Store) (hash.ObjectID, error) {
	head, err := store.Resolve(refs.HEAD)
	if errors.Is(err, refs.ErrNotFound) {
		return hash.Zero, nil
	}
	if err != nil {
		return hash.Zero, err
	}
	return head.Target, nil
}

func commitTreeEntries(db *odb.DB, commitID hash.ObjectID) (map[string]treeEntry, error) {
	out := map[string]treeEntry{}
	if commitID.IsZero() {
		return out, nil
	}
	commit, err := db.Commit(commitID)
	if err != nil {
		return nil, err
	}
	if err := collectTree(db, commit.Tree, "", out); err != nil {
		return nil, err
	}
	return out, nil
}

func collectTree(db *odb.DB, id hash.ObjectID, prefix string, out map[string]treeEntry) error {
	if id.IsZero() {
		return nil
	}
	tree, err := db.Tree(id)
	if err != nil {
		return err
	}
	for _, entry := range tree.Entries {
		name := joinRel(prefix, entry.Name)
		if entry.Mode.IsTree() {
			if err := collectTree(db, entry.ID, name, out); err != nil {
				return err
			}
			continue
		}
		out[name] = treeEntry{mode: entry.Mode, id: entry.ID}
	}
	return nil
}
