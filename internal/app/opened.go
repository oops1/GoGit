package app

import (
	"errors"

	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/refs"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/ui/branches"
)

type openedRepository struct {
	repo  *gitrepo.Repository
	store *refs.Store
	db    *odb.DB
	path  string
	id    string
}

var openGitRepository = gitrepo.Open

var openObjectsDB = odb.Open

var openRefsStore = refs.Open

var loadBranchSnapshot = branches.Load

var closeRefsStore = (*refs.Store).Close

var closeObjectsDB = (*odb.DB).Close

var closeGitRepository = (*gitrepo.Repository).Close

func openRepositoryAt(id, path string) (*openedRepository, branches.Snapshot, error) {
	r, err := openGitRepository(path, gitrepo.OpenOptions{})
	if err != nil {
		return nil, branches.Snapshot{}, err
	}
	db, err := openObjectsDB(r.ObjectsDir(), odb.Options{})
	if err != nil {
		_ = closeGitRepository(r)
		return nil, branches.Snapshot{}, err
	}
	store, err := openRefsStore(refs.Options{GitDir: r.GitDir(), CommonDir: r.CommonDir()})
	if err != nil {
		_ = closeObjectsDB(db)
		_ = closeGitRepository(r)
		return nil, branches.Snapshot{}, err
	}
	snap, err := loadBranchSnapshot(store)
	if err != nil {
		_ = closeRefsStore(store)
		_ = closeObjectsDB(db)
		_ = closeGitRepository(r)
		return nil, branches.Snapshot{}, err
	}
	return &openedRepository{repo: r, store: store, db: db, path: path, id: id}, snap, nil
}

func (o *openedRepository) close() error {
	return errors.Join(closeRefsStore(o.store), closeObjectsDB(o.db), closeGitRepository(o.repo))
}
