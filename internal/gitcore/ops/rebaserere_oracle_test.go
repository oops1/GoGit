//go:build oracle

package ops

import (
	"slices"
	"testing"
	"time"
)

func TestOracleARebaseRemembersResolutionsLikeGit(t *testing.T) {
	for _, action := range []string{"continue", "skip", "abort"} {
		t.Run(action, func(t *testing.T) {
			o := newOracle(t)
			gitSide := rerereRepo(t, o, "git", false)
			ourSide := rerereRepo(t, o, "ours", false)
			for _, side := range []*mergeBuilder{gitSide, ourSide} {
				side.git("checkout", "-q", "feature")
				side.commit("feature h", map[string]string{"h": "h\n"})
			}

			if _, err := gitSide.dated().attempt(gitSide.dir, "rebase", "main"); err == nil {
				t.Fatal("git rebased without a conflict")
			}
			ourSide.dated()
			result, err := Rebase(t.Context(), o.openRepo(ourSide.dir), "main", RebaseOptions{When: time.Unix(ourSide.clock, 0).UTC()})
			if err != nil || result.Finished() {
				t.Fatalf("result = %+v, %v", result, err)
			}

			if action == "continue" {
				for _, side := range []*mergeBuilder{gitSide, ourSide} {
					resolveByHand(t, side)
					side.o.run(side.dir, "add", "f")
				}
			}
			runner := gitSide.dated()
			runner.env = append(slices.Clone(runner.env), "GIT_EDITOR=true")
			runner.run(gitSide.dir, "rebase", "--"+action)
			ourSide.dated()
			ours := RebaseOptions{When: time.Unix(ourSide.clock, 0).UTC()}
			r := o.openRepo(ourSide.dir)
			switch action {
			case "continue":
				_, err = ContinueRebase(t.Context(), r, ours)
			case "skip":
				_, err = SkipRebase(t.Context(), r, ours)
			default:
				err = AbortOperation(t.Context(), r)
			}
			if err != nil {
				t.Fatalf("%s returned error %v", action, err)
			}

			if got, want := rerereTree(t, ourSide.dir), rerereTree(t, gitSide.dir); got != want {
				t.Fatalf("rr-cache differs from git: %s", sectionDiff(got, want))
			}
			if got, want := stashStateFiles(o, ourSide.dir), stashStateFiles(o, gitSide.dir); got != want {
				t.Fatalf("state files differ from git:\n%s\nwant\n%s", got, want)
			}
			compareStashSides(o, gitSide.dir, ourSide.dir,
				[]string{"log", "-3", "--format=%H %T %an %ad %cn %cd %s"},
				[]string{"status", "--porcelain"},
			)
		})
	}
}
