//go:build !race

package ops

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/diff"
	"github.com/oops1/gogit/internal/gitcore/hash"
)

func stashSeams() []faultSeam {
	return append(mergeSeams(), faultSeam{"stat file", func(t *testing.T, failAt int, calls *int) {
		swapSeam(t, &fsRootLstat, func(original func(*os.Root, string) (fs.FileInfo, error)) func(*os.Root, string) (fs.FileInfo, error) {
			return func(root *os.Root, name string) (fs.FileInfo, error) {
				if hit(calls, failAt) {
					return nil, errInjected
				}
				return original(root, name)
			}
		})
	}}, faultSeam{"open directory", func(t *testing.T, failAt int, calls *int) {
		swapSeam(t, &fsRootOpen, func(original func(*os.Root, string) (*os.File, error)) func(*os.Root, string) (*os.File, error) {
			return func(root *os.Root, name string) (*os.File, error) {
				if hit(calls, failAt) {
					return nil, errInjected
				}
				return original(root, name)
			}
		})
	}}, faultSeam{"hash content", func(t *testing.T, failAt int, calls *int) {
		swapSeam(t, &hashSum, func(original func(hash.Format, string, []byte) (hash.ObjectID, error)) func(hash.Format, string, []byte) (hash.ObjectID, error) {
			return func(format hash.Format, kind string, data []byte) (hash.ObjectID, error) {
				if hit(calls, failAt) {
					return hash.Zero, errInjected
				}
				return original(format, kind, data)
			}
		})
	}})
}

func untrackedChanges(tr *testRepo) {
	tr.t.Helper()
	stashChanges(tr)
	tr.writeFile(".git/info/exclude", "*.log\n")
	tr.writeFile("loose/u.txt", "u\n")
	tr.writeFile("loose/skip.log", "log\n")
	tr.writeFile("top.log", "log\n")
}

func stagedLineChanges(tr *testRepo) {
	tr.t.Helper()
	tr.commitFiles("lines", map[string]string{"m": tenLines("m")})
	stashChanges(tr)
	staged := changeLine(tenLines("m"), 5, "STAGED")
	tr.writeFile("m", staged)
	tr.stageAll("m")
	tr.writeFile("m", changeLine(staged, 9, "LATER"))
}

func (r *testRepo) stageAll(paths ...string) {
	r.t.Helper()
	if err := Stage(r.t.Context(), r.repo, paths, StageOptions{}); err != nil {
		r.t.Fatalf("Stage returned error %v", err)
	}
}

func pushWith(opts StashOptions) func(ctx context.Context, tr *testRepo) error {
	return func(ctx context.Context, tr *testRepo) error {
		opts.When = mergeTime
		_, err := StashPush(ctx, tr.repo, opts)
		return err
	}
}

func stashFaultScenarios() []faultScenario {
	stashed := func(tr *testRepo) {
		stashChanges(tr)
		tr.stash(StashOptions{})
	}
	apply := func(ctx context.Context, tr *testRepo) error {
		_, err := StashApply(ctx, tr.repo, 0, StashApplyOptions{})
		return err
	}
	pop := func(ctx context.Context, tr *testRepo) error {
		_, err := StashPop(ctx, tr.repo, 0, StashApplyOptions{})
		return err
	}
	return []faultScenario{
		{"push", stashChanges, func(ctx context.Context, tr *testRepo) error {
			_, err := StashPush(ctx, tr.repo, StashOptions{When: mergeTime})
			return err
		}},
		{"list", stashed, func(ctx context.Context, tr *testRepo) error {
			_, err := StashList(ctx, tr.repo)
			return err
		}},
		{"apply", stashed, apply},
		{"pop", stashed, pop},
		{"pop with conflict", func(tr *testRepo) {
			stashed(tr)
			tr.commitFiles("upstream", map[string]string{"a": "upstream\n"})
		}, pop},
		{"apply over a directory rename split", func(tr *testRepo) {
			tr.commitFiles("base", map[string]string{"lib/a": tenLines("a"), "lib/b": tenLines("b")})
			tr.writeFile("lib/new", "new\n")
			if err := Stage(tr.t.Context(), tr.repo, []string{"lib/new"}, StageOptions{}); err != nil {
				tr.t.Fatal(err)
			}
			if _, err := StashPush(tr.t.Context(), tr.repo, StashOptions{When: mergeTime}); err != nil {
				tr.t.Fatal(err)
			}
			tr.commitFiles("ours", map[string]string{"lib/a": "", "lib/b": "", "x/a": tenLines("a"), "y/b": tenLines("b")})
		}, apply},
		{"drop", stashed, func(ctx context.Context, tr *testRepo) error {
			return StashDrop(ctx, tr.repo, 0)
		}},
		{"push untracked", untrackedChanges, pushWith(StashOptions{IncludeUntracked: true})},
		{"push ignored keeping the index", untrackedChanges, pushWith(StashOptions{IncludeIgnored: true, KeepIndex: true})},
		{"push paths", untrackedChanges, pushWith(StashOptions{IncludeUntracked: true, KeepIndex: true, Paths: []string{"b", "new", "loose"}})},
		{"push staged", stagedLineChanges, pushWith(StashOptions{Staged: true})},
		{"push staged paths", stagedLineChanges, pushWith(StashOptions{Staged: true, Paths: []string{"m", "b"}})},
		{"apply index", func(tr *testRepo) {
			stashed(tr)
			tr.commitFiles("other", map[string]string{"z": "z\n"})
		}, func(ctx context.Context, tr *testRepo) error {
			_, err := StashApply(ctx, tr.repo, 0, StashApplyOptions{Index: true})
			return err
		}},
		{"apply index on shifted lines", func(tr *testRepo) {
			tr.commitFiles("lines", map[string]string{"m": tenLines("m")})
			tr.writeFile("m", changeLine(tenLines("m"), 5, "STAGED"))
			tr.stageAll("m")
			tr.stash(StashOptions{})
			tr.commitFiles("shift", map[string]string{"m": "top\n" + tenLines("m")})
		}, func(ctx context.Context, tr *testRepo) error {
			_, err := StashApply(ctx, tr.repo, 0, StashApplyOptions{Index: true})
			return err
		}},
		{"pop untracked", func(tr *testRepo) {
			untrackedChanges(tr)
			tr.stash(StashOptions{IncludeUntracked: true})
		}, pop},
		{"show", func(tr *testRepo) {
			untrackedChanges(tr)
			tr.stash(StashOptions{IncludeUntracked: true})
		}, func(ctx context.Context, tr *testRepo) error {
			_, err := StashShow(ctx, tr.repo, 0, diff.Options{})
			return err
		}},
		{"switch merging", func(tr *testRepo) {
			switchMergingRepo(tr)
			tr.writeFile("f", changeLine(tenLines("f"), 9, "LOCAL"))
		}, func(ctx context.Context, tr *testRepo) error {
			_, err := SwitchMerging(ctx, tr.repo, "feature")
			return err
		}},
	}
}

func TestStashFailuresSurfaceTheCauseAtEveryStep(t *testing.T) {
	for _, scenario := range stashFaultScenarios() {
		for _, seam := range stashSeams() {
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

func TestStashStopsWhenTheContextIsCancelled(t *testing.T) {
	for _, scenario := range stashFaultScenarios() {
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
