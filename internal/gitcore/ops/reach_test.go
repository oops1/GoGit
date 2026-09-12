package ops

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/index"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/refs"
)

func swapMaint[T any](t testing.TB, target *T, replacement T) {
	t.Helper()
	prev := *target
	*target = replacement
	t.Cleanup(func() { *target = prev })
}

func putMaintBlob(t testing.TB, r *testRepo, content string) hash.ObjectID {
	t.Helper()
	id, err := r.db().Put(object.TypeBlob, []byte(content))
	if err != nil {
		t.Fatalf("Put returned error %v", err)
	}
	return id
}

func putMaintCommit(t testing.TB, r *testRepo, tree hash.ObjectID, parents ...hash.ObjectID) hash.ObjectID {
	t.Helper()
	r.clock += 60
	sig := object.Signature{Name: "ann", Email: "ann@example.com", When: time.Unix(r.clock, 0)}
	id, err := r.db().PutObject(&object.Commit{Tree: tree, Parents: parents, Author: sig, Committer: sig, Message: "m\n"})
	if err != nil {
		t.Fatalf("PutObject returned error %v", err)
	}
	return id
}

func setMaintRef(t testing.TB, r *testRepo, name refs.Name, id hash.ObjectID) {
	t.Helper()
	tx := r.refs().Begin()
	if err := tx.Set(name, id); err != nil {
		t.Fatalf("Set returned error %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit returned error %v", err)
	}
}

func writeGitFile(t testing.TB, dir, rel, content string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o777); err != nil {
		t.Fatalf("MkdirAll returned error %v", err)
	}
	if err := os.WriteFile(full, []byte(content), 0o666); err != nil {
		t.Fatalf("WriteFile returned error %v", err)
	}
}

func writeReflogTo(t testing.TB, r *testRepo, id hash.ObjectID) {
	t.Helper()
	line := hash.Zero.String() + " " + id.String() + " ann <ann@example.com> 1700000000 +0000\tupdate\n"
	writeGitFile(t, r.repo.CommonDir(), "logs/refs/heads/odd", line)
}

func walkRepo(t *testing.T, r *testRepo) (*objectWalk, error) {
	t.Helper()
	return reachableObjects(t.Context(), r.repo, r.db())
}

func mustWalk(t *testing.T, r *testRepo) *objectWalk {
	t.Helper()
	walk, err := walkRepo(t, r)
	if err != nil {
		t.Fatalf("reachableObjects returned error %v", err)
	}
	return walk
}

type maintHistory struct {
	blobA, blobB, subtree, tree1, tree2 hash.ObjectID
	first, second, tag, loose           hash.ObjectID
}

func buildMaintHistory(t *testing.T, r *testRepo) maintHistory {
	t.Helper()
	var h maintHistory
	h.blobA = putMaintBlob(t, r, "a\n")
	h.blobB = putMaintBlob(t, r, "b\n")
	h.loose = putMaintBlob(t, r, "nobody points here\n")
	h.subtree = putTree(t, r, object.TreeEntry{Mode: object.ModeBlob, Name: "a.txt", ID: h.blobA})
	h.tree1 = putTree(t, r, object.TreeEntry{Mode: object.ModeTree, Name: "dir", ID: h.subtree})
	h.tree2 = putTree(t, r,
		object.TreeEntry{Mode: object.ModeBlob, Name: "b.txt", ID: h.blobB},
		object.TreeEntry{Mode: object.ModeTree, Name: "dir", ID: h.subtree},
		object.TreeEntry{Mode: object.ModeSubmodule, Name: "sub", ID: hash.SumSHA1("commit", []byte("elsewhere"))},
	)
	h.first = putMaintCommit(t, r, h.tree1)
	h.second = putMaintCommit(t, r, h.tree2, h.first)
	tag, err := r.db().PutObject(&object.Tag{Object: h.first, ObjectType: object.TypeCommit, Name: "v1", Message: "v1\n"})
	if err != nil {
		t.Fatalf("PutObject returned error %v", err)
	}
	h.tag = tag
	setMaintRef(t, r, refs.BranchName("main"), h.second)
	setMaintRef(t, r, refs.TagName("v1"), h.tag)
	return h
}

func TestTheWalkReachesEverythingBranchesAndTagsPointAt(t *testing.T) {
	r := newTestRepo(t)
	h := buildMaintHistory(t, r)

	walk := mustWalk(t, r)

	for name, id := range map[string]hash.ObjectID{"blob a": h.blobA, "blob b": h.blobB, "subtree": h.subtree, "first tree": h.tree1, "second tree": h.tree2, "first": h.first, "second": h.second, "tag": h.tag} {
		if !walk.reachable(id) {
			t.Fatalf("%s is not reachable", name)
		}
	}
	if walk.reachable(h.loose) {
		t.Fatal("an unreferenced blob counts as reachable")
	}
	if len(walk.commits) != 2 {
		t.Fatalf("commits = %+v", walk.commits)
	}
}

func TestReflogsPseudoRefsAndLinkedWorktreesKeepTheirCommitsAlive(t *testing.T) {
	r := newTestRepo(t)
	h := buildMaintHistory(t, r)
	orphan := putMaintCommit(t, r, h.tree1)
	previous := putMaintCommit(t, r, h.tree2)
	detached := putMaintCommit(t, r, h.subtree)
	setMaintRef(t, r, refs.BranchName("topic"), orphan)
	setMaintRef(t, r, refs.BranchName("topic"), h.second)
	writeGitFile(t, r.repo.GitDir(), "ORIG_HEAD", previous.String()+"\n")
	writeGitFile(t, r.repo.CommonDir(), "worktrees/elsewhere/HEAD", detached.String()+"\n")
	writeGitFile(t, r.repo.CommonDir(), "worktrees/README", "not a worktree\n")
	writeGitFile(t, r.repo.CommonDir(), "logs/refs/heads/broken.lock", "ignored\n")

	walk := mustWalk(t, r)

	for name, id := range map[string]hash.ObjectID{"reflog": orphan, "ORIG_HEAD": previous, "linked worktree HEAD": detached} {
		if !walk.reachable(id) {
			t.Fatalf("the commit kept by %s is not reachable", name)
		}
	}
}

func TestTheIndexKeepsStagedBlobsAndItsCachedTrees(t *testing.T) {
	r := newTestRepo(t)
	buildMaintHistory(t, r)
	staged := putMaintBlob(t, r, "staged only\n")
	idx := r.index()
	idx.Add(index.Entry{Path: "dir/staged.txt", Mode: object.ModeBlob, ID: staged})
	if _, err := idx.WriteTree(r.db()); err != nil {
		t.Fatal(err)
	}
	cached := idx.CacheTree.Find("dir").ID
	idx.Add(index.Entry{Path: "sub", Mode: object.ModeSubmodule, ID: hash.SumSHA1("commit", []byte("absent"))})
	idx.Add(index.Entry{Path: "later.txt", Mode: object.ModeBlob, ID: hash.SumSHA1("blob", []byte("absent")), IntentToAdd: true})
	r.saveIndex(idx)

	walk := mustWalk(t, r)

	if !walk.reachable(staged) || !walk.reachable(cached) {
		t.Fatalf("staged = %v, cached tree = %v", walk.reachable(staged), walk.reachable(cached))
	}
}

func TestAShallowCommitHidesItsParents(t *testing.T) {
	r := newTestRepo(t)
	h := buildMaintHistory(t, r)
	setMaintRef(t, r, refs.TagName("v1"), h.second)
	writeGitFile(t, r.repo.CommonDir(), "shallow", h.second.String()+"\n")
	if err := os.RemoveAll(filepath.Join(r.repo.CommonDir(), "logs")); err != nil {
		t.Fatal(err)
	}

	walk := mustWalk(t, r)

	if walk.reachable(h.first) {
		t.Fatal("the parent of a shallow commit was walked")
	}
	if len(walk.commits) != 1 || walk.commits[0].Parents != nil {
		t.Fatalf("commits = %+v", walk.commits)
	}
}

func TestTheWalkRefusesAHistoryWithHoles(t *testing.T) {
	for _, tt := range []struct {
		name  string
		setup func(t *testing.T, r *testRepo)
		want  error
	}{
		{"a branch at a missing commit", func(t *testing.T, r *testRepo) {
			writeGitFile(t, r.repo.CommonDir(), "refs/heads/lost", hash.SumSHA1("commit", []byte("lost")).String()+"\n")
		}, odb.ErrNotFound},
		{"a staged blob that was never written", func(t *testing.T, r *testRepo) {
			idx := r.index()
			idx.Add(index.Entry{Path: "lost.txt", Mode: object.ModeBlob, ID: hash.SumSHA1("blob", []byte("lost"))})
			r.saveIndex(idx)
		}, odb.ErrNotFound},
		{"a commit that does not parse", func(t *testing.T, r *testRepo) {
			id, err := r.db().Put(object.TypeCommit, []byte("garbage"))
			if err != nil {
				t.Fatal(err)
			}
			setMaintRef(t, r, refs.BranchName("bad"), id)
		}, object.ErrMalformed},
		{"a tree that does not parse", func(t *testing.T, r *testRepo) {
			id, err := r.db().Put(object.TypeTree, []byte("garbage"))
			if err != nil {
				t.Fatal(err)
			}
			writeReflogTo(t, r, id)
		}, object.ErrMalformed},
		{"a tag that does not parse", func(t *testing.T, r *testRepo) {
			id, err := r.db().Put(object.TypeTag, []byte("garbage"))
			if err != nil {
				t.Fatal(err)
			}
			writeReflogTo(t, r, id)
		}, object.ErrMalformed},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := newTestRepo(t)
			tt.setup(t, r)

			if _, err := walkRepo(t, r); !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestTheWalkReportsEveryRootItCannotRead(t *testing.T) {
	boom := errors.New("boom")
	for _, tt := range []struct {
		name  string
		setup func(t *testing.T, r *testRepo)
	}{
		{"a broken shallow file", func(t *testing.T, r *testRepo) {
			writeGitFile(t, r.repo.CommonDir(), "shallow", "not a hash\n")
		}},
		{"an unreadable worktrees directory", func(t *testing.T, r *testRepo) {
			swapMaint(t, &worktreeReadDir, func(string) ([]os.DirEntry, error) { return nil, boom })
		}},
		{"a ref store that will not open", func(t *testing.T, r *testRepo) {
			swapMaint(t, &refsOpen, func(refs.Options) (*refs.Store, error) { return nil, boom })
		}},
		{"a malformed branch", func(t *testing.T, r *testRepo) {
			writeGitFile(t, r.repo.CommonDir(), "refs/heads/bad", "garbage\n")
		}},
		{"a malformed ORIG_HEAD", func(t *testing.T, r *testRepo) {
			writeGitFile(t, r.repo.GitDir(), "ORIG_HEAD", "garbage\n")
		}},
		{"a reflog directory that cannot be walked", func(t *testing.T, r *testRepo) {
			swapMaint(t, &reachWalkDir, func(root string, fn fs.WalkDirFunc) error {
				return fn(filepath.Join(root, "refs"), nil, boom)
			})
		}},
		{"a reflog that cannot be read", func(t *testing.T, r *testRepo) {
			writeGitFile(t, r.repo.CommonDir(), "logs/refs/heads/main", "garbage\n")
		}},
		{"an index that cannot be read", func(t *testing.T, r *testRepo) {
			swapMaint(t, &reachReadIndex, func(string) (*index.Index, error) { return nil, boom })
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := newTestRepo(t)
			buildMaintHistory(t, r)
			tt.setup(t, r)

			if _, err := walkRepo(t, r); err == nil {
				t.Fatal("the walk went on over a root it could not read")
			}
		})
	}
}

func TestTheWalkStopsWhenItsContextIsCancelled(t *testing.T) {
	r := newTestRepo(t)
	buildMaintHistory(t, r)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, err := reachableObjects(ctx, r.repo, r.db()); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestABlobThatCannotBeCheckedStopsTheWalk(t *testing.T) {
	r := newTestRepo(t)
	db, err := odb.Open(r.repo.ObjectsDir(), odb.Options{})
	if err != nil {
		t.Fatal(err)
	}
	walk, err := newObjectWalk(t.Context(), r.repo, db)
	if err != nil {
		t.Fatal(err)
	}
	walk.push(putMaintBlob(t, r, "x\n"), object.TypeBlob)
	_ = db.Close()

	if err := walk.run(); err == nil {
		t.Fatal("a blob check on a closed database passed")
	}
}

func TestAWalkOverABareRepositoryHasNoIndexToRead(t *testing.T) {
	r := newBareTestRepo(t)
	blob := putMaintBlob(t, r, "bare\n")
	tree := putTree(t, r, object.TreeEntry{Mode: object.ModeBlob, Name: "f", ID: blob})
	commit := putMaintCommit(t, r, tree)
	writeGitFile(t, r.repo.CommonDir(), "refs/heads/main", commit.String()+"\n")

	walk, err := reachableObjects(t.Context(), r.repo, r.db())
	if err != nil {
		t.Fatal(err)
	}
	if !walk.reachable(blob) {
		t.Fatal("the blob of a bare repository is not reachable")
	}
}
