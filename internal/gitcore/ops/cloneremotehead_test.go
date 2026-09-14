package ops

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/refs"
)

func TestCloneRecordsTheRemoteHeadOfTheCheckedOutBranch(t *testing.T) {
	src := newFetchServer(t)
	client := cloneForFetch(t, src)

	head, err := client.refs().Lookup(refs.Name("refs/remotes/origin/HEAD"))
	if err != nil {
		t.Fatalf("Lookup(origin/HEAD) returned error %v", err)
	}
	if head.SymbolicTarget != refs.RemoteBranchName("origin", "main") {
		t.Fatalf("origin/HEAD points at %q, want origin/main", head.SymbolicTarget)
	}
}

func TestCloneReportsARemoteHeadItCannotRecord(t *testing.T) {
	src := newFetchServer(t)
	failure := errors.New("remote head refused")
	original := txSetSymbolic
	txSetSymbolic = func(tx *refs.Transaction, name, target refs.Name) error {
		if name == refs.Name("refs/remotes/origin/HEAD") {
			return failure
		}
		return original(tx, name, target)
	}
	t.Cleanup(func() { txSetSymbolic = original })

	if _, err := Clone(t.Context(), src.dir, filepath.Join(t.TempDir(), "dest"), CloneOptions{}); !errors.Is(err, failure) {
		t.Fatalf("Clone returned %v, want %v", err, failure)
	}
}
