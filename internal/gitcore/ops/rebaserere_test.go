package ops

import (
	"errors"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/rerere"
)

func (r *testRepo) stopARebaseWithRerere() rerere.Entry {
	r.t.Helper()
	r.enableRerere(false)
	r.rebaseFork(true)
	if _, err := Rebase(r.t.Context(), r.repo, "main", rebaseOptions()); err != nil {
		r.t.Fatal(err)
	}
	entries := r.mergeRR()
	if len(entries) != 1 {
		r.t.Fatalf("MERGE_RR = %+v", entries)
	}
	return entries[0]
}

func (r *testRepo) variantsOf(id string) []rerere.Variant {
	r.t.Helper()
	return r.rerereCache().Variants(id)
}

func TestContinuingARebaseRecordsTheResolution(t *testing.T) {
	tr := newTestRepo(t)
	entry := tr.stopARebaseWithRerere()
	tr.resolveByHand()
	if err := Stage(t.Context(), tr.repo, []string{"f"}, StageOptions{}); err != nil {
		t.Fatal(err)
	}

	if _, err := ContinueRebase(t.Context(), tr.repo, rebaseOptions()); err != nil {
		t.Fatalf("ContinueRebase returned error %v", err)
	}

	if variants := tr.variantsOf(entry.ID); len(variants) != 1 || !variants[0].Complete() {
		t.Fatalf("variants = %+v", variants)
	}
	if entries := tr.mergeRR(); len(entries) != 0 || !tr.exists(".git/"+rerere.MergeRRFile) {
		t.Fatalf("MERGE_RR = %+v", entries)
	}
}

func TestContinuingAFoldRecordsTheResolution(t *testing.T) {
	tr := newTestRepo(t)
	tr.enableRerere(false)
	f := tenLines("f")
	base := tr.commitFiles("base", map[string]string{"f": f})
	tr.createBranch("topic", base)
	tr.switchTo("topic")
	first := tr.commitFiles("topic a", map[string]string{"a": "a\n"})
	second := tr.commitFiles("topic f", map[string]string{"f": changeLine(f, 4, "TOPIC")})
	tr.switchTo("main")
	tr.commitFiles("main f", map[string]string{"f": changeLine(f, 4, "MAIN")})
	tr.switchTo("topic")
	if _, err := Rebase(t.Context(), tr.repo, "main", RebaseOptions{When: mergeTime, Todo: steps(actionPick, first, actionSquash, second)}); err != nil {
		t.Fatal(err)
	}
	entries := tr.mergeRR()
	tr.resolveByHand()
	if err := Stage(t.Context(), tr.repo, []string{"f"}, StageOptions{}); err != nil {
		t.Fatal(err)
	}

	if _, err := ContinueRebase(t.Context(), tr.repo, rebaseOptions()); err != nil {
		t.Fatalf("ContinueRebase returned error %v", err)
	}

	if len(entries) != 1 || !tr.variantsOf(entries[0].ID)[0].Complete() {
		t.Fatalf("entries = %+v", entries)
	}
}

func TestSkippingOrAbortingARebaseForgetsTheUnresolvedConflict(t *testing.T) {
	for name, undo := range map[string]func(tr *testRepo) error{
		"skip": func(tr *testRepo) error {
			_, err := SkipRebase(tr.t.Context(), tr.repo, rebaseOptions())
			return err
		},
		"abort": func(tr *testRepo) error { return AbortOperation(tr.t.Context(), tr.repo) },
	} {
		t.Run(name, func(t *testing.T) {
			tr := newTestRepo(t)
			entry := tr.stopARebaseWithRerere()

			if err := undo(tr); err != nil {
				t.Fatalf("undo returned error %v", err)
			}

			if variants := tr.variantsOf(entry.ID); len(variants) != 0 || tr.exists(".git/"+rerere.MergeRRFile) || tr.exists(".git/"+rerere.CacheDir+"/"+entry.ID) {
				t.Fatalf("variants = %+v", variants)
			}
			if tr.exists(".git/"+autoMergeFile) && name == "abort" {
				t.Fatal("AUTO_MERGE survived the abort")
			}
		})
	}
}

func TestClearingRerereKeepsRecordedResolutionsAndIgnoresADisabledCache(t *testing.T) {
	tr := newTestRepo(t)
	entry := tr.stopARebaseWithRerere()
	if err := tr.rerereCache().WritePostimage(entry.ID, entry.Variant, []byte("resolved\n")); err != nil {
		t.Fatal(err)
	}
	run := &rerereRun{r: tr.repo}

	if err := run.clear(); err != nil {
		t.Fatalf("clear returned error %v", err)
	}

	if variants := tr.variantsOf(entry.ID); len(variants) != 1 || !variants[0].Complete() {
		t.Fatalf("variants = %+v", variants)
	}
	tr.appendConfig("[rerere]\n\tenabled = false\n")
	tr.repo = tr.reopen()
	tr.writeFile(".git/"+rerere.MergeRRFile, "garbage")
	if err := (&rerereRun{r: tr.repo}).clear(); err != nil || !tr.exists(".git/"+rerere.MergeRRFile) {
		t.Fatalf("clear with rerere off = %v", err)
	}
	if err := (&rerereRun{r: tr.repo}).record(); err != nil {
		t.Fatalf("record with rerere off = %v", err)
	}
}

func TestClearingRerereReportsWhatItCannotRead(t *testing.T) {
	t.Run("MERGE_RR", func(t *testing.T) {
		tr := newTestRepo(t)
		tr.enableRerere(false)
		tr.writeFile(".git/"+rerere.MergeRRFile, "garbage")
		if err := (&rerereRun{r: tr.repo}).clear(); !errors.Is(err, rerere.ErrCorruptMergeRR) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("rr-cache", func(t *testing.T) {
		tr := newTestRepo(t)
		tr.enableRerere(false)
		tr.writeFile(".git/"+rerere.CacheDir, "not a directory")
		if err := (&rerereRun{r: tr.repo}).clear(); err == nil {
			t.Fatal("clear accepted a file in place of the rr-cache")
		}
	})
}
