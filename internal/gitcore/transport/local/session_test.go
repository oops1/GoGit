package local

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/gitcore/transport"
)

func TestDialOpensBareRepository(t *testing.T) {
	src := newTestRepo(t, true)
	sess, err := Dial(t.Context(), src.dir, transport.UploadPack, transport.Options{})
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	t.Cleanup(func() { _ = sess.Close() })
}

func TestDialOpensWorktreeRepository(t *testing.T) {
	src := newTestRepo(t, false)
	sess, err := Dial(t.Context(), src.dir, transport.UploadPack, transport.Options{})
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	t.Cleanup(func() { _ = sess.Close() })
}

func TestDialFailsForMissingPath(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	_, err := Dial(t.Context(), missing, transport.UploadPack, transport.Options{})
	if !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("Dial returned %v, want ErrInvalidPath", err)
	}
}

func TestDialFailsForNonRepository(t *testing.T) {
	dir := t.TempDir()
	_, err := Dial(t.Context(), dir, transport.UploadPack, transport.Options{})
	if !errors.Is(err, transport.ErrRepositoryNotFound) {
		t.Fatalf("Dial returned %v, want ErrRepositoryNotFound", err)
	}
}

func TestDialRespectsCanceledContext(t *testing.T) {
	src := newTestRepo(t, true)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := Dial(ctx, src.dir, transport.UploadPack, transport.Options{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Dial returned %v, want context.Canceled", err)
	}
}

func TestDialFailsWhenObjectDatabaseCannotOpen(t *testing.T) {
	src := newTestRepo(t, true)
	original := odbOpen
	t.Cleanup(func() { odbOpen = original })
	wantErr := errors.New("boom")
	odbOpen = func(string, odb.Options) (*odb.DB, error) {
		return nil, wantErr
	}
	if _, err := Dial(t.Context(), src.dir, transport.UploadPack, transport.Options{}); !errors.Is(err, wantErr) {
		t.Fatalf("Dial returned %v, want wrapping %v", err, wantErr)
	}
}

func TestDialFailsWhenRefsCannotOpen(t *testing.T) {
	src := newTestRepo(t, true)
	original := refsOpen
	t.Cleanup(func() { refsOpen = original })
	wantErr := errors.New("boom")
	refsOpen = func(refs.Options) (*refs.Store, error) {
		return nil, wantErr
	}
	if _, err := Dial(t.Context(), src.dir, transport.UploadPack, transport.Options{}); !errors.Is(err, wantErr) {
		t.Fatalf("Dial returned %v, want wrapping %v", err, wantErr)
	}
}

func TestOpenRepositoryWrapsUnknownErrors(t *testing.T) {
	original := repoOpen
	t.Cleanup(func() { repoOpen = original })
	wantErr := errors.New("boom")
	repoOpen = func(string, repo.OpenOptions) (*repo.Repository, error) {
		return nil, wantErr
	}
	_, err := openRepository("irrelevant")
	if !errors.Is(err, transport.ErrRepositoryNotFound) || !errors.Is(err, wantErr) {
		t.Fatalf("openRepository returned %v, want wrapping ErrRepositoryNotFound and the underlying error", err)
	}
}

func TestSessionCloseIsIdempotent(t *testing.T) {
	src := newTestRepo(t, true)
	sess, err := Dial(t.Context(), src.dir, transport.UploadPack, transport.Options{})
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	if err := sess.Close(); err != nil {
		t.Fatalf("Close returned error %v", err)
	}
	if err := sess.Close(); err != nil {
		t.Fatalf("second Close returned error %v", err)
	}
}

func TestClosedSessionRejectsOperations(t *testing.T) {
	src := newTestRepo(t, true)
	sess, err := Dial(t.Context(), src.dir, transport.UploadPack, transport.Options{})
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	if err := sess.Close(); err != nil {
		t.Fatalf("Close returned error %v", err)
	}
	if _, err := sess.Advertise(t.Context()); !errors.Is(err, transport.ErrProtocol) {
		t.Fatalf("Advertise on closed session returned %v, want ErrProtocol", err)
	}
	if _, err := sess.Fetch(t.Context(), transport.FetchRequest{}, nil); !errors.Is(err, transport.ErrProtocol) {
		t.Fatalf("Fetch on closed session returned %v, want ErrProtocol", err)
	}
	if _, err := sess.Push(t.Context(), transport.PushRequest{}); !errors.Is(err, transport.ErrProtocol) {
		t.Fatalf("Push on closed session returned %v, want ErrProtocol", err)
	}
}

func TestCommitterFuncFallsBackToDefaults(t *testing.T) {
	src := newTestRepo(t, true)
	sig := committerFunc(src.repo)()
	if sig.Name != defaultCommitterName || sig.Email != defaultCommitterEmail {
		t.Fatalf("committerFunc gave %+v, want defaults", sig)
	}
}

func TestCommitterFuncUsesConfiguredIdentity(t *testing.T) {
	dir := t.TempDir()
	global := isolatedGlobalFile(t)
	r, err := repo.Init(dir, repo.InitOptions{InitialBranch: "main", NoSystem: true, GlobalFile: global})
	if err != nil {
		t.Fatalf("repo.Init returned error %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })
	data, err := r.CommonRoot().ReadFile("config")
	if err != nil {
		t.Fatalf("ReadFile returned error %v", err)
	}
	data = append(data, []byte("[user]\n\tname = ann\n\temail = ann@example.com\n")...)
	if err := r.CommonRoot().WriteFile("config", data, 0o666); err != nil {
		t.Fatalf("WriteFile returned error %v", err)
	}
	reopened, err := repo.Open(dir, repo.OpenOptions{NoSystem: true, GlobalFile: global})
	if err != nil {
		t.Fatalf("repo.Open returned error %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	sig := committerFunc(reopened)()
	if sig.Name != "ann" || sig.Email != "ann@example.com" {
		t.Fatalf("committerFunc gave %+v, want configured identity", sig)
	}
}
