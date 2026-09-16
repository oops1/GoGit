//go:build !race

package ops

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hooks"
	"github.com/oops1/gogit/internal/gitcore/repo"
)

func TestCommitMessageFileFailuresStopTheCommit(t *testing.T) {
	boom := errors.New("message file failure")
	cases := map[string]func(t *testing.T){
		"write": func(t *testing.T) {
			failStateFile(t, commitEditMsgFile, boom)
		},
		"read": func(t *testing.T) {
			swapSeam(t, &fsRootReadFile, func(original func(*os.Root, string) ([]byte, error)) func(*os.Root, string) ([]byte, error) {
				return func(root *os.Root, name string) ([]byte, error) {
					if name == commitEditMsgFile {
						return nil, boom
					}
					return original(root, name)
				}
			})
		},
	}
	for label, inject := range cases {
		t.Run(label, func(t *testing.T) {
			tr := stagedRepo(t)
			installHooks(t, tr, testHook{}, hookCommitMsg)
			inject(t)

			if _, err := Commit(t.Context(), tr.repo, CommitOptions{Message: "subject"}); !errors.Is(err, boom) {
				t.Fatalf("Commit returned %v, want %v", err, boom)
			}
		})
	}
}

func failStateFile(t *testing.T, target string, boom error) {
	t.Helper()
	swapSeam(t, &fsRootWriteFile, func(original func(*os.Root, string, []byte, os.FileMode) error) func(*os.Root, string, []byte, os.FileMode) error {
		return func(root *os.Root, name string, data []byte, mode os.FileMode) error {
			if name == target {
				return boom
			}
			return original(root, name, data, mode)
		}
	})
}

func TestMergeStateFailuresAroundTheMergeHooksAreReported(t *testing.T) {
	boom := errors.New("state failure")
	cases := map[string]string{
		"rejection": hookPreMergeCommit,
		"editing":   hookPrepareCommitMsg,
	}
	for label, name := range cases {
		t.Run(label, func(t *testing.T) {
			tr := divergedTopic(t)
			installHooks(t, tr, testHook{exit: 1}, name)
			failStateFile(t, mergeHeadFile, boom)

			if _, err := Merge(t.Context(), tr.repo, "topic", MergeOptions{}); !errors.Is(err, boom) {
				t.Fatalf("Merge returned %v, want %v", err, boom)
			}
		})
	}
}

func TestClearingTheMergeStateAfterEditedMergeMessageIsReported(t *testing.T) {
	tr := divergedTopic(t)
	installHooks(t, tr, testHook{}, hookPrepareCommitMsg)
	boom := errors.New("remove failure")
	swapSeam(t, &fsRootRemove, func(original func(*os.Root, string) error) func(*os.Root, string) error {
		return func(root *os.Root, name string) error {
			if name == mergeHeadFile {
				return boom
			}
			return original(root, name)
		}
	})

	result, err := Merge(t.Context(), tr.repo, "topic", MergeOptions{})

	if !errors.Is(err, boom) || !result.Committed {
		t.Fatalf("Merge = %+v, %v, want the committed merge and %v", result, err, boom)
	}
}

func cloneWithPostCheckout(t *testing.T, hook testHook) (*testRepo, string, *repo.Repository, error) {
	t.Helper()
	src := newFetchServer(t)
	swapSeam(t, &cloneRepoOpen, func(original func(string, repo.OpenOptions) (*repo.Repository, error)) func(string, repo.OpenOptions) (*repo.Repository, error) {
		return func(dir string, opts repo.OpenOptions) (*repo.Repository, error) {
			r, err := original(dir, opts)
			if err == nil {
				installTestHook(t, r.HooksDir(), hookPostCheckout, hook)
			}
			return r, err
		}
	})
	dest := filepath.Join(t.TempDir(), "clone")
	r, err := Clone(t.Context(), src.dir, dest, CloneOptions{})
	if r != nil {
		t.Cleanup(func() { _ = r.Close() })
	}
	return src, dest, r, err
}

func TestCloneRunsPostCheckoutFromTheNullCommit(t *testing.T) {
	log := hookLogPath(t)
	src, _, _, err := cloneWithPostCheckout(t, testHook{log: log})
	if err != nil {
		t.Fatalf("Clone returned error %v", err)
	}

	want := []hookRecord{{name: hookPostCheckout, args: []string{strings.Repeat("0", 40), src.branchTarget("main").String(), "1"}}}
	if got := readHookLog(t, log); !recordsEqual(got, want) {
		t.Fatalf("hooks = %+v, want %+v", got, want)
	}
}

func TestAFailingPostCheckoutHookKeepsTheClone(t *testing.T) {
	_, dest, r, err := cloneWithPostCheckout(t, testHook{exit: 1})

	if !errors.Is(err, hooks.ErrRejected) || r == nil {
		t.Fatalf("Clone = %v, %v, want the repository and the hook failure", r, err)
	}
	if _, statErr := os.Stat(filepath.Join(dest, "a.txt")); statErr != nil {
		t.Fatalf("the checked out file is gone: %v", statErr)
	}
}
