//go:build !race

package ops

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/remote"
	"github.com/oops1/gogit/internal/gitcore/repo"
)

func TestFinishReleaseReportsAFailedPush(t *testing.T) {
	r, _ := newFlowRepo(t)
	flowServer(t, r)
	startFlowRelease(t, r, "1.0", originFlow)
	swapSeam(t, &flowPush, func(func(context.Context, *repo.Repository, string, remote.PushOptions) (remote.PushResult, error)) func(context.Context, *repo.Repository, string, remote.PushOptions) (remote.PushResult, error) {
		return func(context.Context, *repo.Repository, string, remote.PushOptions) (remote.PushResult, error) {
			return remote.PushResult{}, errInjected
		}
	})

	if _, err := FinishRelease(t.Context(), r.repo, "1.0", FinishReleaseOptions{Push: true, Network: originFlow}); !errors.Is(err, errInjected) {
		t.Fatalf("FinishRelease returned %v, want the push failure", err)
	}
}

func TestFinishReleaseReportsAFailedBranchDeletion(t *testing.T) {
	r := newReleaseRepo(t)
	swapSeam(t, &flowDeleteBranch, func(func(context.Context, *repo.Repository, string, bool) error) func(context.Context, *repo.Repository, string, bool) error {
		return func(context.Context, *repo.Repository, string, bool) error { return errInjected }
	})

	if _, err := FinishRelease(t.Context(), r.repo, "1.0", FinishReleaseOptions{DeleteBranch: true}); !errors.Is(err, errInjected) {
		t.Fatalf("FinishRelease returned %v, want the deletion failure", err)
	}
}

func failFlowStateFile[F any](t *testing.T, seam *F, wrap func(original F) F) {
	t.Helper()
	swapSeam(t, seam, wrap)
}

func TestFinishReleaseReportsAStateThatCannotBeSaved(t *testing.T) {
	r, _ := newFlowRepo(t)
	startFlowRelease(t, r, "1.0", FlowNetwork{})
	commitFlowFile(t, r, "a.txt", "release\n", "release change")
	switchFlowBranch(t, r, "main")
	commitFlowFile(t, r, "a.txt", "main\n", "main change")
	failFlowStateFile(t, &fsRootWriteFile, func(original func(*os.Root, string, []byte, fs.FileMode) error) func(*os.Root, string, []byte, fs.FileMode) error {
		return func(root *os.Root, name string, data []byte, mode fs.FileMode) error {
			if name == flowStateFile {
				return errInjected
			}
			return original(root, name, data, mode)
		}
	})

	if _, err := FinishRelease(t.Context(), r.repo, "1.0", FinishReleaseOptions{}); !errors.Is(err, errInjected) {
		t.Fatalf("FinishRelease returned %v, want the state write failure", err)
	}
}

func TestFinishReleaseReportsAStateThatCannotBeCleared(t *testing.T) {
	r := newReleaseRepo(t)
	failFlowStateFile(t, &fsRootRemove, func(original func(*os.Root, string) error) func(*os.Root, string) error {
		return func(root *os.Root, name string) error {
			if name == flowStateFile {
				return errInjected
			}
			return original(root, name)
		}
	})

	if _, err := FinishRelease(t.Context(), r.repo, "1.0", FinishReleaseOptions{}); !errors.Is(err, errInjected) {
		t.Fatalf("FinishRelease returned %v, want the state removal failure", err)
	}
}

func TestFlowNotBehindReportsWhatItCannotRead(t *testing.T) {
	r, _ := newFlowRepo(t)
	flowServer(t, r)
	if err := flowNotBehind(r.repo, originFlow, "develop"); err != nil {
		t.Fatalf("flowNotBehind before any fetch returned %v", err)
	}
	startFlowRelease(t, r, "1.0", originFlow)

	t.Run("refs cannot be opened", func(t *testing.T) {
		swapSeam(t, &refsOpen, func(func(refs.Options) (*refs.Store, error)) func(refs.Options) (*refs.Store, error) {
			return func(refs.Options) (*refs.Store, error) { return nil, errInjected }
		})
		if err := flowNotBehind(r.repo, originFlow, "develop"); !errors.Is(err, errInjected) {
			t.Fatalf("flowNotBehind returned %v", err)
		}
	})
	t.Run("the remote branch cannot be read", func(t *testing.T) {
		swapSeam(t, &refsLookup, func(original func(*refs.Store, refs.Name) (refs.Ref, error)) func(*refs.Store, refs.Name) (refs.Ref, error) {
			return func(store *refs.Store, name refs.Name) (refs.Ref, error) {
				if name.IsRemote() {
					return refs.Ref{}, errInjected
				}
				return original(store, name)
			}
		})
		if err := flowNotBehind(r.repo, originFlow, "develop"); !errors.Is(err, errInjected) {
			t.Fatalf("flowNotBehind returned %v", err)
		}
	})
	t.Run("the remote commit is missing", func(t *testing.T) {
		swapSeam(t, &refsLookup, func(original func(*refs.Store, refs.Name) (refs.Ref, error)) func(*refs.Store, refs.Name) (refs.Ref, error) {
			return func(store *refs.Store, name refs.Name) (refs.Ref, error) {
				if name.IsRemote() {
					return refs.Ref{Name: name, Target: bogusObjectID(t, r.repo.ObjectFormat)}, nil
				}
				return original(store, name)
			}
		})
		if err := flowNotBehind(r.repo, originFlow, "develop"); err == nil {
			t.Fatal("flowNotBehind walked a commit that does not exist")
		}
	})
}
