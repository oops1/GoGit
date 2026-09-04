package ops

import (
	"errors"
	"time"

	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/repo"
)

type repoContext struct {
	db   *odb.DB
	refs *refs.Store
	sig  object.Signature
	repo *repo.Repository
}

func identityOf(r *repo.Repository, when time.Time) (object.Signature, error) {
	user := r.Config().User()
	if user.Name == "" || user.Email == "" {
		return object.Signature{}, ErrMissingIdentity
	}
	return object.Signature{Name: user.Name, Email: user.Email, When: when}, nil
}

func openRepoContext(r *repo.Repository) (*repoContext, error) {
	sig, err := identityOf(r, time.Now())
	if err != nil {
		sig = object.Signature{}
	}
	db, err := odbOpen(r.ObjectsDir(), odb.Options{Format: r.ObjectFormat})
	if err != nil {
		return nil, err
	}
	store, err := refsOpen(refs.Options{
		GitDir:    r.GitDir(),
		CommonDir: r.CommonDir(),
		Bare:      r.IsBare(),
		Peeler:    db,
		Committer: func() object.Signature { return sig },
	})
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return &repoContext{db: db, refs: store, sig: sig, repo: r}, nil
}

func (rc *repoContext) requireIdentity() error {
	if rc.sig.Name == "" || rc.sig.Email == "" {
		return ErrMissingIdentity
	}
	return nil
}

func (rc *repoContext) close() error {
	return errors.Join(rc.refs.Close(), rc.db.Close())
}
