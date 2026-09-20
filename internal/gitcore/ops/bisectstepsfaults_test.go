//go:build !race

package ops

import (
	"context"
	"errors"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/repo"
)

func failObjectDatabase(t *testing.T) {
	t.Helper()
	swapSeam(t, &odbOpen, func(func(string, odb.Options) (*odb.DB, error)) func(string, odb.Options) (*odb.DB, error) {
		return func(string, odb.Options) (*odb.DB, error) { return nil, errInjected }
	})
}

func failRefUpdates(t *testing.T) {
	t.Helper()
	swapSeam(t, &txSet, func(func(*refs.Transaction, refs.Name, hash.ObjectID) error) func(*refs.Transaction, refs.Name, hash.ObjectID) error {
		return func(*refs.Transaction, refs.Name, hash.ObjectID) error { return errInjected }
	})
}

func failRefCommits(t *testing.T) {
	t.Helper()
	swapSeam(t, &txCommit, func(original func(*refs.Transaction) error) func(*refs.Transaction) error {
		return func(tx *refs.Transaction) error {
			if err := original(tx); err != nil {
				return err
			}
			return errInjected
		}
	})
}

func TestStartBisectReportsEveryFailure(t *testing.T) {
	tests := map[string]func(t *testing.T, r *testRepo){
		"objects":          func(t *testing.T, _ *testRepo) { failObjectDatabase(t) },
		"old state":        func(t *testing.T, _ *testRepo) { failRemoveOf(t, bisectNamesFile) },
		"start file":       func(t *testing.T, _ *testRepo) { failWriteOf(t, bisectStartFile) },
		"names file":       func(t *testing.T, _ *testRepo) { failWriteOf(t, bisectNamesFile) },
		"terms file":       func(t *testing.T, _ *testRepo) { failWriteOf(t, bisectTermsFile) },
		"mark ref":         func(t *testing.T, _ *testRepo) { failRefUpdates(t) },
		"mark ref commit":  func(t *testing.T, _ *testRepo) { failRefCommits(t) },
		"mark log":         func(t *testing.T, _ *testRepo) { failAppendAfter(t, bisectLogFile, 0) },
		"start log":        func(t *testing.T, _ *testRepo) { failAppendAfter(t, bisectLogFile, 2) },
		"ancestors stat":   func(t *testing.T, _ *testRepo) { failLstatOf(t, bisectAncestorsFile) },
		"ancestors mark":   func(t *testing.T, _ *testRepo) { failWriteOf(t, bisectAncestorsFile) },
		"expected rev":     func(t *testing.T, _ *testRepo) { failWriteOf(t, bisectExpectedRevFile) },
		"bisect head stat": func(t *testing.T, _ *testRepo) { failLstatOf(t, bisectHeadFile) },
		"start file read":  func(t *testing.T, _ *testRepo) { failReadOf(t, bisectStartFile) },
		"checkout": func(t *testing.T, _ *testRepo) {
			swapSeam(t, &bisectSwitch, func(func(context.Context, *repo.Repository, string, SwitchOptions) error) func(context.Context, *repo.Repository, string, SwitchOptions) error {
				return func(context.Context, *repo.Repository, string, SwitchOptions) error { return errInjected }
			})
		},
		"bisect refs": func(t *testing.T, r *testRepo) {
			r.writeFile(".git/BISECT_LOG", "git bisect start\n")
			r.writeFile(".git/BISECT_START", "main\n")
			r.writeFile(".git/refs/bisect/good-x", "not a ref\n")
		},
	}
	for name, fail := range tests {
		t.Run(name, func(t *testing.T) {
			r, ids := bisectChain(t, 6)
			fail(t, r)

			_, err := StartBisect(t.Context(), r.repo, BisectStartOptions{Bad: "main", Good: []string{ids[1].String()}})

			if err == nil {
				t.Fatal("StartBisect returned no error")
			}
		})
	}
}

func TestStartBisectReportsABrokenHead(t *testing.T) {
	r, _ := bisectChain(t, 3)
	r.writeRawHead("not a head\n")

	if _, err := StartBisect(t.Context(), r.repo, BisectStartOptions{}); err == nil {
		t.Fatal("StartBisect accepted a broken HEAD")
	}
}

func TestStartBisectReportsAnUnreadableBisectLog(t *testing.T) {
	r, _ := bisectChain(t, 3)
	failLstatOf(t, bisectLogFile)

	if _, err := StartBisect(t.Context(), r.repo, BisectStartOptions{}); !errors.Is(err, errInjected) {
		t.Fatalf("StartBisect = %v, want errInjected", err)
	}
}

func TestMarkBisectReportsEveryFailure(t *testing.T) {
	tests := map[string]func(t *testing.T, r *testRepo, ids []hash.ObjectID){
		"objects":      func(t *testing.T, _ *testRepo, _ []hash.ObjectID) { failObjectDatabase(t) },
		"terms read":   func(t *testing.T, _ *testRepo, _ []hash.ObjectID) { failReadOf(t, bisectTermsFile) },
		"mark ref":     func(t *testing.T, _ *testRepo, _ []hash.ObjectID) { failRefUpdates(t) },
		"mark log":     func(t *testing.T, _ *testRepo, _ []hash.ObjectID) { failAppendAfter(t, bisectLogFile, 0) },
		"subject":      func(t *testing.T, _ *testRepo, ids []hash.ObjectID) { failCommitOf(t, ids[3]) },
		"expected rev": func(t *testing.T, _ *testRepo, _ []hash.ObjectID) { failWriteOf(t, bisectExpectedRevFile) },
		"candidates":   func(t *testing.T, _ *testRepo, _ []hash.ObjectID) { failEveryObjectRead(t) },
		"broken head":  func(t *testing.T, r *testRepo, _ []hash.ObjectID) { r.writeRawHead("not a head\n") },
	}
	for name, fail := range tests {
		t.Run(name, func(t *testing.T) {
			r, ids := bisectingChain(t, 6, 1)
			fail(t, r, ids)

			if _, err := MarkBisect(t.Context(), r.repo, BisectBad); err == nil {
				t.Fatal("MarkBisect returned no error")
			}
		})
	}
}

func TestMarkBisectReportsAFailedStatusLine(t *testing.T) {
	r, ids := bisectChain(t, 4)
	if _, err := StartBisect(t.Context(), r.repo, BisectStartOptions{}); err != nil {
		t.Fatalf("StartBisect returned error %v", err)
	}
	failAppendAfter(t, bisectLogFile, 1)

	if _, err := MarkBisectRevision(t.Context(), r.repo, BisectGood, ids[0].String()); !errors.Is(err, errInjected) {
		t.Fatalf("MarkBisectRevision = %v, want errInjected", err)
	}
}

func TestMarkBisectReportsAFailedFinalLine(t *testing.T) {
	for name, skip := range map[string]int{"first bad commit": 1, "only skipped": 1} {
		t.Run(name, func(t *testing.T) {
			r, ids := bisectingChain(t, 4, 2)
			if name == "only skipped" {
				if _, err := MarkBisectRevision(t.Context(), r.repo, BisectSkip, ids[3].String()); err != nil {
					t.Fatalf("MarkBisectRevision returned error %v", err)
				}
			}
			failAppendAfter(t, bisectLogFile, skip)

			_, err := MarkBisectRevision(t.Context(), r.repo, BisectGood, ids[2].String())

			if !errors.Is(err, errInjected) {
				t.Fatalf("MarkBisectRevision = %v, want errInjected", err)
			}
		})
	}
}

func TestMarkBisectWithoutACheckoutReportsItsOwnHeadFailures(t *testing.T) {
	r, ids := bisectChain(t, 6)
	if _, err := StartBisect(t.Context(), r.repo, BisectStartOptions{Bad: "main", Good: []string{ids[1].String()}, NoCheckout: true}); err != nil {
		t.Fatalf("StartBisect returned error %v", err)
	}
	failReadOf(t, bisectHeadFile)

	if _, err := MarkBisect(t.Context(), r.repo, BisectBad); !errors.Is(err, errInjected) {
		t.Fatalf("MarkBisect = %v, want errInjected", err)
	}
}

func TestMarkBisectReportsAnUnreadableExpectedRevision(t *testing.T) {
	r, ids := bisectChain(t, 4)
	if _, err := StartBisect(t.Context(), r.repo, BisectStartOptions{}); err != nil {
		t.Fatalf("StartBisect returned error %v", err)
	}
	if _, err := MarkBisectRevision(t.Context(), r.repo, BisectBad, ids[1].String()); err != nil {
		t.Fatalf("MarkBisectRevision returned error %v", err)
	}
	failReadOf(t, bisectExpectedRevFile)

	if _, err := MarkBisectRevision(t.Context(), r.repo, BisectGood, ids[3].String()); !errors.Is(err, errInjected) {
		t.Fatalf("MarkBisectRevision = %v, want errInjected", err)
	}
}

func TestStartBisectReportsAMergeBaseItCannotDescribe(t *testing.T) {
	r, ids, side := bisectForkedRepo(t)
	failCommitOf(t, ids[0])

	if _, err := StartBisect(t.Context(), r.repo, BisectStartOptions{Bad: "main", Good: []string{side.String()}}); !errors.Is(err, errInjected) {
		t.Fatalf("StartBisect = %v, want errInjected", err)
	}
}

func TestReadBisectStatusReportsEveryFailure(t *testing.T) {
	tests := map[string]func(t *testing.T, r *testRepo, ids []hash.ObjectID){
		"objects":        func(t *testing.T, _ *testRepo, _ []hash.ObjectID) { failObjectDatabase(t) },
		"ancestors stat": func(t *testing.T, _ *testRepo, _ []hash.ObjectID) { failLstatOf(t, bisectAncestorsFile) },
		"candidates":     func(t *testing.T, _ *testRepo, _ []hash.ObjectID) { failEveryObjectRead(t) },
	}
	for name, fail := range tests {
		t.Run(name, func(t *testing.T) {
			r, ids := bisectingChain(t, 6, 1)
			fail(t, r, ids)

			if _, err := ReadBisectStatus(t.Context(), r.repo); err == nil {
				t.Fatal("ReadBisectStatus returned no error")
			}
		})
	}
}

func TestReadBisectStatusReportsAMergeBaseItCannotDescribe(t *testing.T) {
	r, ids, side := bisectForkedRepo(t)
	if _, err := StartBisect(t.Context(), r.repo, BisectStartOptions{Bad: "main", Good: []string{side.String()}}); err != nil {
		t.Fatalf("StartBisect returned error %v", err)
	}
	for name, fail := range map[string]func(t *testing.T){
		"expected rev": func(t *testing.T) { failReadOf(t, bisectExpectedRevFile) },
		"subject":      func(t *testing.T) { failCommitOf(t, ids[0]) },
	} {
		t.Run(name, func(t *testing.T) {
			fail(t)

			if _, err := ReadBisectStatus(t.Context(), r.repo); !errors.Is(err, errInjected) {
				t.Fatalf("ReadBisectStatus = %v, want errInjected", err)
			}
		})
	}
}

func TestBisectLogReportsAnUnreadableFile(t *testing.T) {
	r, _ := bisectingChain(t, 4, 1)
	failReadOf(t, bisectLogFile)

	if _, err := BisectLog(r.repo); !errors.Is(err, errInjected) {
		t.Fatalf("BisectLog = %v, want errInjected", err)
	}
}
