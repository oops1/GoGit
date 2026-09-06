package local

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/pack"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/transport"
)

func makeDirReadOnly(t testing.TB, dir string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not enforce POSIX write permissions on directories")
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatalf("Chmod returned error %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o777) })
	probe := filepath.Join(dir, "permission-probe")
	file, err := os.OpenFile(probe, os.O_CREATE|os.O_WRONLY, 0o600)
	if err == nil {
		_ = file.Close()
		_ = os.Remove(probe)
		t.Skip("the current user bypasses POSIX write permissions")
	}
}

func makeDirUnreadable(t testing.TB, dir string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not enforce POSIX read permissions on directories")
	}
	if err := os.Chmod(dir, 0o000); err != nil {
		t.Fatalf("Chmod returned error %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	if _, err := os.ReadDir(dir); err == nil {
		t.Skip("the current user bypasses POSIX read permissions")
	}
}

func writePackFor(t testing.TB, src *testRepo, ids []hash.ObjectID) []byte {
	t.Helper()
	var buf bytes.Buffer
	if _, err := pack.WritePack(t.Context(), &buf, src.db, ids, pack.WriteOptions{}); err != nil {
		t.Fatalf("WritePack returned error %v", err)
	}
	return buf.Bytes()
}

func TestPushCreatesNewBranch(t *testing.T) {
	src := newTestRepo(t, true)
	commit := src.commit("main", map[string]string{"a.txt": "hello"})
	dst := newTestRepo(t, true)

	sess := dialSession(t, dst.dir)
	packBytes := writePackFor(t, src, []hash.ObjectID{commit})
	result, err := sess.Push(t.Context(), transport.PushRequest{
		Updates: []transport.Update{{Name: "refs/heads/feature", Old: hash.Zero, New: commit}},
		Pack:    bytes.NewReader(packBytes),
	})
	if err != nil {
		t.Fatalf("Push returned error %v", err)
	}
	if !result.UnpackOK {
		t.Fatalf("result.UnpackOK = false, UnpackError = %q", result.UnpackError)
	}
	if len(result.Refs) != 1 || !result.Refs[0].OK {
		t.Fatalf("result.Refs = %+v, want a single OK status", result.Refs)
	}
	if got := dst.branchTarget("feature"); got != commit {
		t.Fatalf("refs/heads/feature = %s, want %s", got, commit)
	}
	if !dst.hasObject(commit) {
		t.Fatal("pushed commit is missing from the destination")
	}
}

func TestPushFastForwardUpdatesBranch(t *testing.T) {
	src := newTestRepo(t, true)
	first := src.commit("main", map[string]string{"a.txt": "hello"})
	second := src.commit("main", map[string]string{"a.txt": "hello", "b.txt": "world"}, first)

	dst := newTestRepo(t, true)
	dst.setBranch("main", first)

	sess := dialSession(t, dst.dir)
	packBytes := writePackFor(t, src, []hash.ObjectID{first, second})
	result, err := sess.Push(t.Context(), transport.PushRequest{
		Updates: []transport.Update{{Name: "refs/heads/main", Old: first, New: second}},
		Pack:    bytes.NewReader(packBytes),
	})
	if err != nil {
		t.Fatalf("Push returned error %v", err)
	}
	if len(result.Refs) != 1 || !result.Refs[0].OK {
		t.Fatalf("result.Refs = %+v, want a single OK status", result.Refs)
	}
	if got := dst.branchTarget("main"); got != second {
		t.Fatalf("refs/heads/main = %s, want %s", got, second)
	}
}

func TestPushRejectsNonFastForwardWithoutForce(t *testing.T) {
	src := newTestRepo(t, true)
	base := src.commit("main", map[string]string{"a.txt": "hello"})
	diverged := src.commit("main", map[string]string{"a.txt": "hello", "c.txt": "diverged"}, base)

	dst := newTestRepo(t, true)
	other := dst.commit("main", map[string]string{"a.txt": "hello", "d.txt": "dst-only"}, base)

	sess := dialSession(t, dst.dir)
	packBytes := writePackFor(t, src, []hash.ObjectID{base, diverged})
	result, err := sess.Push(t.Context(), transport.PushRequest{
		Updates: []transport.Update{{Name: "refs/heads/main", Old: other, New: diverged}},
		Pack:    bytes.NewReader(packBytes),
	})
	if err != nil {
		t.Fatalf("Push returned error %v", err)
	}
	if len(result.Refs) != 1 || result.Refs[0].OK {
		t.Fatalf("result.Refs = %+v, want a single rejected status", result.Refs)
	}
	if got := dst.branchTarget("main"); got != other {
		t.Fatalf("refs/heads/main = %s, want unchanged %s", got, other)
	}
}

func TestPushForcedAcceptsNonFastForward(t *testing.T) {
	src := newTestRepo(t, true)
	base := src.commit("main", map[string]string{"a.txt": "hello"})
	diverged := src.commit("main", map[string]string{"a.txt": "hello", "c.txt": "diverged"}, base)

	dst := newTestRepo(t, true)
	other := dst.commit("main", map[string]string{"a.txt": "hello", "d.txt": "dst-only"}, base)

	sess := dialSession(t, dst.dir)
	packBytes := writePackFor(t, src, []hash.ObjectID{base, diverged})
	result, err := sess.Push(t.Context(), transport.PushRequest{
		Updates: []transport.Update{{Name: "refs/heads/main", Old: other, New: diverged}},
		Pack:    bytes.NewReader(packBytes),
		Options: []string{"force:refs/heads/main"},
	})
	if err != nil {
		t.Fatalf("Push returned error %v", err)
	}
	if len(result.Refs) != 1 || !result.Refs[0].OK {
		t.Fatalf("result.Refs = %+v, want a single OK status", result.Refs)
	}
	if got := dst.branchTarget("main"); got != diverged {
		t.Fatalf("refs/heads/main = %s, want %s", got, diverged)
	}
}

func TestPushDeletesRef(t *testing.T) {
	dst := newTestRepo(t, true)
	commit := dst.commit("main", map[string]string{"a.txt": "hello"})
	dst.setBranch("doomed", commit)

	sess := dialSession(t, dst.dir)
	result, err := sess.Push(t.Context(), transport.PushRequest{
		Updates: []transport.Update{{Name: "refs/heads/doomed", Old: commit, New: hash.Zero}},
	})
	if err != nil {
		t.Fatalf("Push returned error %v", err)
	}
	if len(result.Refs) != 1 || !result.Refs[0].OK {
		t.Fatalf("result.Refs = %+v, want a single OK status", result.Refs)
	}
	if _, err := dst.refs.Lookup(refs.BranchName("doomed")); !errors.Is(err, refs.ErrNotFound) {
		t.Fatalf("refs/heads/doomed still exists after deletion: err=%v", err)
	}
}

func TestPushWithMalformedPackReportsUnpackFailure(t *testing.T) {
	dst := newTestRepo(t, true)
	sess := dialSession(t, dst.dir)
	result, err := sess.Push(t.Context(), transport.PushRequest{
		Updates: []transport.Update{{Name: "refs/heads/main", Old: hash.Zero, New: hash.Zero}},
		Pack:    bytes.NewReader([]byte("not a pack file")),
	})
	if err != nil {
		t.Fatalf("Push returned error %v", err)
	}
	if result.UnpackOK {
		t.Fatal("result.UnpackOK = true for a malformed pack")
	}
	if result.UnpackError == "" {
		t.Fatal("result.UnpackError is empty for a malformed pack")
	}
}

func TestPushAtomicAbortsAllOnOneFailure(t *testing.T) {
	src := newTestRepo(t, true)
	base := src.commit("main", map[string]string{"a.txt": "hello"})
	diverged := src.commit("main", map[string]string{"a.txt": "hello", "c.txt": "diverged"}, base)

	dst := newTestRepo(t, true)
	other := dst.commit("main", map[string]string{"a.txt": "hello", "d.txt": "dst-only"}, base)
	dst.setBranch("second", base)

	sess := dialSession(t, dst.dir)
	packBytes := writePackFor(t, src, []hash.ObjectID{base, diverged})
	result, err := sess.Push(t.Context(), transport.PushRequest{
		Updates: []transport.Update{
			{Name: "refs/heads/main", Old: other, New: diverged},
			{Name: "refs/heads/second", Old: base, New: diverged},
		},
		Pack:   bytes.NewReader(packBytes),
		Atomic: true,
	})
	if err != nil {
		t.Fatalf("Push returned error %v", err)
	}
	for _, status := range result.Refs {
		if status.OK {
			t.Fatalf("result.Refs = %+v, want every ref rejected by the atomic push", result.Refs)
		}
	}
	if got := dst.branchTarget("second"); got != base {
		t.Fatalf("refs/heads/second = %s, want unchanged %s", got, base)
	}
}

func TestPushFailsWhenObjectDatabaseCannotReload(t *testing.T) {
	src := newTestRepo(t, true)
	commit := src.commit("main", map[string]string{"a.txt": "hello"})
	dst := newTestRepo(t, true)
	sess := dialSession(t, dst.dir)
	if err := os.WriteFile(filepath.Join(dst.repo.PackDir(), "pack-broken.pack"), []byte("not a packfile"), 0o666); err != nil {
		t.Fatalf("WriteFile returned error %v", err)
	}
	if err := os.WriteFile(filepath.Join(dst.repo.PackDir(), "pack-broken.idx"), []byte("not an index"), 0o666); err != nil {
		t.Fatalf("WriteFile returned error %v", err)
	}

	packBytes := writePackFor(t, src, []hash.ObjectID{commit})
	_, err := sess.Push(t.Context(), transport.PushRequest{
		Updates: []transport.Update{{Name: "refs/heads/feature", Old: hash.Zero, New: commit}},
		Pack:    bytes.NewReader(packBytes),
	})
	if err == nil {
		t.Fatal("Push tolerated a broken pack file already present in the destination")
	}
}

func TestPushChecksContextAfterUnpacking(t *testing.T) {
	dst := newTestRepo(t, true)
	sess := dialSession(t, dst.dir)
	ctx := newCountingContext(t, 2)
	if _, err := sess.Push(ctx, transport.PushRequest{}); err == nil {
		t.Fatal("Push tolerated cancellation right after unpacking")
	}
}

func TestPushAtomicRollsBackWhenAnUpdateCannotBeStaged(t *testing.T) {
	dst := newTestRepo(t, true)
	commit := dst.commit("main", map[string]string{"a.txt": "hello"})

	sess := dialSession(t, dst.dir)
	result, err := sess.Push(t.Context(), transport.PushRequest{
		Updates: []transport.Update{
			{Name: "refs/heads/feature", Old: hash.Zero, New: commit},
			{Name: "refs/heads/feature", Old: hash.Zero, New: commit},
		},
		Atomic: true,
	})
	if err != nil {
		t.Fatalf("Push returned error %v", err)
	}
	for _, status := range result.Refs {
		if status.OK {
			t.Fatalf("result.Refs = %+v, want every ref rejected", result.Refs)
		}
	}
	if _, err := dst.refs.Lookup(refs.BranchName("feature")); !errors.Is(err, refs.ErrNotFound) {
		t.Fatalf("refs/heads/feature was created despite the aborted transaction: err=%v", err)
	}
}

func TestPushAtomicFailsAtCommitOnStaleOldValue(t *testing.T) {
	dst := newTestRepo(t, true)
	a := dst.commit("main", map[string]string{"a.txt": "1"})
	b := dst.commit("main", map[string]string{"a.txt": "2"}, a)
	c := dst.commitWithTree(dst.tree(map[string]string{"a.txt": "3"}), a)

	sess := dialSession(t, dst.dir)
	result, err := sess.Push(t.Context(), transport.PushRequest{
		Updates: []transport.Update{{Name: "refs/heads/main", Old: a, New: c}},
		Atomic:  true,
	})
	if err != nil {
		t.Fatalf("Push returned error %v", err)
	}
	if len(result.Refs) != 1 || result.Refs[0].OK {
		t.Fatalf("result.Refs = %+v, want a single rejected status", result.Refs)
	}
	if got := dst.branchTarget("main"); got != b {
		t.Fatalf("refs/heads/main = %s, want unchanged %s", got, b)
	}
}

func TestPushRejectsUpdateWhenOldValueDoesNotExist(t *testing.T) {
	dst := newTestRepo(t, true)
	commit := dst.commit("main", map[string]string{"a.txt": "hello"})
	sess := dialSession(t, dst.dir)
	result, err := sess.Push(t.Context(), transport.PushRequest{
		Updates: []transport.Update{{Name: "refs/heads/feature", Old: dst.missingObjectID(), New: commit}},
	})
	if err != nil {
		t.Fatalf("Push returned error %v", err)
	}
	if len(result.Refs) != 1 || result.Refs[0].OK {
		t.Fatalf("result.Refs = %+v, want a single rejected status", result.Refs)
	}
}

func TestPushIndividuallyRejectsAnInvalidRefName(t *testing.T) {
	dst := newTestRepo(t, true)
	commit := dst.commit("main", map[string]string{"a.txt": "hello"})

	sess := dialSession(t, dst.dir)
	result, err := sess.Push(t.Context(), transport.PushRequest{
		Updates: []transport.Update{{Name: "not a valid ref name", Old: hash.Zero, New: commit}},
	})
	if err != nil {
		t.Fatalf("Push returned error %v", err)
	}
	if len(result.Refs) != 1 || result.Refs[0].OK {
		t.Fatalf("result.Refs = %+v, want a single rejected status", result.Refs)
	}
}

func TestPushIndividuallyFailsAtCommitOnStaleOldValue(t *testing.T) {
	dst := newTestRepo(t, true)
	a := dst.commit("main", map[string]string{"a.txt": "1"})
	b := dst.commit("main", map[string]string{"a.txt": "2"}, a)
	c := dst.commitWithTree(dst.tree(map[string]string{"a.txt": "3"}), a)

	sess := dialSession(t, dst.dir)
	result, err := sess.Push(t.Context(), transport.PushRequest{
		Updates: []transport.Update{{Name: "refs/heads/main", Old: a, New: c}},
	})
	if err != nil {
		t.Fatalf("Push returned error %v", err)
	}
	if len(result.Refs) != 1 || result.Refs[0].OK {
		t.Fatalf("result.Refs = %+v, want a single rejected status", result.Refs)
	}
	if got := dst.branchTarget("main"); got != b {
		t.Fatalf("refs/heads/main = %s, want unchanged %s", got, b)
	}
}

func TestPushRespectsCanceledContext(t *testing.T) {
	dst := newTestRepo(t, true)
	sess := dialSession(t, dst.dir)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := sess.Push(ctx, transport.PushRequest{}); err == nil {
		t.Fatal("Push on a canceled context returned no error")
	}
}

func TestPushToReadOnlyRepositoryFails(t *testing.T) {
	src := newTestRepo(t, true)
	commit := src.commit("main", map[string]string{"a.txt": "hello"})
	dst := newTestRepo(t, true)
	sess := dialSession(t, dst.dir)
	s := sess.(*session)
	makeDirReadOnly(t, s.repo.PackDir())
	packBytes := writePackFor(t, src, []hash.ObjectID{commit})
	_, err := sess.Push(t.Context(), transport.PushRequest{
		Updates: []transport.Update{{Name: "refs/heads/main", Old: hash.Zero, New: commit}},
		Pack:    bytes.NewReader(packBytes),
	})
	if !errors.Is(err, ErrReadOnly) {
		t.Fatalf("Push into a read-only pack directory returned %v, want ErrReadOnly", err)
	}
}

func TestPlanUpdatesRespectsCanceledContextMidLoop(t *testing.T) {
	dst := newTestRepo(t, true)
	commit := dst.commit("main", map[string]string{"a.txt": "hello"})
	sess := dialSession(t, dst.dir).(*session)
	ctx := newCountingContext(t, 1)
	plan, statuses := sess.planUpdates(ctx, []transport.Update{
		{Name: "refs/heads/x", Old: hash.Zero, New: commit},
		{Name: "refs/heads/y", Old: hash.Zero, New: commit},
	}, nil)
	if len(plan) != 0 {
		t.Fatalf("plan = %v, want none staged after cancellation", plan)
	}
	if len(statuses) != 2 || statuses["refs/heads/x"].OK || statuses["refs/heads/y"].OK {
		t.Fatalf("statuses = %+v, want both updates rejected by cancellation", statuses)
	}
}

func TestApplyAtomicRespectsCanceledContextBeforeCommit(t *testing.T) {
	dst := newTestRepo(t, true)
	commit := dst.commit("main", map[string]string{"a.txt": "hello"})
	sess := dialSession(t, dst.dir).(*session)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	plan := []transport.Update{{Name: "refs/heads/feature", Old: hash.Zero, New: commit}}
	statuses := make(map[string]transport.RefStatus)
	sess.applyAtomic(ctx, plan, statuses)
	if statuses["refs/heads/feature"].OK {
		t.Fatalf("statuses = %+v, want the update rejected by cancellation", statuses)
	}
	if _, err := dst.refs.Lookup(refs.BranchName("feature")); !errors.Is(err, refs.ErrNotFound) {
		t.Fatalf("refs/heads/feature was created despite cancellation before commit: err=%v", err)
	}
}

func TestApplyIndividuallyRespectsCanceledContextMidLoop(t *testing.T) {
	dst := newTestRepo(t, true)
	commit := dst.commit("main", map[string]string{"a.txt": "hello"})
	sess := dialSession(t, dst.dir).(*session)
	ctx := newCountingContext(t, 1)
	plan := []transport.Update{{Name: "refs/heads/x", Old: hash.Zero, New: commit}}
	statuses := make(map[string]transport.RefStatus)
	sess.applyIndividually(ctx, plan, statuses)
	if statuses["refs/heads/x"].OK {
		t.Fatalf("statuses = %+v, want the update rejected by cancellation", statuses)
	}
	if _, err := dst.refs.Lookup(refs.BranchName("x")); !errors.Is(err, refs.ErrNotFound) {
		t.Fatalf("refs/heads/x was created despite cancellation: err=%v", err)
	}
}

func TestIsForced(t *testing.T) {
	cases := []struct {
		name    string
		options []string
		ref     string
		want    bool
	}{
		{"no options", nil, "refs/heads/main", false},
		{"global force", []string{"force"}, "refs/heads/main", true},
		{"scoped match", []string{"force:refs/heads/main"}, "refs/heads/main", true},
		{"scoped mismatch", []string{"force:refs/heads/other"}, "refs/heads/main", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isForced(c.options, c.ref); got != c.want {
				t.Fatalf("isForced(%v, %q) = %v, want %v", c.options, c.ref, got, c.want)
			}
		})
	}
}
