package ops

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/repo"
)

func blockPath(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o777); err != nil {
		t.Fatalf("MkdirAll returned error %v", err)
	}
}

func TestSparseCheckoutReportsAnUnreadableSetting(t *testing.T) {
	tests := []struct {
		name  string
		entry string
		run   func(*testRepo) error
	}{
		{"enabled", "[core]\n\tsparseCheckout = perhaps\n", func(r *testRepo) error {
			_, err := SparseCheckoutList(r.repo)
			return err
		}},
		{"cone", "[core]\n\tsparseCheckout = true\n\tsparseCheckoutCone = perhaps\n", func(r *testRepo) error {
			_, err := SparseCheckoutList(r.repo)
			return err
		}},
		{"coneOnReapply", "[core]\n\tsparseCheckout = true\n\tsparseCheckoutCone = perhaps\n", func(r *testRepo) error {
			return SparseCheckoutReapply(r.t.Context(), r.repo)
		}},
		{"coneOnAdd", "[core]\n\tsparseCheckout = true\n\tsparseCheckoutCone = perhaps\n", func(r *testRepo) error {
			return SparseCheckoutAdd(r.t.Context(), r.repo, []string{"a"}, SparseCheckoutOptions{})
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := sparseWorkRepo(t)
			r.appendConfig(tc.entry)
			r.repo = r.reopen()
			if err := tc.run(r); err == nil {
				t.Fatal("the invalid boolean was not reported")
			}
		})
	}
}

func TestSparseCheckoutReportsAnUnreadablePatternFile(t *testing.T) {
	r := sparseWorkRepo(t)
	failure := errors.New("cannot read the pattern file")
	original := sparseReadFile
	sparseReadFile = func(string) ([]byte, error) { return nil, failure }
	t.Cleanup(func() { sparseReadFile = original })

	if err := SparseCheckoutInit(t.Context(), r.repo, SparseCheckoutOptions{Cone: true}); !errors.Is(err, failure) {
		t.Fatalf("SparseCheckoutInit returned %v, want %v", err, failure)
	}
}

func TestSparseCheckoutReportsAnUnreadablePatternFileOfAnEnabledCheckout(t *testing.T) {
	r := sparseWorkRepo(t)
	r.enableSparse(true, sparseConeOfA)
	failure := errors.New("cannot read the pattern file")
	original := sparseReadFile
	sparseReadFile = func(string) ([]byte, error) { return nil, failure }
	t.Cleanup(func() { sparseReadFile = original })

	if _, err := SparseCheckoutList(r.repo); !errors.Is(err, failure) {
		t.Fatalf("SparseCheckoutList returned %v, want %v", err, failure)
	}
	if err := SparseCheckoutReapply(t.Context(), r.repo); !errors.Is(err, failure) {
		t.Fatalf("SparseCheckoutReapply returned %v, want %v", err, failure)
	}
	if err := SparseCheckoutAdd(t.Context(), r.repo, []string{"a"}, SparseCheckoutOptions{}); !errors.Is(err, failure) {
		t.Fatalf("SparseCheckoutAdd returned %v, want %v", err, failure)
	}
}

func TestSparseCheckoutReportsADirectoryItCannotCreate(t *testing.T) {
	r := sparseWorkRepo(t)
	failure := errors.New("cannot create the info directory")
	original := sparseMkdirAll
	sparseMkdirAll = func(string, os.FileMode) error { return failure }
	t.Cleanup(func() { sparseMkdirAll = original })

	if err := SparseCheckoutSet(t.Context(), r.repo, []string{"a"}, SparseCheckoutOptions{Cone: true}); !errors.Is(err, failure) {
		t.Fatalf("SparseCheckoutSet returned %v, want %v", err, failure)
	}
}

func TestSparseCheckoutReportsAConfigItCannotSave(t *testing.T) {
	tests := []struct {
		name string
		lock string
	}{
		{"local", "config.lock"},
		{"worktree", "config.worktree.lock"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := sparseWorkRepo(t)
			blockPath(t, filepath.Join(r.repo.GitDir(), tc.lock))
			if err := SparseCheckoutSet(t.Context(), r.repo, []string{"a"}, SparseCheckoutOptions{Cone: true}); err == nil {
				t.Fatal("the blocked config write was not reported")
			}
		})
	}
}

func TestSparseCheckoutReportsARepositoryItCannotReopen(t *testing.T) {
	failure := errors.New("cannot reopen the repository")
	for _, name := range []string{"set", "disable"} {
		t.Run(name, func(t *testing.T) {
			r := sparseWorkRepo(t)
			original := repoOpenLayout
			repoOpenLayout = func(repo.Layout, repo.OpenOptions) (*repo.Repository, error) { return nil, failure }
			t.Cleanup(func() { repoOpenLayout = original })
			var err error
			if name == "set" {
				err = SparseCheckoutSet(t.Context(), r.repo, []string{"a"}, SparseCheckoutOptions{Cone: true})
			} else {
				err = SparseCheckoutDisable(t.Context(), r.repo)
			}
			if !errors.Is(err, failure) {
				t.Fatalf("%s returned %v, want %v", name, err, failure)
			}
		})
	}
}

func TestSparseCheckoutNeedsAWorkingTree(t *testing.T) {
	r := newBareTestRepo(t)
	if err := SparseCheckoutSet(t.Context(), r.repo, []string{"a"}, SparseCheckoutOptions{Cone: true}); !errors.Is(err, ErrBareRepository) {
		t.Fatalf("SparseCheckoutSet returned %v, want ErrBareRepository", err)
	}
}

func TestSparseCheckoutReportsTheSeamsItUpdatesTheWorkingTreeThrough(t *testing.T) {
	failure := errors.New("the working tree could not be laid out")
	tests := []struct {
		name string
		seam func(*testing.T)
	}{
		{"objects", func(t *testing.T) {
			swapSeam(t, &odbOpen, func(func(string, odb.Options) (*odb.DB, error)) func(string, odb.Options) (*odb.DB, error) {
				return func(string, odb.Options) (*odb.DB, error) { return nil, failure }
			})
		}},
		{"refs", func(t *testing.T) {
			swapSeam(t, &refsOpen, func(func(refs.Options) (*refs.Store, error)) func(refs.Options) (*refs.Store, error) {
				return func(refs.Options) (*refs.Store, error) { return nil, failure }
			})
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := sparseWorkRepo(t)
			tc.seam(t)
			if err := SparseCheckoutSet(t.Context(), r.repo, []string{"a"}, SparseCheckoutOptions{Cone: true}); !errors.Is(err, failure) {
				t.Fatalf("SparseCheckoutSet returned %v, want %v", err, failure)
			}
		})
	}
}

func TestSparseCheckoutReportsInvalidPathRules(t *testing.T) {
	r := sparseWorkRepo(t)
	r.appendConfig("[core]\n\tprotectNTFS = perhaps\n")
	r.repo = r.reopen()
	if err := SparseCheckoutSet(t.Context(), r.repo, []string{"a"}, SparseCheckoutOptions{Cone: true}); err == nil {
		t.Fatal("the invalid path rule setting was not reported")
	}
}
