package ops

import (
	"errors"
	"os"
	"os/user"
	"strings"
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

var accountIdentity = systemAccountIdentity

func systemAccountIdentity() (string, string) {
	name := "gogit"
	if account, err := user.Current(); err == nil {
		switch {
		case account.Name != "":
			name = account.Name
		case account.Username != "":
			name = account.Username
		}
	}
	host := "localhost"
	if reported, err := os.Hostname(); err == nil && reported != "" {
		host = reported
	}
	return name, strings.ReplaceAll(name, " ", ".") + "@" + host
}

func fallbackIdentity(when time.Time) object.Signature {
	name, email := accountIdentity()
	return object.Signature{Name: name, Email: email, When: when}
}

func reflogIdentity(r *repo.Repository, when time.Time) object.Signature {
	sig, err := identityOf(r, when)
	if err != nil {
		return fallbackIdentity(when)
	}
	return sig
}

func openRepoContext(r *repo.Repository) (*repoContext, error) {
	now := time.Now()
	sig, err := identityOf(r, now)
	committer := reflogIdentity(r, now)
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
		Committer: func() object.Signature { return committer },
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
