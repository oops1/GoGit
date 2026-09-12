package ops

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/rerere"
)

const handResolution = "RESOLVED BY HAND\n"

func (r *testRepo) enableRerere(autoupdate bool) {
	r.t.Helper()
	text := "[rerere]\n\tenabled = true\n"
	if autoupdate {
		text += "\tautoupdate = true\n"
	}
	r.appendConfig(text)
	r.repo = r.reopen()
}

func (r *testRepo) rerereCache() *rerere.Cache {
	r.t.Helper()
	cache, err := rerere.OpenCache(r.repo.GitDir())
	if err != nil {
		r.t.Fatalf("OpenCache returned error %v", err)
	}
	return cache
}

func (r *testRepo) mergeRR() []rerere.Entry {
	r.t.Helper()
	data, err := os.ReadFile(filepath.Join(r.repo.GitDir(), rerere.MergeRRFile))
	if err != nil {
		return nil
	}
	entries, err := rerere.ParseMergeRR(data)
	if err != nil {
		r.t.Fatalf("ParseMergeRR returned error %v", err)
	}
	return entries
}

func (r *testRepo) resolveByHand() {
	r.t.Helper()
	r.writeFile("f", changeLine(tenLines("f"), 4, strings.TrimSuffix(handResolution, "\n")))
}

func (r *testRepo) recordAResolution() {
	r.t.Helper()
	if _, err := r.merge("feature", MergeOptions{When: mergeTime}); err != nil {
		r.t.Fatalf("merge returned error %v", err)
	}
	r.resolveByHand()
	if err := Stage(r.t.Context(), r.repo, []string{"f"}, StageOptions{}); err != nil {
		r.t.Fatal(err)
	}
	if _, err := Commit(r.t.Context(), r.repo, CommitOptions{Message: "merged", When: mergeTime}); err != nil {
		r.t.Fatalf("Commit returned error %v", err)
	}
}

func TestAConflictIsRememberedOnlyWhenRerereIsOn(t *testing.T) {
	tr := newTestRepo(t)
	tr.conflictingFork()

	if _, err := tr.merge("feature", MergeOptions{When: mergeTime}); err != nil {
		t.Fatalf("merge returned error %v", err)
	}

	if tr.exists(".git/"+rerere.CacheDir) || tr.mergeRR() != nil {
		t.Fatal("a conflict was remembered without rerere.enabled")
	}
}

func TestAnExistingCacheTurnsRerereOn(t *testing.T) {
	tr := newTestRepo(t)
	tr.conflictingFork()
	if _, err := rerere.OpenCache(tr.repo.GitDir()); err != nil {
		t.Fatal(err)
	}

	if _, err := tr.merge("feature", MergeOptions{When: mergeTime}); err != nil {
		t.Fatalf("merge returned error %v", err)
	}

	if entries := tr.mergeRR(); len(entries) != 1 || entries[0].Path != "f" {
		t.Fatalf("MERGE_RR = %+v", entries)
	}
}

func TestRerereRemembersTheConflictAndTheResolution(t *testing.T) {
	tr := newTestRepo(t)
	tr.conflictingFork()
	tr.enableRerere(false)

	if _, err := tr.merge("feature", MergeOptions{When: mergeTime}); err != nil {
		t.Fatalf("merge returned error %v", err)
	}

	entries := tr.mergeRR()
	if len(entries) != 1 || entries[0].Path != "f" || entries[0].Variant != 0 {
		t.Fatalf("MERGE_RR = %+v", entries)
	}
	cache := tr.rerereCache()
	preimage, err := cache.Preimage(entries[0].ID, 0)
	if err != nil || !strings.Contains(string(preimage), "<<<<<<<\n") {
		t.Fatalf("preimage = %q, %v", preimage, err)
	}

	tr.resolveByHand()
	if err := Stage(t.Context(), tr.repo, []string{"f"}, StageOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := Commit(t.Context(), tr.repo, CommitOptions{Message: "merged", When: mergeTime}); err != nil {
		t.Fatalf("Commit returned error %v", err)
	}

	postimage, err := cache.Postimage(entries[0].ID, 0)
	if err != nil || !strings.Contains(string(postimage), handResolution) {
		t.Fatalf("postimage = %q, %v", postimage, err)
	}
	if entries := tr.mergeRR(); entries != nil {
		t.Fatalf("MERGE_RR = %+v", entries)
	}
}

func TestTheRememberedResolutionComesBackOnTheSameConflict(t *testing.T) {
	tr := newTestRepo(t)
	tr.conflictingFork()
	tr.enableRerere(false)
	tr.recordAResolution()
	if _, err := Reset(t.Context(), tr.repo, "HEAD~1", resetOptions(ResetHard)); err != nil {
		t.Fatalf("Reset returned error %v", err)
	}

	result, err := tr.merge("feature", MergeOptions{When: mergeTime})

	if err != nil || result.Clean() {
		t.Fatalf("merge = %+v, %v", result, err)
	}
	if got := tr.readFile("f"); !strings.Contains(got, handResolution) || strings.Contains(got, "<<<<<<<") {
		t.Fatalf("f = %q", got)
	}
	if entries := tr.mergeRR(); entries != nil {
		t.Fatalf("MERGE_RR = %+v", entries)
	}
	if stages := tr.stageEntries("f"); len(stages) < 2 {
		t.Fatalf("without autoupdate the index should stay conflicted: %v", stages)
	}
}

func TestAutoUpdateStagesTheResolutionItBroughtBack(t *testing.T) {
	tr := newTestRepo(t)
	tr.conflictingFork()
	tr.enableRerere(true)
	tr.recordAResolution()
	if _, err := Reset(t.Context(), tr.repo, "HEAD~1", resetOptions(ResetHard)); err != nil {
		t.Fatalf("Reset returned error %v", err)
	}

	if _, err := tr.merge("feature", MergeOptions{When: mergeTime}); err != nil {
		t.Fatalf("merge returned error %v", err)
	}

	if stages := tr.stageEntries("f"); len(stages) != 1 {
		t.Fatalf("stages = %v", stages)
	}
	if got := tr.readFile("f"); !strings.Contains(got, handResolution) {
		t.Fatalf("f = %q", got)
	}
}

func TestRerereIgnoresAnUnreadableConfiguredValue(t *testing.T) {
	tr := newTestRepo(t)
	tr.conflictingFork()
	tr.appendConfig("[rerere]\n\tenabled = maybe\n\tautoupdate = maybe\n")
	tr.repo = tr.reopen()

	if rerereActive(tr.repo) || rerereAutoUpdate(tr.repo) {
		t.Fatal("a broken value turned rerere on")
	}
}

func TestTheMarkerSizeComesFromTheAttributes(t *testing.T) {
	tr := newTestRepo(t)
	tr.writeFile(".gitattributes", "wide conflict-marker-size=11\nbroken conflict-marker-size=nonsense\n")
	wt, err := openWorkingTree(tr.repo)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = wt.close() })

	if got := wt.markerSizeOf("wide"); got != 11 {
		t.Fatalf("wide = %d", got)
	}
	if got := wt.markerSizeOf("broken"); got != 7 {
		t.Fatalf("broken = %d", got)
	}
	if got := wt.markerSizeOf("plain"); got != 7 {
		t.Fatalf("plain = %d", got)
	}
}

func TestACorruptMergeRRIsReported(t *testing.T) {
	tr := newTestRepo(t)
	tr.conflictingFork()
	tr.enableRerere(false)
	if err := os.WriteFile(filepath.Join(tr.repo.GitDir(), rerere.MergeRRFile), []byte("nonsense\x00"), 0o666); err != nil {
		t.Fatal(err)
	}

	if _, err := tr.merge("feature", MergeOptions{When: mergeTime}); !errors.Is(err, rerere.ErrCorruptMergeRR) {
		t.Fatalf("err = %v", err)
	}
}

func TestAPathThatVanishedIsForgotten(t *testing.T) {
	tr := newTestRepo(t)
	tr.conflictingFork()
	tr.enableRerere(false)
	if _, err := tr.merge("feature", MergeOptions{When: mergeTime}); err != nil {
		t.Fatal(err)
	}
	tr.remove("f")

	if _, err := Commit(t.Context(), tr.repo, CommitOptions{Message: "merged", When: mergeTime}); err == nil {
		t.Fatal("a commit over unmerged paths was allowed")
	}
	if err := Stage(t.Context(), tr.repo, []string{"f"}, StageOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := Commit(t.Context(), tr.repo, CommitOptions{Message: "merged", When: mergeTime}); err != nil {
		t.Fatalf("Commit returned error %v", err)
	}

	if entries := tr.mergeRR(); entries != nil {
		t.Fatalf("MERGE_RR = %+v", entries)
	}
}

func TestRerereStaysOutOfABareRepository(t *testing.T) {
	tr := newBareTestRepo(t)
	tr.appendConfig("[rerere]\n\tenabled = true\n")
	tr.repo = tr.reopen()

	if err := recordRerereResolutions(t.Context(), tr.repo, tr.db()); err != nil {
		t.Fatalf("recordRerereResolutions returned error %v", err)
	}
}

func TestRerereReportsACacheItCannotOpen(t *testing.T) {
	tr := newTestRepo(t)
	tr.conflictingFork()
	tr.enableRerere(false)
	if err := os.WriteFile(filepath.Join(tr.repo.GitDir(), rerere.CacheDir), nil, 0o666); err != nil {
		t.Fatal(err)
	}

	if _, err := tr.merge("feature", MergeOptions{When: mergeTime}); err == nil {
		t.Fatal("the merge ignored a cache it could not open")
	}
}

func TestRerereReportsAPreimageItCannotWrite(t *testing.T) {
	tr := newTestRepo(t)
	tr.conflictingFork()
	tr.enableRerere(false)
	if _, err := tr.merge("feature", MergeOptions{When: mergeTime}); err != nil {
		t.Fatal(err)
	}
	id := tr.mergeRR()[0].ID
	if _, err := Reset(t.Context(), tr.repo, "HEAD", resetOptions(ResetHard)); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(tr.repo.GitDir(), rerere.CacheDir, id)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tr.repo.GitDir(), rerere.CacheDir, id), nil, 0o666); err != nil {
		t.Fatal(err)
	}

	if _, err := tr.merge("feature", MergeOptions{When: mergeTime}); err == nil {
		t.Fatal("the merge ignored a preimage it could not write")
	}
}

func TestRerereReportsAPostimageItCannotWrite(t *testing.T) {
	tr := newTestRepo(t)
	tr.conflictingFork()
	tr.enableRerere(false)
	if _, err := tr.merge("feature", MergeOptions{When: mergeTime}); err != nil {
		t.Fatal(err)
	}
	id := tr.mergeRR()[0].ID
	tr.resolveByHand()
	if err := Stage(t.Context(), tr.repo, []string{"f"}, StageOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(tr.repo.GitDir(), rerere.CacheDir, id)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tr.repo.GitDir(), rerere.CacheDir, id), nil, 0o666); err != nil {
		t.Fatal(err)
	}

	if _, err := Commit(t.Context(), tr.repo, CommitOptions{Message: "merged", When: mergeTime}); err == nil {
		t.Fatal("the commit ignored a postimage it could not write")
	}
}

func TestRerereKeepsAPathItCouldNotReplay(t *testing.T) {
	tr := newTestRepo(t)
	tr.conflictingFork()
	tr.enableRerere(false)
	tr.recordAResolution()
	id := ""
	cache := tr.rerereCache()
	entries, err := os.ReadDir(cache.Dir())
	if err != nil || len(entries) != 1 {
		t.Fatalf("cache = %+v, %v", entries, err)
	}
	id = entries[0].Name()
	if err := cache.WritePreimage(id, 0, []byte("a preimage that cannot be merged\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := Reset(t.Context(), tr.repo, "HEAD~1", resetOptions(ResetHard)); err != nil {
		t.Fatal(err)
	}

	if _, err := tr.merge("feature", MergeOptions{When: mergeTime}); err != nil {
		t.Fatalf("merge returned error %v", err)
	}

	if remembered := tr.mergeRR(); len(remembered) != 1 || remembered[0].Variant != 1 {
		t.Fatalf("MERGE_RR = %+v", remembered)
	}
	if got := tr.readFile("f"); !strings.Contains(got, "<<<<<<<") {
		t.Fatalf("f = %q", got)
	}
}

func TestAConflictWithoutMarkersIsNotRemembered(t *testing.T) {
	tr := newTestRepo(t)
	f := tenLines("f")
	tr.fork(map[string]string{"f": changeLine(f, 4, "OURS")}, map[string]string{"f": ""})
	tr.enableRerere(false)

	result, err := tr.merge("feature", MergeOptions{When: mergeTime})

	if err != nil || result.Clean() {
		t.Fatalf("merge = %+v, %v", result, err)
	}
	if entries := tr.mergeRR(); entries != nil {
		t.Fatalf("MERGE_RR = %+v", entries)
	}
}

func TestTwoConflictedPathsAreRememberedInOrder(t *testing.T) {
	tr := newTestRepo(t)
	f, g := tenLines("f"), tenLines("g")
	tr.fork(
		map[string]string{"f": changeLine(f, 4, "OURS"), "g": changeLine(g, 4, "OURS")},
		map[string]string{"f": changeLine(f, 4, "THEIRS"), "g": changeLine(g, 4, "THEIRS")},
	)
	tr.enableRerere(false)

	if _, err := tr.merge("feature", MergeOptions{When: mergeTime}); err != nil {
		t.Fatalf("merge returned error %v", err)
	}

	entries := tr.mergeRR()
	if len(entries) != 2 || entries[0].Path != "f" || entries[1].Path != "g" {
		t.Fatalf("MERGE_RR = %+v", entries)
	}
}

func TestAStagedFileThatStillHasMarkersStaysRemembered(t *testing.T) {
	tr := newTestRepo(t)
	tr.conflictingFork()
	tr.enableRerere(false)
	if _, err := tr.merge("feature", MergeOptions{When: mergeTime}); err != nil {
		t.Fatal(err)
	}
	if err := Stage(t.Context(), tr.repo, []string{"f"}, StageOptions{}); err != nil {
		t.Fatal(err)
	}

	if _, err := Commit(t.Context(), tr.repo, CommitOptions{Message: "merged", When: mergeTime}); err != nil {
		t.Fatalf("Commit returned error %v", err)
	}

	if entries := tr.mergeRR(); len(entries) != 1 || entries[0].Path != "f" {
		t.Fatalf("MERGE_RR = %+v", entries)
	}
}

func TestABrokenCacheEntryStopsTheReplay(t *testing.T) {
	for _, image := range []string{"preimage", "postimage", "thisimage"} {
		t.Run(image, func(t *testing.T) {
			tr := newTestRepo(t)
			tr.conflictingFork()
			tr.enableRerere(false)
			tr.recordAResolution()
			cache := tr.rerereCache()
			entries, err := os.ReadDir(cache.Dir())
			if err != nil || len(entries) != 1 {
				t.Fatalf("cache = %+v, %v", entries, err)
			}
			blocked := filepath.Join(cache.Dir(), entries[0].Name(), image)
			if err := os.RemoveAll(blocked); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Join(blocked, "inside"), 0o777); err != nil {
				t.Fatal(err)
			}
			if _, err := Reset(t.Context(), tr.repo, "HEAD~1", resetOptions(ResetHard)); err != nil {
				t.Fatal(err)
			}

			if _, err := tr.merge("feature", MergeOptions{When: mergeTime}); err == nil {
				t.Fatalf("the merge ignored a %s it could not use", image)
			}
		})
	}
}

func TestACacheThatVanishedStopsTheRecording(t *testing.T) {
	tr := newTestRepo(t)
	tr.conflictingFork()
	tr.enableRerere(false)
	if _, err := tr.merge("feature", MergeOptions{When: mergeTime}); err != nil {
		t.Fatal(err)
	}
	tr.resolveByHand()
	if err := Stage(t.Context(), tr.repo, []string{"f"}, StageOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(tr.repo.GitDir(), rerere.CacheDir)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tr.repo.GitDir(), rerere.CacheDir), nil, 0o666); err != nil {
		t.Fatal(err)
	}

	if _, err := Commit(t.Context(), tr.repo, CommitOptions{Message: "merged", When: mergeTime}); err == nil {
		t.Fatal("the commit ignored a cache it could not open")
	}
}
