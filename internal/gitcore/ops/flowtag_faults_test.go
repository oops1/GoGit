//go:build !race

package ops

import (
	"errors"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/refs"
)

func failTagLookups(t *testing.T) {
	t.Helper()
	swapSeam(t, &refsLookup, func(original func(*refs.Store, refs.Name) (refs.Ref, error)) func(*refs.Store, refs.Name) (refs.Ref, error) {
		return func(store *refs.Store, name refs.Name) (refs.Ref, error) {
			if name.IsTag() {
				return refs.Ref{}, errInjected
			}
			return original(store, name)
		}
	})
}

func TestFlowReportsVersionTagsItCannotRead(t *testing.T) {
	t.Run("start cannot look the tag up", func(t *testing.T) {
		r, _ := newFlowRepo(t)
		failTagLookups(t)
		if _, err := StartFlow(t.Context(), r.repo, FlowKindRelease, "1.0", StartFlowOptions{}); !errors.Is(err, errInjected) {
			t.Fatalf("StartFlow returned %v", err)
		}
	})
	t.Run("finish cannot look the tag up", func(t *testing.T) {
		r := newReleaseRepo(t)
		failTagLookups(t)
		if _, err := FinishFlow(t.Context(), r.repo, FlowKindRelease, "1.0", FinishFlowOptions{}); !errors.Is(err, errInjected) {
			t.Fatalf("FinishFlow returned %v", err)
		}
	})
	t.Run("the references cannot be opened", func(t *testing.T) {
		r := newReleaseRepo(t)
		failRefsOpen(t)
		if _, _, err := flowTagPlace(r.repo, "1.0", "main"); !errors.Is(err, errInjected) {
			t.Fatalf("flowTagPlace returned %v", err)
		}
	})
}
