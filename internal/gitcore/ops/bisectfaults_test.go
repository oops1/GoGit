//go:build !race

package ops

import (
	"errors"
	"io/fs"
	"os"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/refs"
)

func failLstatOf(t *testing.T, target string) {
	t.Helper()
	swapSeam(t, &fsRootLstat, func(original func(*os.Root, string) (fs.FileInfo, error)) func(*os.Root, string) (fs.FileInfo, error) {
		return func(root *os.Root, name string) (fs.FileInfo, error) {
			if name == target {
				return nil, errInjected
			}
			return original(root, name)
		}
	})
}

func failReadOf(t *testing.T, target string) {
	t.Helper()
	swapSeam(t, &fsRootReadFile, func(original func(*os.Root, string) ([]byte, error)) func(*os.Root, string) ([]byte, error) {
		return func(root *os.Root, name string) ([]byte, error) {
			if name == target {
				return nil, errInjected
			}
			return original(root, name)
		}
	})
}

func failRemoveOf(t *testing.T, target string) {
	t.Helper()
	swapSeam(t, &fsRootRemove, func(original func(*os.Root, string) error) func(*os.Root, string) error {
		return func(root *os.Root, name string) error {
			if name == target {
				return errInjected
			}
			return original(root, name)
		}
	})
}

func TestReadMergeStateFailsWhenTheBisectCannotBeRead(t *testing.T) {
	for name, fail := range map[string]func(*testing.T){
		"log":   func(t *testing.T) { failLstatOf(t, "BISECT_LOG") },
		"start": func(t *testing.T) { failReadOf(t, "BISECT_START") },
	} {
		t.Run(name, func(t *testing.T) {
			r, _, _ := bisectingRepo(t)
			fail(t)

			if _, err := ReadMergeState(r.repo); !errors.Is(err, errInjected) {
				t.Fatalf("ReadMergeState = %v, want errInjected", err)
			}
		})
	}
}

func TestResetBisectReportsEveryFailure(t *testing.T) {
	tests := map[string]func(t *testing.T, r *testRepo){
		"read start":   func(t *testing.T, _ *testRepo) { failReadOf(t, "BISECT_START") },
		"stat head":    func(t *testing.T, _ *testRepo) { failLstatOf(t, "BISECT_HEAD") },
		"remove log":   func(t *testing.T, _ *testRepo) { failRemoveOf(t, "BISECT_LOG") },
		"remove start": func(t *testing.T, _ *testRepo) { failRemoveOf(t, "BISECT_START") },
		"open objects": func(t *testing.T, r *testRepo) {
			r.writeFile(".git/BISECT_HEAD", "x\n")
			swapSeam(t, &odbOpen, func(func(string, odb.Options) (*odb.DB, error)) func(string, odb.Options) (*odb.DB, error) {
				return func(string, odb.Options) (*odb.DB, error) { return nil, errInjected }
			})
		},
		"delete ref": func(t *testing.T, _ *testRepo) {
			swapSeam(t, &txDelete, func(func(*refs.Transaction, refs.Name, hash.ObjectID) error) func(*refs.Transaction, refs.Name, hash.ObjectID) error {
				return func(*refs.Transaction, refs.Name, hash.ObjectID) error { return errInjected }
			})
		},
		"commit refs": func(t *testing.T, _ *testRepo) {
			swapSeam(t, &txCommit, func(original func(*refs.Transaction) error) func(*refs.Transaction) error {
				return func(tx *refs.Transaction) error {
					if err := original(tx); err != nil {
						return err
					}
					return errInjected
				}
			})
		},
	}
	for name, fail := range tests {
		t.Run(name, func(t *testing.T) {
			r, _, _ := bisectingRepo(t)
			fail(t, r)

			if err := ResetBisect(t.Context(), r.repo); !errors.Is(err, errInjected) {
				t.Fatalf("ResetBisect = %v, want errInjected", err)
			}
		})
	}
}

func TestBranchRefusalsFailWhenTheWorktreesCannotBeListed(t *testing.T) {
	r, _, _ := bisectingRepo(t)
	swapSeam(t, &worktreeReadDir, func(func(string) ([]os.DirEntry, error)) func(string) ([]os.DirEntry, error) {
		return func(string) ([]os.DirEntry, error) { return nil, errInjected }
	})

	if err := RenameBranch(t.Context(), r.repo, "topic", "renamed", false); !errors.Is(err, errInjected) {
		t.Fatalf("RenameBranch = %v, want errInjected", err)
	}
}
