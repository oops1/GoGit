package local

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/gitcore/transport"
)

const (
	defaultCommitterName  = "gogit"
	defaultCommitterEmail = "gogit@localhost"
)

var (
	repoOpen = repo.Open
	odbOpen  = odb.Open
	refsOpen = refs.Open
)

type session struct {
	mu      sync.Mutex
	path    string
	service transport.Service
	opts    transport.Options
	repo    *repo.Repository
	db      *odb.DB
	refs    *refs.Store
	closed  bool
}

func Dial(ctx context.Context, path string, service transport.Service, opts transport.Options) (transport.Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r, err := openRepository(path)
	if err != nil {
		return nil, err
	}
	db, err := odbOpen(r.ObjectsDir(), odb.Options{Format: r.ObjectFormat})
	if err != nil {
		_ = r.Close()
		return nil, fmt.Errorf("local: open object database of %s: %w", path, err)
	}
	store, err := refsOpen(refs.Options{
		GitDir:    r.GitDir(),
		CommonDir: r.CommonDir(),
		Bare:      r.IsBare(),
		Peeler:    db,
		Committer: committerFunc(r),
	})
	if err != nil {
		_ = db.Close()
		_ = r.Close()
		return nil, fmt.Errorf("local: open references of %s: %w", path, err)
	}
	return &session{path: path, service: service, opts: opts, repo: r, db: db, refs: store}, nil
}

func openRepository(path string) (*repo.Repository, error) {
	r, err := repoOpen(path, repo.OpenOptions{})
	if err != nil {
		if errors.Is(err, repo.ErrInvalidPath) {
			return nil, fmt.Errorf("%w: %s: %w", ErrInvalidPath, path, err)
		}
		return nil, fmt.Errorf("%w: %s: %w", transport.ErrRepositoryNotFound, path, err)
	}
	return r, nil
}

func committerFunc(r *repo.Repository) func() object.Signature {
	return func() object.Signature {
		user := r.Config().User()
		name, email := user.Name, user.Email
		if name == "" {
			name = defaultCommitterName
		}
		if email == "" {
			email = defaultCommitterEmail
		}
		return object.Signature{Name: name, Email: email, When: time.Now()}
	}
}

func (s *session) checkOpen(ctx context.Context) error {
	if s.closed {
		return fmt.Errorf("%w: session is closed", transport.ErrProtocol)
	}
	return ctx.Err()
}

func (s *session) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	return errors.Join(s.refs.Close(), s.db.Close(), s.repo.Close())
}
