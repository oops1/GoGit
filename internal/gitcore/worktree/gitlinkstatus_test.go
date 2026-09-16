package worktree

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/index"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/repo"
)

func newTestRepoAt(t *testing.T, dir string) *testRepo {
	t.Helper()
	r, err := repo.Init(dir, repo.InitOptions{InitialBranch: "main"})
	if err != nil {
		t.Fatalf("repo.Init returned error %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })
	db, err := odb.Open(r.ObjectsDir(), odb.Options{})
	if err != nil {
		t.Fatalf("odb.Open returned error %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store, err := refs.Open(refs.Options{GitDir: r.GitDir(), CommonDir: r.CommonDir(), Committer: func() object.Signature {
		return object.Signature{Name: "ann", Email: "ann@example.com", When: time.Unix(1700000000, 0)}
	}})
	if err != nil {
		t.Fatalf("refs.Open returned error %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return &testRepo{t: t, dir: dir, repo: r, db: db, refs: store, idx: index.New(index.Version2), clock: 1700000000}
}

func submoduleFixture(t *testing.T) (*testRepo, *testRepo, hash.ObjectID) {
	t.Helper()
	super := newTestRepo(t)
	super.stage("top.txt", "top\n")
	nested := newTestRepoAt(t, super.path("sub"))
	nested.stage("a.txt", "a\n")
	pointer := nested.commit("one")
	nested.saveIndex()
	super.idx.Add(index.Entry{Path: "sub", Mode: object.ModeSubmodule, ID: pointer, Stage: index.StageMerged})
	super.commit("superproject")
	return super, nested, pointer
}

func submoduleEntry(t *testing.T, w *Worktree) (Entry, bool) {
	t.Helper()
	status, err := w.Status(t.Context())
	if err != nil {
		t.Fatalf("Status returned error %v", err)
	}
	entry, ok := entryMap(status.Entries)["sub"]
	return entry, ok
}

func TestStatusReportsNothingForASubmoduleAtItsRecordedCommit(t *testing.T) {
	super, _, _ := submoduleFixture(t)

	if entry, ok := submoduleEntry(t, super.open()); ok {
		t.Fatalf("sub = %+v", entry)
	}
}

func TestStatusReportsASubmoduleWhoseCheckedOutCommitMoved(t *testing.T) {
	super, nested, _ := submoduleFixture(t)
	nested.stage("b.txt", "b\n")
	nested.commit("two")
	nested.saveIndex()

	entry, ok := submoduleEntry(t, super.open())

	if !ok || entry.Unstaged != StatusModified || entry.Submodule != (SubmoduleChange{CommitChanged: true}) {
		t.Fatalf("sub = %+v, %v", entry, ok)
	}
}

func TestStatusReportsChangesInsideASubmodule(t *testing.T) {
	tests := []struct {
		name  string
		setup func(nested *testRepo)
		want  SubmoduleChange
	}{
		{"modified file", func(nested *testRepo) { nested.writeFile("a.txt", "changed\n") }, SubmoduleChange{Modified: true}},
		{"untracked file", func(nested *testRepo) { nested.writeFile("new.txt", "new\n") }, SubmoduleChange{Untracked: true}},
		{"staged file", func(nested *testRepo) {
			nested.stage("staged.txt", "staged\n")
			nested.saveIndex()
		}, SubmoduleChange{Modified: true}},
		{"staged deletion kept as an untracked file", func(nested *testRepo) {
			nested.unstage("a.txt")
			nested.saveIndex()
		}, SubmoduleChange{Modified: true, Untracked: true}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			super, nested, _ := submoduleFixture(t)
			tc.setup(nested)

			entry, ok := submoduleEntry(t, super.open())

			if !ok || entry.Unstaged != StatusModified || entry.Submodule != tc.want {
				t.Fatalf("sub = %+v, %v; want %+v", entry, ok, tc.want)
			}
		})
	}
}

func TestStatusCountsUntrackedContentOfANestedSubmoduleAsUntrackedOnly(t *testing.T) {
	super, nested, _ := submoduleFixture(t)
	inner := newTestRepoAt(t, nested.path("inner"))
	inner.stage("i.txt", "i\n")
	innerPointer := inner.commit("inner")
	inner.saveIndex()
	inner.writeFile("loose.txt", "loose\n")
	nested.idx.Add(index.Entry{Path: "inner", Mode: object.ModeSubmodule, ID: innerPointer, Stage: index.StageMerged})
	nested.saveIndex()

	entry, ok := submoduleEntry(t, super.open())

	if !ok || entry.Submodule != (SubmoduleChange{Untracked: true}) {
		t.Fatalf("sub = %+v, %v", entry, ok)
	}
	nested.commit("with inner")

	entry, ok = submoduleEntry(t, super.open())

	if !ok || entry.Submodule != (SubmoduleChange{CommitChanged: true, Untracked: true}) {
		t.Fatalf("after the commit sub = %+v, %v", entry, ok)
	}
}

func TestStatusIgnoresAnUninitialisedOrSkippedSubmodule(t *testing.T) {
	super := newTestRepo(t)
	pointer := hash.SumSHA1("commit", []byte("elsewhere"))
	super.mkdir("empty")
	super.idx.Add(index.Entry{Path: "empty", Mode: object.ModeSubmodule, ID: pointer, Stage: index.StageMerged})
	newTestRepoAt(t, super.path("skipped")).writeFile("loose.txt", "x\n")
	super.idx.Add(index.Entry{Path: "skipped", Mode: object.ModeSubmodule, ID: pointer, Stage: index.StageMerged, SkipWorktree: true})
	super.commit("superproject")

	status, err := super.open().Status(t.Context())

	if err != nil || len(status.Entries) != 0 {
		t.Fatalf("status = %+v, %v", status.Entries, err)
	}
}

func TestStatusReportsUntrackedContentOfAnUnbornSubmodule(t *testing.T) {
	super := newTestRepo(t)
	newTestRepoAt(t, super.path("sub")).writeFile("loose.txt", "x\n")
	super.idx.Add(index.Entry{Path: "sub", Mode: object.ModeSubmodule, ID: hash.SumSHA1("commit", []byte("x")), Stage: index.StageMerged})
	super.commit("superproject")

	entry, ok := submoduleEntry(t, super.open())

	if !ok || entry.Submodule != (SubmoduleChange{Untracked: true}) {
		t.Fatalf("sub = %+v, %v", entry, ok)
	}
}

func swapNested[F any](t *testing.T, seam *F, replacement F) {
	t.Helper()
	original := *seam
	*seam = replacement
	t.Cleanup(func() { *seam = original })
}

func TestStatusFailsWhenASubmoduleCannotBeRead(t *testing.T) {
	boom := errors.New("boom")
	tests := []struct {
		name  string
		setup func(t *testing.T, nested *testRepo)
	}{
		{"repository", func(t *testing.T, _ *testRepo) {
			swapNested(t, &nestedOpenRepository, func(repo.Layout, repo.OpenOptions) (*repo.Repository, error) { return nil, boom })
		}},
		{"objects", func(t *testing.T, _ *testRepo) {
			swapNested(t, &nestedOpenObjects, func(string, odb.Options) (*odb.DB, error) { return nil, boom })
		}},
		{"index", func(t *testing.T, nested *testRepo) {
			if err := os.WriteFile(nested.repo.IndexFile(), []byte("not an index"), 0o666); err != nil {
				t.Fatal(err)
			}
		}},
		{"head commit", func(_ *testing.T, nested *testRepo) {
			nested.setBranchTarget(hash.SumSHA1("commit", []byte("missing")))
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			super, nested, _ := submoduleFixture(t)
			w := super.open()
			tc.setup(t, nested)

			if _, err := w.Status(t.Context()); !errors.Is(err, ErrReadSubmodule) {
				t.Fatalf("Status = %v, want ErrReadSubmodule", err)
			}
		})
	}
}
