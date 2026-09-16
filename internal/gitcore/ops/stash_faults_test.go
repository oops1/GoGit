//go:build !race

package ops

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"testing"
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
	}})
}

func stashFaultScenarios() []faultScenario {
	stashed := func(tr *testRepo) {
		stashChanges(tr)
		tr.stash(StashOptions{})
	}
	apply := func(ctx context.Context, tr *testRepo) error {
		_, err := StashApply(ctx, tr.repo, 0)
		return err
	}
	pop := func(ctx context.Context, tr *testRepo) error {
		_, err := StashPop(ctx, tr.repo, 0)
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
