package ops

import (
	"errors"
	"maps"
	"os"
	"path"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/index"
	"github.com/oops1/gogit/internal/gitcore/merge"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/refs"
)

var mergeTime = time.Unix(1700009000, 0).UTC()

func tenLines(seed string) string {
	var out []string
	for i := range 10 {
		out = append(out, seed+" line "+strconv.Itoa(i)+" long enough to be recognised")
	}
	return strings.Join(out, "\n") + "\n"
}

func changeLine(text string, line int, replacement string) string {
	parts := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	parts[line] = replacement
	return strings.Join(parts, "\n") + "\n"
}

func (r *testRepo) commitFiles(message string, files map[string]string) hash.ObjectID {
	r.t.Helper()
	for _, rel := range slices.Sorted(maps.Keys(files)) {
		if files[rel] == "" {
			r.remove(rel)
			for dir := path.Dir(rel); dir != "."; dir = path.Dir(dir) {
				_ = os.Remove(r.path(dir))
			}
		}
	}
	for _, rel := range slices.Sorted(maps.Keys(files)) {
		if files[rel] != "" {
			r.writeFile(rel, files[rel])
		}
	}
	if err := Stage(r.t.Context(), r.repo, slices.Sorted(maps.Keys(files)), StageOptions{}); err != nil {
		r.t.Fatalf("Stage returned error %v", err)
	}
	return r.commitAll(message)
}

func (r *testRepo) switchTo(target string) {
	r.t.Helper()
	if err := Switch(r.t.Context(), r.repo, target, SwitchOptions{}); err != nil {
		r.t.Fatalf("Switch returned error %v", err)
	}
}

func (r *testRepo) fork(ours, theirs map[string]string) {
	r.t.Helper()
	base := r.commitFiles("base", map[string]string{"keep": "keep\n", "f": tenLines("f"), "g": tenLines("g")})
	r.createBranch("feature", base)
	r.commitFiles("ours", ours)
	r.switchTo("feature")
	r.commitFiles("theirs", theirs)
	r.switchTo("main")
}

func (r *testRepo) merge(target string, opts MergeOptions) (MergeResult, error) {
	r.t.Helper()
	if opts.When.IsZero() {
		opts.When = mergeTime
	}
	return Merge(r.t.Context(), r.repo, target, opts)
}

func (r *testRepo) stageEntries(path string) []index.Stage {
	r.t.Helper()
	var stages []index.Stage
	for entry := range r.index().Entries() {
		if entry.Path == path {
			stages = append(stages, entry.Stage)
		}
	}
	return stages
}

func (r *testRepo) mergeState() MergeState {
	r.t.Helper()
	state, err := ReadMergeState(r.repo)
	if err != nil {
		r.t.Fatalf("ReadMergeState returned error %v", err)
	}
	return state
}

func (r *testRepo) conflictingFork() {
	r.t.Helper()
	r.fork(map[string]string{"f": changeLine(tenLines("f"), 4, "OURS")}, map[string]string{"f": changeLine(tenLines("f"), 4, "THEIRS")})
}

func TestMergeFastForwardsWhenTheBranchIsAhead(t *testing.T) {
	tr := newTestRepo(t)
	base := tr.commitFiles("base", map[string]string{"f": "one\n"})
	tr.createBranch("feature", base)
	tr.switchTo("feature")
	next := tr.commitFiles("next", map[string]string{"f": "two\n", "dir/new": "new\n"})
	tr.switchTo("main")

	result, err := tr.merge("feature", MergeOptions{})

	if err != nil || !result.FastForward || result.New != next || tr.branchTarget("main") != next {
		t.Fatalf("result = %+v, %v", result, err)
	}
	if tr.readFile("f") != "two\n" || tr.readFile("dir/new") != "new\n" {
		t.Fatalf("working tree was not moved to the new commit")
	}
}

func TestMergeOfAnAncestorIsUpToDate(t *testing.T) {
	tr := newTestRepo(t)
	base := tr.commitFiles("base", map[string]string{"f": "one\n"})
	tr.createBranch("feature", base)
	ahead := tr.commitFiles("ahead", map[string]string{"f": "two\n"})

	result, err := tr.merge("feature", MergeOptions{})

	if err != nil || !result.UpToDate || result.New != ahead || tr.branchTarget("main") != ahead {
		t.Fatalf("result = %+v, %v", result, err)
	}
}

func TestMergeOfDivergedBranchesCommitsBothParents(t *testing.T) {
	tr := newTestRepo(t)
	tr.fork(map[string]string{"f": changeLine(tenLines("f"), 0, "OURS")}, map[string]string{"g": changeLine(tenLines("g"), 0, "THEIRS")})
	ours, theirs := tr.branchTarget("main"), tr.branchTarget("feature")

	result, err := tr.merge("feature", MergeOptions{})

	if err != nil || !result.Committed || !result.Clean() {
		t.Fatalf("result = %+v, %v", result, err)
	}
	commit, err := tr.db().Commit(result.New)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(commit.Parents, []hash.ObjectID{ours, theirs}) || commit.Message != "Merge branch 'feature'\n" || !commit.Committer.When.Equal(mergeTime) {
		t.Fatalf("commit = %+v", commit)
	}
	if !strings.HasPrefix(tr.readFile("g"), "THEIRS\n") || tr.mergeState().InProgress() {
		t.Fatalf("working tree or state is wrong after a clean merge")
	}
}

func TestMergeUsesTheGivenMessage(t *testing.T) {
	tr := newTestRepo(t)
	tr.fork(map[string]string{"f": changeLine(tenLines("f"), 0, "OURS")}, map[string]string{"g": changeLine(tenLines("g"), 0, "THEIRS")})

	result, err := tr.merge("feature", MergeOptions{Message: "Bring the feature in\n\n# dropped\n"})

	commit, _ := tr.db().Commit(result.New)
	if err != nil || commit.Message != "Bring the feature in\n" {
		t.Fatalf("commit = %+v, %v", commit, err)
	}
}

func TestAConflictLeavesStagesMarkersAndMergeState(t *testing.T) {
	tr := newTestRepo(t)
	tr.conflictingFork()
	head, theirs := tr.branchTarget("main"), tr.branchTarget("feature")

	result, err := tr.merge("feature", MergeOptions{})

	if err != nil || result.Clean() || !slices.Equal(result.Conflicts, []string{"f"}) || tr.branchTarget("main") != head {
		t.Fatalf("result = %+v, %v", result, err)
	}
	if got := tr.stageEntries("f"); !slices.Equal(got, []index.Stage{index.StageAncestor, index.StageOurs, index.StageTheirs}) {
		t.Fatalf("stages = %v", got)
	}
	if text := tr.readFile("f"); !strings.Contains(text, "<<<<<<< HEAD\nOURS\n=======\nTHEIRS\n>>>>>>> feature\n") {
		t.Fatalf("file:\n%s", text)
	}
	state := tr.mergeState()
	if !slices.Equal(state.Heads, []hash.ObjectID{theirs}) || state.Message != "Merge branch 'feature'\n\n# Conflicts:\n#\tf\n" || state.NoFastForward {
		t.Fatalf("state = %+v", state)
	}
}

func TestCommitConcludesAMergeWithBothParents(t *testing.T) {
	tr := newTestRepo(t)
	tr.conflictingFork()
	head, theirs := tr.branchTarget("main"), tr.branchTarget("feature")
	if _, err := tr.merge("feature", MergeOptions{}); err != nil {
		t.Fatal(err)
	}
	tr.writeFile("f", "resolved\n")
	if err := Stage(t.Context(), tr.repo, []string{"f"}, StageOptions{}); err != nil {
		t.Fatal(err)
	}

	id, err := Commit(t.Context(), tr.repo, CommitOptions{Message: tr.mergeState().Message, When: mergeTime})

	if err != nil {
		t.Fatal(err)
	}
	commit, _ := tr.db().Commit(id)
	if !slices.Equal(commit.Parents, []hash.ObjectID{head, theirs}) || commit.Message != "Merge branch 'feature'\n" {
		t.Fatalf("commit = %+v", commit)
	}
	if state := tr.mergeState(); state.InProgress() || state.Message != "" {
		t.Fatalf("state = %+v", state)
	}
}

func TestCommitRefusesUnmergedPaths(t *testing.T) {
	tr := newTestRepo(t)
	tr.conflictingFork()
	if _, err := tr.merge("feature", MergeOptions{}); err != nil {
		t.Fatal(err)
	}

	if _, err := Commit(t.Context(), tr.repo, CommitOptions{Message: "x"}); !errors.Is(err, ErrUnmergedPaths) {
		t.Fatalf("err = %v, want ErrUnmergedPaths", err)
	}
}

func TestCommitRefusesToAmendDuringAMerge(t *testing.T) {
	tr := newTestRepo(t)
	tr.conflictingFork()
	if _, err := tr.merge("feature", MergeOptions{}); err != nil {
		t.Fatal(err)
	}

	if _, err := Commit(t.Context(), tr.repo, CommitOptions{Message: "x", Amend: true}); !errors.Is(err, ErrMergeInProgress) {
		t.Fatalf("err = %v, want ErrMergeInProgress", err)
	}
}

func TestAMergeCannotStartWhileAnotherIsInProgress(t *testing.T) {
	tr := newTestRepo(t)
	tr.conflictingFork()
	if _, err := tr.merge("feature", MergeOptions{}); err != nil {
		t.Fatal(err)
	}

	if _, err := tr.merge("feature", MergeOptions{}); !errors.Is(err, ErrMergeInProgress) {
		t.Fatalf("err = %v, want ErrMergeInProgress", err)
	}
}

func TestAbortMergeRestoresHeadAndKeepsUnrelatedChanges(t *testing.T) {
	tr := newTestRepo(t)
	tr.fork(map[string]string{"f": changeLine(tenLines("f"), 4, "OURS"), "gone": "gone\n"}, map[string]string{"f": changeLine(tenLines("f"), 4, "THEIRS"), "new": "new\n"})
	tr.writeFile("keep", "local\n")
	if _, err := tr.merge("feature", MergeOptions{}); err != nil {
		t.Fatal(err)
	}

	if err := AbortMerge(t.Context(), tr.repo); err != nil {
		t.Fatal(err)
	}

	if tr.readFile("f") != changeLine(tenLines("f"), 4, "OURS") || tr.exists("new") || tr.readFile("keep") != "local\n" {
		t.Fatalf("working tree was not restored")
	}
	if tr.index().HasConflicts() || tr.mergeState().InProgress() {
		t.Fatalf("merge state survived the abort")
	}
	if _, staged := entryOf(t, tr.index(), "new"); staged {
		t.Fatalf("their new file is still staged")
	}
}

func TestAbortWithoutAMergeFails(t *testing.T) {
	tr := newTestRepo(t)
	tr.commitFiles("base", map[string]string{"f": "f\n"})

	if err := AbortMerge(t.Context(), tr.repo); !errors.Is(err, ErrNoMergeInProgress) {
		t.Fatalf("err = %v, want ErrNoMergeInProgress", err)
	}
}

func TestFastForwardOnlyRefusesDivergedBranches(t *testing.T) {
	tr := newTestRepo(t)
	tr.fork(map[string]string{"f": "ours\n"}, map[string]string{"g": "theirs\n"})

	if _, err := tr.merge("feature", MergeOptions{Mode: MergeFastForwardOnly}); !errors.Is(err, ErrCannotFastForward) {
		t.Fatalf("err = %v, want ErrCannotFastForward", err)
	}
}

func TestNoFastForwardCommitsEvenWhenTheBranchIsAhead(t *testing.T) {
	tr := newTestRepo(t)
	base := tr.commitFiles("base", map[string]string{"f": "one\n"})
	tr.createBranch("feature", base)
	tr.switchTo("feature")
	next := tr.commitFiles("next", map[string]string{"g": "g\n"})
	tr.switchTo("main")

	result, err := tr.merge("feature", MergeOptions{Mode: MergeNoFastForward})

	commit, _ := tr.db().Commit(result.New)
	if err != nil || !result.Committed || !slices.Equal(commit.Parents, []hash.ObjectID{base, next}) {
		t.Fatalf("result = %+v, %v", result, err)
	}
}

func TestMergeRefusesUnrelatedHistories(t *testing.T) {
	tr := newTestRepo(t)
	tr.commitFiles("main", map[string]string{"f": "f\n"})
	tr.writeRawHead("ref: refs/heads/other\n")
	idx := index.New(index.Version2)
	tr.saveIndex(idx)
	tr.commitFiles("other", map[string]string{"g": "g\n"})
	tr.writeRawHead("ref: refs/heads/main\n")

	if _, err := tr.merge("other", MergeOptions{}); !errors.Is(err, ErrUnrelatedHistories) {
		t.Fatalf("err = %v, want ErrUnrelatedHistories", err)
	}
}

func TestMergeOfSomethingThatIsNotACommitFails(t *testing.T) {
	tr := newTestRepo(t)
	tr.commitFiles("base", map[string]string{"f": "f\n"})
	for _, target := range []string{"missing", "HEAD^{tree}"} {
		if _, err := tr.merge(target, MergeOptions{}); !errors.Is(err, ErrTargetNotFound) {
			t.Fatalf("%s: err = %v, want ErrTargetNotFound", target, err)
		}
	}
}

func TestMergeRefusesToOverwriteLocalWork(t *testing.T) {
	for name, prepare := range map[string]func(tr *testRepo){
		"changed file": func(tr *testRepo) { tr.writeFile("g", "local\n") },
		"staged elsewhere": func(tr *testRepo) {
			tr.writeFile("keep", "staged\n")
			_ = Stage(t.Context(), tr.repo, []string{"keep"}, StageOptions{})
		},
		"untracked in path": func(tr *testRepo) { tr.writeFile("new", "untracked\n") },
	} {
		t.Run(name, func(t *testing.T) {
			tr := newTestRepo(t)
			tr.fork(map[string]string{"f": changeLine(tenLines("f"), 0, "OURS")}, map[string]string{"g": changeLine(tenLines("g"), 0, "THEIRS"), "new": "new\n"})
			head := tr.branchTarget("main")
			prepare(tr)

			_, err := tr.merge("feature", MergeOptions{})

			var overwrite *OverwriteError
			if !errors.As(err, &overwrite) || tr.branchTarget("main") != head || tr.mergeState().InProgress() {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestMergeKeepsAnUnrelatedLocalChange(t *testing.T) {
	tr := newTestRepo(t)
	tr.fork(map[string]string{"f": changeLine(tenLines("f"), 0, "OURS")}, map[string]string{"g": changeLine(tenLines("g"), 0, "THEIRS")})
	tr.writeFile("keep", "local\n")

	if result, err := tr.merge("feature", MergeOptions{}); err != nil || !result.Committed {
		t.Fatalf("result = %+v, %v", result, err)
	}
	if tr.readFile("keep") != "local\n" {
		t.Fatalf("the local change was lost")
	}
}

func TestSquashLeavesTheChangesStagedWithASquashMessage(t *testing.T) {
	tr := newTestRepo(t)
	tr.fork(map[string]string{"f": changeLine(tenLines("f"), 0, "OURS")}, map[string]string{"g": changeLine(tenLines("g"), 0, "THEIRS")})
	head, theirs := tr.branchTarget("main"), tr.branchTarget("feature")

	result, err := tr.merge("feature", MergeOptions{Mode: MergeSquash})

	if err != nil || result.Committed || tr.branchTarget("main") != head {
		t.Fatalf("result = %+v, %v", result, err)
	}
	state := tr.mergeState()
	want := "Squashed commit of the following:\n\ncommit " + theirs.String() + "\nAuthor: ann <ann@example.com>\n"
	if state.InProgress() || !strings.HasPrefix(state.Message, want) || !strings.HasSuffix(state.Message, "\n\n    theirs\n") {
		t.Fatalf("state = %+v", state)
	}
	if entry, _ := entryOf(t, tr.index(), "g"); entry.ID == (hash.ObjectID{}) || !strings.HasPrefix(tr.readFile("g"), "THEIRS") {
		t.Fatalf("their change is not staged")
	}
}

func TestSquashMessageListsMergeParentsAndIndentsTheBody(t *testing.T) {
	tr := newTestRepo(t)
	tr.fork(map[string]string{"f": changeLine(tenLines("f"), 0, "OURS")}, map[string]string{"g": changeLine(tenLines("g"), 0, "THEIRS")})
	tr.switchTo("feature")
	sideBase := tr.branchTarget("feature")
	tr.createBranch("side", sideBase)
	tr.commitFiles("feature two\n\nbody line\n\nlast", map[string]string{"two": "two\n"})
	tr.switchTo("side")
	tr.commitFiles("side", map[string]string{"side": "side\n"})
	tr.switchTo("feature")
	merged, err := tr.merge("side", MergeOptions{})
	if err != nil || !merged.Committed {
		t.Fatalf("merge side = %+v, %v", merged, err)
	}
	tr.switchTo("main")

	if _, err := tr.merge("feature", MergeOptions{Mode: MergeSquash}); err != nil {
		t.Fatal(err)
	}

	message := tr.mergeState().Message
	if !strings.Contains(message, "\nMerge: ") || !strings.Contains(message, "    feature two\n    \n    body line\n    \n    last\n") {
		t.Fatalf("message:\n%s", message)
	}
}

func TestSquashWithAConflictListsItForTheCommit(t *testing.T) {
	tr := newTestRepo(t)
	tr.conflictingFork()

	result, err := tr.merge("feature", MergeOptions{Mode: MergeSquash})

	if err != nil || result.Clean() {
		t.Fatalf("result = %+v, %v", result, err)
	}
	if message := tr.mergeState().Message; !strings.HasPrefix(message, "Squashed commit") || !strings.HasSuffix(message, "\n# Conflicts:\n#\tf\n") {
		t.Fatalf("message:\n%s", message)
	}
}

func TestNoCommitStopsWithTheMergeReadyToCommit(t *testing.T) {
	for _, mode := range []MergeMode{MergeFastForward, MergeNoFastForward} {
		tr := newTestRepo(t)
		tr.fork(map[string]string{"f": changeLine(tenLines("f"), 0, "OURS")}, map[string]string{"g": changeLine(tenLines("g"), 0, "THEIRS")})
		head := tr.branchTarget("main")

		result, err := tr.merge("feature", MergeOptions{NoCommit: true, Mode: mode})

		state := tr.mergeState()
		if err != nil || result.Committed || tr.branchTarget("main") != head || !state.InProgress() || state.NoFastForward != (mode == MergeNoFastForward) {
			t.Fatalf("mode %d: result = %+v, %v, state = %+v", mode, result, err, state)
		}
	}
}

func TestMergeIntoAnUnbornBranchTakesTheTarget(t *testing.T) {
	tr := newTestRepo(t)
	base := tr.commitFiles("base", map[string]string{"f": "f\n"})
	tr.createBranch("feature", base)
	tr.writeRawHead("ref: refs/heads/fresh\n")
	tr.saveIndex(index.New(index.Version2))
	tr.remove("f")

	result, err := tr.merge("feature", MergeOptions{})

	if err != nil || !result.FastForward || tr.branchTarget("fresh") != base || tr.readFile("f") != "f\n" {
		t.Fatalf("result = %+v, %v", result, err)
	}
}

func TestMergeOfCrissCrossHistoriesUsesAMergedBase(t *testing.T) {
	tr := newTestRepo(t)
	f, g := tenLines("f"), tenLines("g")
	base := tr.commitFiles("base", map[string]string{"f": f, "g": g})
	tr.createBranch("feature", base)
	tr.commitFiles("ours 1", map[string]string{"f": changeLine(f, 0, "OURS")})
	tr.switchTo("feature")
	tr.commitFiles("theirs 1", map[string]string{"g": changeLine(g, 0, "THEIRS")})
	if _, err := tr.merge("main", MergeOptions{}); err != nil {
		t.Fatal(err)
	}
	tr.switchTo("main")
	if _, err := tr.merge("feature~1", MergeOptions{}); err != nil {
		t.Fatal(err)
	}
	tr.commitFiles("ours 2", map[string]string{"f": changeLine(changeLine(f, 0, "OURS"), 9, "OURS AGAIN")})
	tr.switchTo("feature")
	tr.commitFiles("theirs 2", map[string]string{"g": changeLine(changeLine(g, 0, "THEIRS"), 9, "THEIRS AGAIN")})
	tr.switchTo("main")

	result, err := tr.merge("feature", MergeOptions{})

	if err != nil || !result.Committed {
		t.Fatalf("result = %+v, %v", result, err)
	}
	if tr.readFile("g") != changeLine(changeLine(g, 0, "THEIRS"), 9, "THEIRS AGAIN") {
		t.Fatalf("g:\n%s", tr.readFile("g"))
	}
}

func TestMergeWithoutAnIdentityCannotCommit(t *testing.T) {
	tr := newTestRepo(t)
	tr.fork(map[string]string{"f": "ours\n"}, map[string]string{"g": "theirs\n"})
	data, err := tr.repo.CommonRoot().ReadFile("config")
	if err != nil {
		t.Fatal(err)
	}
	stripped := strings.Replace(string(data), "[user]\n\tname = ann\n\temail = ann@example.com\n", "", 1)
	if err := tr.repo.CommonRoot().WriteFile("config", []byte(stripped), 0o666); err != nil {
		t.Fatal(err)
	}
	tr.repo = tr.reopen()

	if _, err := tr.merge("feature", MergeOptions{}); !errors.Is(err, ErrMissingIdentity) {
		t.Fatalf("err = %v, want ErrMissingIdentity", err)
	}
	if result, err := tr.merge("feature", MergeOptions{Mode: MergeSquash}); err != nil || result.Committed {
		t.Fatalf("a squash needs no identity: %+v, %v", result, err)
	}
}

func TestMergeInABareRepositoryFails(t *testing.T) {
	tr := newBareTestRepo(t)

	if _, err := Merge(t.Context(), tr.repo, "main", MergeOptions{}); !errors.Is(err, ErrBareRepository) {
		t.Fatalf("err = %v, want ErrBareRepository", err)
	}
}

func TestMergeMessageNamesWhatWasMerged(t *testing.T) {
	tr := newTestRepo(t)
	base := tr.commitFiles("base", map[string]string{"f": "f\n"})
	tr.createBranch("feature", base)
	store := tr.refs()
	onMain := headTarget{ref: "refs/heads/main"}
	onDev := headTarget{ref: "refs/heads/dev"}
	detached := headTarget{ref: "HEAD", detached: true}
	for _, c := range []struct {
		name string
		ref  string
		head headTarget
		want string
	}{
		{"feature", "refs/heads/feature", onMain, "Merge branch 'feature'\n"},
		{"feature", "refs/heads/feature", onDev, "Merge branch 'feature' into dev\n"},
		{"feature", "refs/heads/feature", detached, "Merge branch 'feature' into HEAD\n"},
		{"origin/x", "refs/remotes/origin/x", onMain, "Merge remote-tracking branch 'origin/x'\n"},
		{"v1", "refs/tags/v1", onMain, "Merge tag 'v1'\n"},
		{"abc123", "", onMain, "Merge commit 'abc123'\n"},
		{"feature~0", "", onMain, "Merge branch 'feature'\n"},
		{"feature~2", "", onMain, "Merge branch 'feature' (early part)\n"},
		{"feature~", "", onMain, "Merge branch 'feature' (early part)\n"},
		{"feature^^", "", onMain, "Merge branch 'feature' (early part)\n"},
		{"missing~1", "", onMain, "Merge commit 'missing~1'\n"},
		{"feature~x", "", onMain, "Merge commit 'feature~x'\n"},
	} {
		if got := defaultMergeMessage(tr.repo, store, c.name, refs.Name(c.ref), c.head); got != c.want {
			t.Errorf("%s on %s: %q, want %q", c.name, c.head.ref, got, c.want)
		}
	}
}

func TestSuppressDestFollowsTheConfig(t *testing.T) {
	tr := newTestRepo(t)
	tr.appendConfig("[merge]\n\tsuppressDest = release/*\n\tsuppressDest =\n\tsuppressDest = dev*\n")
	tr.repo = tr.reopen()
	for branch, want := range map[string]bool{"main": false, "release/1": false, "develop": true} {
		if got := suppressesDest(tr.repo, branch); got != want {
			t.Errorf("%s: %v, want %v", branch, got, want)
		}
	}
}

func TestReadMergeStateRejectsAGarbledMergeHead(t *testing.T) {
	tr := newTestRepo(t)
	if err := tr.repo.Root().WriteFile(mergeHeadFile, []byte("not a hash\n"), 0o666); err != nil {
		t.Fatal(err)
	}

	if _, err := ReadMergeState(tr.repo); err == nil {
		t.Fatal("a garbled MERGE_HEAD was accepted")
	}
	if _, err := tr.merge("main", MergeOptions{}); err == nil {
		t.Fatal("a merge started over a garbled MERGE_HEAD")
	}
	if err := AbortMerge(t.Context(), tr.repo); err == nil {
		t.Fatal("an abort ran over a garbled MERGE_HEAD")
	}
}

func TestAModifyDeleteConflictStagesOnlyTheSidesThatExist(t *testing.T) {
	tr := newTestRepo(t)
	tr.fork(map[string]string{"f": changeLine(tenLines("f"), 4, "OURS")}, map[string]string{"f": ""})

	result, err := tr.merge("feature", MergeOptions{})

	if err != nil || !slices.Equal(result.Conflicts, []string{"f"}) {
		t.Fatalf("result = %+v, %v", result, err)
	}
	if got := tr.stageEntries("f"); !slices.Equal(got, []index.Stage{index.StageAncestor, index.StageOurs}) {
		t.Fatalf("stages = %v", got)
	}
	if tr.readFile("f") != changeLine(tenLines("f"), 4, "OURS") {
		t.Fatalf("our version is not left in the working tree")
	}
}

func TestMergeBasesWithoutACommonAncestorMergeOverAnEmptyTree(t *testing.T) {
	tr := newTestRepo(t)
	db := tr.db()
	blob := func(text string) merge.Entry {
		id, err := db.Put(object.TypeBlob, []byte(text))
		if err != nil {
			t.Fatal(err)
		}
		return merge.Entry{Mode: object.ModeBlob, ID: id}
	}
	commit := func(files merge.Snapshot, message string, parents ...hash.ObjectID) hash.ObjectID {
		tree, err := files.Write(db)
		if err != nil {
			t.Fatal(err)
		}
		sig := object.Signature{Name: "ann", Email: "ann@example.com", When: mergeTime}
		id, err := db.PutObject(&object.Commit{Tree: tree, Parents: parents, Author: sig, Committer: sig, Message: message + "\n"})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	a, b := blob("a\n"), blob("b\n")
	left := commit(merge.Snapshot{"a": a}, "left")
	right := commit(merge.Snapshot{"b": b}, "right")
	ours := commit(merge.Snapshot{"a": a, "b": b, "ours": blob("ours\n")}, "ours", left, right)
	theirs := commit(merge.Snapshot{"a": a, "b": b, "theirs": blob("theirs\n")}, "theirs", right, left)
	tr.createBranch("feature", theirs)
	tr.createBranch("work", ours)
	tr.switchTo("work")

	result, err := tr.merge("feature", MergeOptions{})

	if err != nil || !result.Committed || tr.readFile("theirs") != "theirs\n" || tr.readFile("ours") != "ours\n" {
		t.Fatalf("result = %+v, %v", result, err)
	}
}

func TestAnUnreadableHeadStopsMergeAndAbort(t *testing.T) {
	tr := newTestRepo(t)
	tr.conflictingFork()
	if _, err := tr.merge("feature", MergeOptions{}); err != nil {
		t.Fatal(err)
	}
	feature := tr.branchTarget("feature").String()
	tr.writeRawHead("garbage\n")

	if err := AbortMerge(t.Context(), tr.repo); err == nil {
		t.Fatal("abort ran with an unreadable HEAD")
	}
	if err := clearMergeState(tr.repo); err != nil {
		t.Fatal(err)
	}
	if _, err := tr.merge(feature, MergeOptions{}); err == nil {
		t.Fatal("merge ran with an unreadable HEAD")
	}
}

func TestMergeReplacesAFileWithTheirDirectory(t *testing.T) {
	tr := newTestRepo(t)
	tr.fork(map[string]string{"f": changeLine(tenLines("f"), 0, "OURS")}, map[string]string{"keep": "", "keep/inside": "inside\n"})

	result, err := tr.merge("feature", MergeOptions{})

	if err != nil || !result.Committed || tr.readFile("keep/inside") != "inside\n" {
		t.Fatalf("result = %+v, %v", result, err)
	}
}

func TestSwitchReplacesAFileWithADirectory(t *testing.T) {
	tr := newTestRepo(t)
	base := tr.commitFiles("base", map[string]string{"d": "file\n"})
	tr.createBranch("other", base)
	tr.switchTo("other")
	tr.commitFiles("dir", map[string]string{"d": "", "d/x": "inside\n"})
	tr.switchTo("main")

	tr.switchTo("other")

	if tr.readFile("d/x") != "inside\n" {
		t.Fatal("the directory did not replace the file")
	}
}

func TestSwitchRefusesToReplaceADirectoryHoldingUntrackedFiles(t *testing.T) {
	tr := newTestRepo(t)
	base := tr.commitFiles("base", map[string]string{"d": "file\n"})
	tr.createBranch("other", base)
	tr.switchTo("other")
	tr.commitFiles("dir", map[string]string{"d": "", "d/x": "inside\n"})
	tr.writeFile("d/sub/untracked", "mine\n")

	err := Switch(t.Context(), tr.repo, "main", SwitchOptions{})

	var overwrite *OverwriteError
	if !errors.As(err, &overwrite) || tr.readFile("d/sub/untracked") != "mine\n" {
		t.Fatalf("err = %v", err)
	}
}

func TestMergeReplacesOurDirectoryWithTheirFile(t *testing.T) {
	tr := newTestRepo(t)
	base := tr.commitFiles("base", map[string]string{"d/x": "inside\n", "f": tenLines("f")})
	tr.createBranch("feature", base)
	tr.commitFiles("ours", map[string]string{"f": changeLine(tenLines("f"), 0, "OURS")})
	tr.switchTo("feature")
	tr.commitFiles("theirs", map[string]string{"d/x": "", "d": "file\n"})
	tr.switchTo("main")

	result, err := tr.merge("feature", MergeOptions{})

	if err != nil || !result.Committed || tr.readFile("d") != "file\n" {
		t.Fatalf("result = %+v, %v", result, err)
	}
}

func TestSwitchStopsWhenADirectoryInTheWayCannotBeRead(t *testing.T) {
	tr := newTestRepo(t)
	base := tr.commitFiles("base", map[string]string{"d": "file\n"})
	tr.createBranch("other", base)
	tr.switchTo("other")
	tr.commitFiles("dir", map[string]string{"d": "", "d/x": "inside\n"})
	swapRootOpenFailForPath(t, "d")

	if err := Switch(t.Context(), tr.repo, "main", SwitchOptions{}); !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want the injected failure", err)
	}
}
