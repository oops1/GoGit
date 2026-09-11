//go:build !race

package ops

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/refs"
)

type faultSeam struct {
	name    string
	install func(t *testing.T, failAt int, calls *int)
}

func swapSeam[F any](t *testing.T, seam *F, wrap func(original F) F) {
	t.Helper()
	original := *seam
	*seam = wrap(original)
	t.Cleanup(func() { *seam = original })
}

func hit(calls *int, failAt int) bool {
	*calls++
	return *calls == failAt
}

func mergeSeams() []faultSeam {
	return []faultSeam{
		{"read object", func(t *testing.T, failAt int, calls *int) {
			swapSeam(t, &dbGet, func(original func(*odb.DB, hash.ObjectID) (object.Type, []byte, error)) func(*odb.DB, hash.ObjectID) (object.Type, []byte, error) {
				return func(db *odb.DB, id hash.ObjectID) (object.Type, []byte, error) {
					if hit(calls, failAt) {
						return 0, nil, errInjected
					}
					return original(db, id)
				}
			})
		}},
		{"read tree", func(t *testing.T, failAt int, calls *int) {
			swapSeam(t, &dbTree, func(original func(*odb.DB, hash.ObjectID) (*object.Tree, error)) func(*odb.DB, hash.ObjectID) (*object.Tree, error) {
				return func(db *odb.DB, id hash.ObjectID) (*object.Tree, error) {
					if hit(calls, failAt) {
						return nil, errInjected
					}
					return original(db, id)
				}
			})
		}},
		{"read commit", func(t *testing.T, failAt int, calls *int) {
			swapSeam(t, &dbCommit, func(original func(*odb.DB, hash.ObjectID) (*object.Commit, error)) func(*odb.DB, hash.ObjectID) (*object.Commit, error) {
				return func(db *odb.DB, id hash.ObjectID) (*object.Commit, error) {
					if hit(calls, failAt) {
						return nil, errInjected
					}
					return original(db, id)
				}
			})
		}},
		{"peel", func(t *testing.T, failAt int, calls *int) {
			swapSeam(t, &dbPeel, func(original func(*odb.DB, hash.ObjectID) (object.Type, hash.ObjectID, error)) func(*odb.DB, hash.ObjectID) (object.Type, hash.ObjectID, error) {
				return func(db *odb.DB, id hash.ObjectID) (object.Type, hash.ObjectID, error) {
					if hit(calls, failAt) {
						return 0, hash.Zero, errInjected
					}
					return original(db, id)
				}
			})
		}},
		{"write object", func(t *testing.T, failAt int, calls *int) {
			swapSeam(t, &dbPut, func(original func(*odb.DB, object.Type, []byte) (hash.ObjectID, error)) func(*odb.DB, object.Type, []byte) (hash.ObjectID, error) {
				return func(db *odb.DB, kind object.Type, data []byte) (hash.ObjectID, error) {
					if hit(calls, failAt) {
						return hash.Zero, errInjected
					}
					return original(db, kind, data)
				}
			})
		}},
		{"write commit", func(t *testing.T, failAt int, calls *int) {
			swapSeam(t, &dbPutObject, func(original func(*odb.DB, object.Object) (hash.ObjectID, error)) func(*odb.DB, object.Object) (hash.ObjectID, error) {
				return func(db *odb.DB, o object.Object) (hash.ObjectID, error) {
					if hit(calls, failAt) {
						return hash.Zero, errInjected
					}
					return original(db, o)
				}
			})
		}},
		{"update ref", func(t *testing.T, failAt int, calls *int) {
			swapSeam(t, &txUpdate, func(original func(*refs.Transaction, refs.Name, hash.ObjectID, hash.ObjectID) error) func(*refs.Transaction, refs.Name, hash.ObjectID, hash.ObjectID) error {
				return func(tx *refs.Transaction, name refs.Name, next, old hash.ObjectID) error {
					if hit(calls, failAt) {
						return errInjected
					}
					return original(tx, name, next, old)
				}
			})
		}},
		{"write state", func(t *testing.T, failAt int, calls *int) {
			swapSeam(t, &fsRootWriteFile, func(original func(*os.Root, string, []byte, fs.FileMode) error) func(*os.Root, string, []byte, fs.FileMode) error {
				return func(root *os.Root, name string, data []byte, mode fs.FileMode) error {
					if hit(calls, failAt) {
						return errInjected
					}
					return original(root, name, data, mode)
				}
			})
		}},
		{"read file", func(t *testing.T, failAt int, calls *int) {
			swapSeam(t, &fsRootReadFile, func(original func(*os.Root, string) ([]byte, error)) func(*os.Root, string) ([]byte, error) {
				return func(root *os.Root, name string) ([]byte, error) {
					if hit(calls, failAt) {
						return nil, errInjected
					}
					return original(root, name)
				}
			})
		}},
		{"remove file", func(t *testing.T, failAt int, calls *int) {
			swapSeam(t, &fsRootRemove, func(original func(*os.Root, string) error) func(*os.Root, string) error {
				return func(root *os.Root, name string) error {
					if hit(calls, failAt) {
						return errInjected
					}
					return original(root, name)
				}
			})
		}},
		{"open file", func(t *testing.T, failAt int, calls *int) {
			swapSeam(t, &fsRootOpenFile, func(original func(*os.Root, string, int, fs.FileMode) (*os.File, error)) func(*os.Root, string, int, fs.FileMode) (*os.File, error) {
				return func(root *os.Root, name string, flag int, mode fs.FileMode) (*os.File, error) {
					if hit(calls, failAt) {
						return nil, errInjected
					}
					return original(root, name, flag, mode)
				}
			})
		}},
		{"open root", func(t *testing.T, failAt int, calls *int) {
			swapSeam(t, &fsOpenRoot, func(original func(string) (*os.Root, error)) func(string) (*os.Root, error) {
				return func(dir string) (*os.Root, error) {
					if hit(calls, failAt) {
						return nil, errInjected
					}
					return original(dir)
				}
			})
		}},
		{"open refs", func(t *testing.T, failAt int, calls *int) {
			swapSeam(t, &refsOpen, func(original func(refs.Options) (*refs.Store, error)) func(refs.Options) (*refs.Store, error) {
				return func(opts refs.Options) (*refs.Store, error) {
					if hit(calls, failAt) {
						return nil, errInjected
					}
					return original(opts)
				}
			})
		}},
	}
}

type faultScenario struct {
	name  string
	build func(tr *testRepo)
	run   func(ctx context.Context, tr *testRepo) error
}

func mergeFaultScenarios() []faultScenario {
	merge := func(opts MergeOptions) func(ctx context.Context, tr *testRepo) error {
		return func(ctx context.Context, tr *testRepo) error {
			opts.When = mergeTime
			_, err := Merge(ctx, tr.repo, "feature", opts)
			return err
		}
	}
	clean := func(tr *testRepo) {
		tr.fork(map[string]string{"f": changeLine(tenLines("f"), 0, "OURS")}, map[string]string{"g": changeLine(tenLines("g"), 0, "THEIRS"), "moved": tenLines("keep")})
	}
	return []faultScenario{
		{"fast-forward", func(tr *testRepo) {
			base := tr.commitFiles("base", map[string]string{"f": "one\n", "gone": "gone\n"})
			tr.createBranch("feature", base)
			tr.switchTo("feature")
			tr.commitFiles("next", map[string]string{"f": "two\n", "gone": ""})
			tr.switchTo("main")
		}, merge(MergeOptions{})},
		{"clean merge", clean, merge(MergeOptions{})},
		{"conflict", func(tr *testRepo) { tr.conflictingFork() }, merge(MergeOptions{})},
		{"squash", func(tr *testRepo) { tr.conflictingFork() }, merge(MergeOptions{Mode: MergeSquash})},
		{"criss-cross", func(tr *testRepo) {
			f, g := tenLines("f"), tenLines("g")
			base := tr.commitFiles("base", map[string]string{"f": f, "g": g})
			tr.createBranch("feature", base)
			tr.commitFiles("ours 1", map[string]string{"f": changeLine(f, 0, "OURS")})
			tr.switchTo("feature")
			tr.commitFiles("theirs 1", map[string]string{"g": changeLine(g, 0, "THEIRS")})
			if _, err := tr.merge("main", MergeOptions{}); err != nil {
				tr.t.Fatal(err)
			}
			tr.switchTo("main")
			if _, err := tr.merge("feature~1", MergeOptions{}); err != nil {
				tr.t.Fatal(err)
			}
			tr.commitFiles("ours 2", map[string]string{"f": changeLine(f, 9, "OURS AGAIN")})
			tr.switchTo("feature")
			tr.commitFiles("theirs 2", map[string]string{"g": changeLine(g, 9, "THEIRS AGAIN")})
			tr.switchTo("main")
		}, merge(MergeOptions{})},
		{"abort", func(tr *testRepo) {
			tr.fork(map[string]string{"f": changeLine(tenLines("f"), 4, "OURS"), "gone": "gone\n"}, map[string]string{"f": changeLine(tenLines("f"), 4, "THEIRS"), "new": "new\n"})
			if _, err := tr.merge("feature", MergeOptions{}); err != nil {
				tr.t.Fatal(err)
			}
		}, func(ctx context.Context, tr *testRepo) error { return AbortMerge(ctx, tr.repo) }},
		{"take a side", func(tr *testRepo) {
			tr.fork(map[string]string{"f": changeLine(tenLines("f"), 4, "OURS"), "g": changeLine(tenLines("g"), 4, "OURS")}, map[string]string{"f": changeLine(tenLines("f"), 4, "THEIRS"), "g": ""})
			if _, err := tr.merge("feature", MergeOptions{}); err != nil {
				tr.t.Fatal(err)
			}
		}, func(ctx context.Context, tr *testRepo) error {
			return ResolveConflicts(ctx, tr.repo, []string{"f", "g"}, TakeTheirs)
		}},
		{"conclude", func(tr *testRepo) {
			tr.conflictingFork()
			if _, err := tr.merge("feature", MergeOptions{}); err != nil {
				tr.t.Fatal(err)
			}
			tr.writeFile("f", "resolved\n")
			if err := Stage(tr.t.Context(), tr.repo, []string{"f"}, StageOptions{}); err != nil {
				tr.t.Fatal(err)
			}
		}, func(ctx context.Context, tr *testRepo) error {
			_, err := Commit(ctx, tr.repo, CommitOptions{Message: "merged", When: mergeTime})
			return err
		}},
	}
}

func TestMergeFailuresSurfaceTheCauseAtEveryStep(t *testing.T) {
	for _, scenario := range mergeFaultScenarios() {
		for _, seam := range mergeSeams() {
			t.Run(scenario.name+"/"+seam.name, func(t *testing.T) {
				for failAt := 1; ; failAt++ {
					tr := newTestRepo(t)
					scenario.build(tr)
					calls := 0
					err := runWithFault(t, func(t *testing.T) error {
						seam.install(t, failAt, &calls)
						return scenario.run(t.Context(), tr)
					})
					if err != nil && !errors.Is(err, errInjected) {
						t.Fatalf("failure at call %d surfaced as %v", failAt, err)
					}
					if calls < failAt {
						return
					}
				}
			})
		}
	}
}

func runWithFault(t *testing.T, body func(t *testing.T) error) error {
	t.Helper()
	var err error
	t.Run("step", func(t *testing.T) { err = body(t) })
	return err
}

func TestMergeStopsWhenTheContextIsCancelled(t *testing.T) {
	for _, scenario := range mergeFaultScenarios() {
		t.Run(scenario.name, func(t *testing.T) {
			for failAt := 1; ; failAt++ {
				tr := newTestRepo(t)
				scenario.build(tr)
				ctx := newCountingContext(t, failAt).(countingContext)
				err := scenario.run(ctx, tr)
				if err != nil && !errors.Is(err, context.Canceled) {
					t.Fatalf("cancellation at check %d surfaced as %v", failAt, err)
				}
				if *ctx.calls < failAt {
					return
				}
			}
		})
	}
}
