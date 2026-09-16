package ops

import (
	"errors"
	"os"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/index"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/repo"
)

func nestedRepo(t *testing.T, super *testRepo, rel string) *testRepo {
	t.Helper()
	dir := super.path(rel)
	r, err := repo.Init(dir, repo.InitOptions{InitialBranch: "main", NoSystem: true, GlobalFile: super.globalFile})
	if err != nil {
		t.Fatalf("repo.Init returned error %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })
	return &testRepo{t: t, dir: dir, repo: r, clock: super.clock, globalFile: super.globalFile}
}

func (r *testRepo) commitNamedFile(name string) hash.ObjectID {
	r.t.Helper()
	r.writeFile(name, name+"\n")
	mustStage(r.t, r, name)
	return r.commitAll(name)
}

func requireGitlink(t *testing.T, r *testRepo, rel string, want hash.ObjectID) {
	t.Helper()
	idx := r.index()
	entry, ok := entryOf(t, idx, rel)
	if !ok || entry.Mode != object.ModeSubmodule || entry.ID != want {
		t.Fatalf("%s = %+v, %v; want a gitlink to %s", rel, entry, ok, want)
	}
	for tracked := range idx.Paths(rel + "/") {
		t.Fatalf("%s is tracked inside the submodule", tracked)
	}
	if len(idx.Conflicts(rel)) > 0 {
		t.Fatalf("%s is still conflicted", rel)
	}
}

func TestStageRecordsTheCommitASubmoduleHasCheckedOut(t *testing.T) {
	r := newTestRepo(t)
	nested := nestedRepo(t, r, "libs/sub")
	first := nested.commitNamedFile("a.txt")

	mustStage(t, r, "libs")
	requireGitlink(t, r, "libs/sub", first)

	second := nested.commitNamedFile("b.txt")
	mustStage(t, r, "libs/sub")
	requireGitlink(t, r, "libs/sub", second)
}

func TestStagingAParentKeepsAnUninitialisedSubmodule(t *testing.T) {
	r := newTestRepo(t)
	pointer := hash.SumSHA1("commit", []byte("elsewhere"))
	idx := r.index()
	idx.Add(index.Entry{Path: "libs/sub", Mode: object.ModeSubmodule, ID: pointer, Stage: index.StageMerged})
	r.saveIndex(idx)
	if err := os.MkdirAll(r.path("libs/sub"), 0o777); err != nil {
		t.Fatal(err)
	}
	r.writeFile("libs/x.txt", "x\n")

	mustStage(t, r, "libs")

	requireGitlink(t, r, "libs/sub", pointer)
	if _, ok := entryOf(t, r.index(), "libs/x.txt"); !ok {
		t.Fatal("libs/x.txt was not staged")
	}
}

func TestStageAddsAnUntrackedNestedRepositoryAsAGitlink(t *testing.T) {
	r := newTestRepo(t)
	head := nestedRepo(t, r, "vendor/lib").commitNamedFile("x.txt")
	r.writeFile("vendor/own.txt", "own\n")

	mustStage(t, r, "vendor")

	requireGitlink(t, r, "vendor/lib", head)
	if _, ok := entryOf(t, r.index(), "vendor/own.txt"); !ok {
		t.Fatal("vendor/own.txt was not staged")
	}
}

func TestStageTreatsANestedRepositoryOverTrackedFilesAsADirectory(t *testing.T) {
	r := newTestRepo(t)
	r.commitFiles("tracked", map[string]string{"vendor/lib/x.txt": "x\n"})
	nestedRepo(t, r, "vendor/lib").commitNamedFile("y.txt")
	r.writeFile("vendor/lib/x.txt", "changed\n")

	mustStage(t, r, "vendor")

	idx := r.index()
	if entry, ok := entryOf(t, idx, "vendor/lib"); ok {
		t.Fatalf("vendor/lib became %+v", entry)
	}
	for _, rel := range []string{"vendor/lib/x.txt", "vendor/lib/y.txt"} {
		if _, ok := entryOf(t, idx, rel); !ok {
			t.Fatalf("%s was not staged", rel)
		}
	}
}

func TestStageRefusesANestedRepositoryWithoutACommit(t *testing.T) {
	r := newTestRepo(t)
	nestedRepo(t, r, "vendor/lib").writeFile("x.txt", "x\n")
	r.writeFile("vendor/own.txt", "own\n")

	err := Stage(t.Context(), r.repo, []string{"vendor"}, StageOptions{})

	if !errors.Is(err, ErrNoCommitCheckedOut) {
		t.Fatalf("Stage = %v, want ErrNoCommitCheckedOut", err)
	}
	if r.index().Len() != 0 {
		t.Fatal("a refused stage changed the index")
	}
}

func TestStageLeavesATrackedSubmoduleWithoutACommitOrOutsideTheSparseCheckoutAlone(t *testing.T) {
	r := newTestRepo(t)
	pointer := hash.SumSHA1("commit", []byte("elsewhere"))
	nestedRepo(t, r, "unborn")
	nestedRepo(t, r, "skipped").commitNamedFile("x.txt")
	idx := r.index()
	idx.Add(index.Entry{Path: "unborn", Mode: object.ModeSubmodule, ID: pointer, Stage: index.StageMerged})
	idx.Add(index.Entry{Path: "skipped", Mode: object.ModeSubmodule, ID: pointer, Stage: index.StageMerged, SkipWorktree: true})
	r.saveIndex(idx)

	if err := Stage(t.Context(), r.repo, []string{"unborn", "skipped"}, StageOptions{}); err != nil {
		t.Fatalf("Stage returned error %v", err)
	}

	requireGitlink(t, r, "unborn", pointer)
	requireGitlink(t, r, "skipped", pointer)
}

func TestStageAddsTheFilesOfADirectoryWhoseGitDirectoryIsBroken(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("vendor/lib/.git/HEAD", "ref: refs/heads/main\n")
	r.writeFile("vendor/lib/x.txt", "x\n")

	mustStage(t, r, "vendor")

	idx := r.index()
	if _, ok := entryOf(t, idx, "vendor/lib/x.txt"); !ok {
		t.Fatal("vendor/lib/x.txt was not staged")
	}
	if idx.Len() != 1 {
		t.Fatalf("index holds %d entries, want only vendor/lib/x.txt", idx.Len())
	}
}

func TestStageResolvesASubmoduleConflictWithTheCheckedOutCommit(t *testing.T) {
	r := newTestRepo(t)
	head := nestedRepo(t, r, "sub").commitNamedFile("x.txt")
	idx := r.index()
	for stage, seed := range map[index.Stage]string{index.StageAncestor: "base", index.StageOurs: "ours", index.StageTheirs: "theirs"} {
		idx.Add(index.Entry{Path: "sub", Mode: object.ModeSubmodule, ID: hash.SumSHA1("commit", []byte(seed)), Stage: stage})
	}
	r.saveIndex(idx)

	mustStage(t, r, "sub")

	requireGitlink(t, r, "sub", head)
}

func TestStageRejectsANestedRepositoryAtAPathGitRefuses(t *testing.T) {
	r := newTestRepo(t)
	nestedRepo(t, r, "deep/.git").commitNamedFile("x.txt")
	wt, err := openWorkingTree(r.repo)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = wt.close() })
	stager := &stager{ctx: t.Context(), wt: wt, idx: index.New(index.Version2), rules: index.DefaultPathRules()}

	if _, err := stager.stageGitlink("deep/.git", false); !errors.Is(err, index.ErrUnsafePath) {
		t.Fatalf("stageGitlink = %v, want ErrUnsafePath", err)
	}
}
