package console

import (
	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/revision"
	"github.com/oops1/gogit/internal/ui/branches"
)

func (e Env) resolve(spec string) (hash.ObjectID, error) {
	db, err := e.openObjects()
	if err != nil {
		return hash.Zero, err
	}
	defer func() { _ = db.Close() }()
	store, err := e.openRefs()
	if err != nil {
		return hash.Zero, err
	}
	defer func() { _ = store.Close() }()
	rev, err := revision.Parse(spec, revision.Context{Objects: db, Refs: store})
	if err != nil {
		return hash.Zero, err
	}
	return rev.ID, nil
}

func (e Env) snapshot() (branches.Snapshot, error) {
	store, err := e.openRefs()
	if err != nil {
		return branches.Snapshot{}, err
	}
	defer func() { _ = store.Close() }()
	return branches.Load(store)
}

func commitSubject(db *odb.DB, id hash.ObjectID) string {
	commit, err := db.Commit(id)
	if err != nil {
		return ""
	}
	return subjectOf(commit.Message)
}
