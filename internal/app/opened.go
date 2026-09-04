package app

import (
	"errors"
	"sync"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/refs"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/gitcore/worktree"
	"github.com/oops1/gogit/internal/ui/branches"
)

const worktreeMaxFiles = 200000

type openedRepository struct {
	repo    *gitrepo.Repository
	store   *refs.Store
	db      *odb.DB
	path    string
	id      string
	shallow map[hash.ObjectID]struct{}

	wtMu     sync.RWMutex
	worktree *worktree.Worktree
}

func (o *openedRepository) currentWorktree() *worktree.Worktree {
	o.wtMu.RLock()
	defer o.wtMu.RUnlock()
	return o.worktree
}

func (o *openedRepository) swapWorktree(fresh *worktree.Worktree) *worktree.Worktree {
	o.wtMu.Lock()
	defer o.wtMu.Unlock()
	stale := o.worktree
	o.worktree = fresh
	return stale
}

var openGitRepository = gitrepo.Open

var readRepositoryShallow = (*gitrepo.Repository).Shallow

var openObjectsDB = odb.Open

var openRefsStore = refs.Open

var openWorktree = worktree.Open

var loadBranchSnapshot = branches.Load

var closeRefsStore = (*refs.Store).Close

var closeObjectsDB = (*odb.DB).Close

var closeGitRepository = (*gitrepo.Repository).Close

var closeWorktree = (*worktree.Worktree).Close

func openRepositoryAt(id, path string) (*openedRepository, branches.Snapshot, error) {
	r, err := openGitRepository(path, gitrepo.OpenOptions{})
	if err != nil {
		return nil, branches.Snapshot{}, err
	}
	shallow, err := readRepositoryShallow(r)
	if err != nil {
		_ = closeGitRepository(r)
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
	wt, err := openWorktree(r, worktree.Options{DB: db, Refs: store, MaxFiles: worktreeMaxFiles})
	if err != nil && !errors.Is(err, worktree.ErrBareRepository) {
		_ = closeRefsStore(store)
		_ = closeObjectsDB(db)
		_ = closeGitRepository(r)
		return nil, branches.Snapshot{}, err
	}
	if errors.Is(err, worktree.ErrBareRepository) {
		wt = nil
	}
	return &openedRepository{repo: r, store: store, db: db, worktree: wt, path: path, id: id, shallow: shallow}, snap, nil
}

func (o *openedRepository) close() error {
	var errs []error
	if wt := o.currentWorktree(); wt != nil {
		errs = append(errs, closeWorktree(wt))
	}
	errs = append(errs, closeRefsStore(o.store), closeObjectsDB(o.db), closeGitRepository(o.repo))
	return errors.Join(errs...)
}
