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

	if _, err := FinishFlow(t.Context(), r.repo, FlowKindRelease, "1.0", FinishFlowOptions{Push: true, Network: originFlow}); !errors.Is(err, errInjected) {
		t.Fatalf("FinishFlow returned %v, want the push failure", err)
	}
}

func TestFinishReleaseReportsAFailedBranchDeletion(t *testing.T) {
	r := newReleaseRepo(t)
	swapSeam(t, &flowDeleteBranch, func(func(context.Context, *repo.Repository, string, bool) error) func(context.Context, *repo.Repository, string, bool) error {
		return func(context.Context, *repo.Repository, string, bool) error { return errInjected }
	})

	if _, err := FinishFlow(t.Context(), r.repo, FlowKindRelease, "1.0", FinishFlowOptions{DeleteBranch: true}); !errors.Is(err, errInjected) {
		t.Fatalf("FinishFlow returned %v, want the deletion failure", err)
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

	if _, err := FinishFlow(t.Context(), r.repo, FlowKindRelease, "1.0", FinishFlowOptions{}); !errors.Is(err, errInjected) {
		t.Fatalf("FinishFlow returned %v, want the state write failure", err)
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

	if _, err := FinishFlow(t.Context(), r.repo, FlowKindRelease, "1.0", FinishFlowOptions{}); !errors.Is(err, errInjected) {
		t.Fatalf("FinishFlow returned %v, want the state removal failure", err)
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
		failRefsOpen(t)
		if err := flowNotBehind(r.repo, originFlow, "develop"); !errors.Is(err, errInjected) {
			t.Fatalf("flowNotBehind returned %v", err)
		}
	})
	t.Run("the remote branch cannot be read", func(t *testing.T) {
		failRemoteLookups(t)
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

func failRefsOpen(t *testing.T) {
	t.Helper()
	swapSeam(t, &refsOpen, func(func(refs.Options) (*refs.Store, error)) func(refs.Options) (*refs.Store, error) {
		return func(refs.Options) (*refs.Store, error) { return nil, errInjected }
	})
}

func failRemoteLookups(t *testing.T) {
	t.Helper()
	swapSeam(t, &refsLookup, func(original func(*refs.Store, refs.Name) (refs.Ref, error)) func(*refs.Store, refs.Name) (refs.Ref, error) {
		return func(store *refs.Store, name refs.Name) (refs.Ref, error) {
			if name.IsRemote() {
				return refs.Ref{}, errInjected
			}
			return original(store, name)
		}
	})
}

func TestFlowReportsReferencesItCannotRead(t *testing.T) {
	r, _ := newFlowRepo(t)
	if err := AddRemote(r.repo, "origin", newBareTestRepo(t).dir); err != nil {
		t.Fatalf("AddRemote returned error %v", err)
	}
	r.repo = r.reopen()
	startFlowRelease(t, r, "1.0", FlowNetwork{})

	t.Run("refs cannot be opened", func(t *testing.T) {
		failRefsOpen(t)
		if _, err := ConfigureFlow(t.Context(), r.repo, mainFlowConfig()); !errors.Is(err, errInjected) {
			t.Fatalf("ConfigureFlow returned %v", err)
		}
		if _, err := StartFlow(t.Context(), r.repo, FlowKindFeature, "login", StartFlowOptions{}); !errors.Is(err, errInjected) {
			t.Fatalf("StartFlow returned %v", err)
		}
	})
	t.Run("remote branches cannot be read", func(t *testing.T) {
		failRemoteLookups(t)
		if _, err := StartFlow(t.Context(), r.repo, FlowKindFeature, "login", StartFlowOptions{Network: originFlow}); !errors.Is(err, errInjected) {
			t.Fatalf("StartFlow returned %v", err)
		}
		if _, err := FinishFlow(t.Context(), r.repo, FlowKindRelease, "1.0", FinishFlowOptions{Fetch: true, Network: originFlow}); !errors.Is(err, errInjected) {
			t.Fatalf("FinishFlow with a fetch returned %v", err)
		}
		if _, err := FinishFlow(t.Context(), r.repo, FlowKindRelease, "1.0", FinishFlowOptions{Push: true, Network: originFlow}); !errors.Is(err, errInjected) {
			t.Fatalf("FinishFlow with a push returned %v", err)
		}
	})
}
