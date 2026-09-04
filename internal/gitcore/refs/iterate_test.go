package refs

import (
	"errors"
	"slices"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

func TestAllMergesLooseAndPackedInGitOrder(t *testing.T) {
	dir := newGitDir(t)
	writeAt(t, dir, "refs/heads/main", oidFrom(t, "11").String()+"\n")
	writeAt(t, dir, "refs/heads/a-b", oidFrom(t, "22").String()+"\n")
	writeAt(t, dir, "refs/heads/a/c", oidFrom(t, "33").String()+"\n")
	writeAt(t, dir, "refs/heads/main.lock", "")
	writeAt(t, dir, "refs/heads/.hidden", oidFrom(t, "44").String()+"\n")
	writeAt(t, dir, packedRefsFile, packedHeaderPlain+
		oidFrom(t, "55").String()+" refs/heads/main\n"+
		oidFrom(t, "66").String()+" refs/tags/v1\n")
	store := openStore(t, dir)

	got := collect(t, store, RefsPrefix)
	want := []string{"refs/heads/a-b", "refs/heads/a/c", "refs/heads/main", "refs/tags/v1"}
	if !slices.Equal(names(got), want) {
		t.Fatalf("All returned %v, want %v", names(got), want)
	}
	if got[2].Target != oidFrom(t, "11") {
		t.Fatalf("loose value does not win: %s", got[2].Target)
	}
}

func TestAllResolvesSymbolicReferences(t *testing.T) {
	dir := newGitDir(t)
	writeAt(t, dir, "refs/heads/main", oidFrom(t, "11").String()+"\n")
	writeAt(t, dir, "refs/remotes/origin/HEAD", "ref: refs/heads/main\n")
	writeAt(t, dir, "refs/remotes/origin/dangling", "ref: refs/heads/gone\n")
	store := openStore(t, dir)

	got := collect(t, store, RemotesPrefix)
	if len(got) != 2 {
		t.Fatalf("Prefix returned %v", names(got))
	}
	if got[0].SymbolicTarget != BranchName("main") || got[0].Target != oidFrom(t, "11") {
		t.Fatalf("symbolic reference is %+v", got[0])
	}
	if !got[1].Target.IsZero() {
		t.Fatalf("dangling reference is %+v", got[1])
	}
}

func TestAllPeelsTagsWithPeeler(t *testing.T) {
	dir := newGitDir(t)
	writeAt(t, dir, "refs/tags/loose", oidFrom(t, "11").String()+"\n")
	writeAt(t, dir, packedRefsFile, packedHeaderFull+
		oidFrom(t, "22").String()+" refs/tags/light\n"+
		oidFrom(t, "33").String()+" refs/tags/packed\n"+
		"^"+oidFrom(t, "44").String()+"\n")
	peeler := fakePeeler{tags: map[hash.ObjectID]hash.ObjectID{
		oidFrom(t, "11"): oidFrom(t, "55"),
		oidFrom(t, "22"): oidFrom(t, "66"),
	}}
	store := openStoreWith(t, Options{GitDir: dir, Peeler: peeler})

	got := collect(t, store, TagsPrefix)
	want := map[string]hash.ObjectID{
		"refs/tags/light":  hash.Zero,
		"refs/tags/loose":  oidFrom(t, "55"),
		"refs/tags/packed": oidFrom(t, "44"),
	}
	for _, ref := range got {
		if ref.Peeled != want[string(ref.Name)] {
			t.Errorf("%s peels to %s, want %s", ref.Name, ref.Peeled, want[string(ref.Name)])
		}
	}
	if len(got) != len(want) {
		t.Fatalf("Prefix returned %v", names(got))
	}
}

func TestPrefixWalksOnlyMatchingSubtree(t *testing.T) {
	dir := newGitDir(t)
	writeAt(t, dir, "refs/heads/feature/one", oidFrom(t, "11").String()+"\n")
	writeAt(t, dir, "refs/heads/feature/two", oidFrom(t, "22").String()+"\n")
	writeAt(t, dir, "refs/heads/main", oidFrom(t, "33").String()+"\n")
	writeAt(t, dir, packedRefsFile, packedHeaderPlain+oidFrom(t, "44").String()+" refs/tags/v1\n")
	store := openStore(t, dir)

	got := names(collect(t, store, "refs/heads/feature/"))
	if !slices.Equal(got, []string{"refs/heads/feature/one", "refs/heads/feature/two"}) {
		t.Fatalf("Prefix returned %v", got)
	}
	if partial := names(collect(t, store, "refs/heads/feature/t")); !slices.Equal(partial, []string{"refs/heads/feature/two"}) {
		t.Fatalf("Prefix returned %v", partial)
	}
	if all := names(collect(t, store, "")); len(all) != 4 {
		t.Fatalf("Prefix of an empty prefix returned %v", all)
	}
}

func TestPrefixFailsWhenWalkRootIsAFile(t *testing.T) {
	dir := newGitDir(t)
	writeAt(t, dir, "refs/heads/main", oidFrom(t, "11").String()+"\n")
	store := openStore(t, dir)
	if err := iterationError(t, store, "refs/heads/main/deep"); !errors.Is(err, ErrDirFailed) {
		t.Fatalf("Prefix returned %v, want ErrDirFailed", err)
	}
}

func TestIterationStopsWhenCallerBreaks(t *testing.T) {
	dir := newGitDir(t)
	writeAt(t, dir, "refs/heads/one", oidFrom(t, "11").String()+"\n")
	writeAt(t, dir, "refs/heads/two", oidFrom(t, "22").String()+"\n")
	store := openStore(t, dir)

	seen := 0
	for range store.All() {
		seen++
		break
	}
	if seen != 1 {
		t.Fatalf("iteration produced %d references", seen)
	}
}

func TestIterationReportsBrokenState(t *testing.T) {
	dir := newGitDir(t)
	writeAt(t, dir, "refs/heads/broken", "not an object id\n")
	store := openStore(t, dir)
	if err := iterationError(t, store, RefsPrefix); !errors.Is(err, ErrMalformedRef) {
		t.Fatalf("All returned %v, want ErrMalformedRef", err)
	}

	packedDir := newGitDir(t)
	writeAt(t, packedDir, packedRefsFile, "garbage\n")
	packedStore := openStore(t, packedDir)
	if err := iterationError(t, packedStore, RefsPrefix); !errors.Is(err, ErrMalformedPacked) {
		t.Fatalf("All returned %v, want ErrMalformedPacked", err)
	}

	loopDir := newGitDir(t)
	writeAt(t, loopDir, "refs/heads/loop", "ref: refs/heads/loop\n")
	loopStore := openStore(t, loopDir)
	if err := iterationError(t, loopStore, RefsPrefix); !errors.Is(err, ErrTooManySymlinks) {
		t.Fatalf("All returned %v, want ErrTooManySymlinks", err)
	}

	broken := errors.New("odb is broken")
	tagDir := newGitDir(t)
	writeAt(t, tagDir, "refs/tags/v1", oidFrom(t, "11").String()+"\n")
	tagStore := openStoreWith(t, Options{GitDir: tagDir, Peeler: fakePeeler{err: broken}})
	if err := iterationError(t, tagStore, RefsPrefix); !errors.Is(err, broken) {
		t.Fatalf("All returned %v, want the peeler error", err)
	}
}

func TestIterationSkipsReferenceThatDisappears(t *testing.T) {
	dir := newGitDir(t)
	writeAt(t, dir, "refs/heads/gone", oidFrom(t, "11").String()+"\n")
	writeAt(t, dir, "refs/heads/main", oidFrom(t, "22").String()+"\n")
	store := openStore(t, dir)
	swapLstat(t, func(name string) bool { return name == "refs/heads/gone" })
	if got := names(collect(t, store, RefsPrefix)); !slices.Equal(got, []string{"refs/heads/main"}) {
		t.Fatalf("All returned %v", got)
	}
}

func TestIterationSplitsPerWorktreeReferences(t *testing.T) {
	common := newGitDir(t)
	worktree := t.TempDir()
	writeAt(t, common, "refs/heads/main", oidFrom(t, "11").String()+"\n")
	writeAt(t, common, "refs/bisect/ignored", oidFrom(t, "22").String()+"\n")
	writeAt(t, worktree, "refs/bisect/good", oidFrom(t, "33").String()+"\n")
	writeAt(t, worktree, "refs/heads/ignored", oidFrom(t, "44").String()+"\n")
	writeAt(t, worktree, "HEAD", "ref: refs/heads/main\n")
	store := openStoreWith(t, Options{GitDir: worktree, CommonDir: common, Committer: testCommitter()})

	got := names(collect(t, store, RefsPrefix))
	if !slices.Equal(got, []string{"refs/bisect/good", "refs/heads/main"}) {
		t.Fatalf("All returned %v", got)
	}
	if err := iterationError(t, store, RefsPrefix); err != nil {
		t.Fatalf("All returned error %v", err)
	}
}

func TestPrefixFailsWhenPerWorktreeWalkFails(t *testing.T) {
	common := newGitDir(t)
	worktree := t.TempDir()
	writeAt(t, worktree, "refs/heads/main/deep", oidFrom(t, "11").String()+"\n")
	store := openStoreWith(t, Options{GitDir: worktree, CommonDir: common})
	if err := iterationError(t, store, "refs/heads/main/deep/more"); !errors.Is(err, ErrDirFailed) {
		t.Fatalf("Prefix returned %v, want ErrDirFailed", err)
	}
}
