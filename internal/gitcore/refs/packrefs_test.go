package refs

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

func TestPackRefsMergesLooseAndPackedReferences(t *testing.T) {
	dir := newGitDir(t)
	writeAt(t, dir, "refs/heads/main", oidFrom(t, "11").String()+"\n")
	writeAt(t, dir, "refs/heads/topic/one", oidFrom(t, "22").String()+"\n")
	writeAt(t, dir, "refs/remotes/origin/HEAD", "ref: refs/heads/main\n")
	writeAt(t, dir, "refs/bisect/good", oidFrom(t, "33").String()+"\n")
	writeAt(t, dir, packedRefsFile, packedHeaderPlain+oidFrom(t, "44").String()+" refs/tags/old\n")
	store := openStore(t, dir)

	if err := store.PackRefs(false); err != nil {
		t.Fatalf("PackRefs returned error %v", err)
	}
	packed := readAt(t, dir, packedRefsFile)
	want := packedHeaderPlain +
		oidFrom(t, "11").String() + " refs/heads/main\n" +
		oidFrom(t, "22").String() + " refs/heads/topic/one\n" +
		oidFrom(t, "44").String() + " refs/tags/old\n"
	if packed != want {
		t.Fatalf("packed-refs is\n%q\nwant\n%q", packed, want)
	}
	if !existsAt(dir, "refs/heads/main") {
		t.Fatal("loose reference was pruned without a request")
	}
}

func TestPackRefsPrunesLooseReferencesAndEmptyDirectories(t *testing.T) {
	dir := newGitDir(t)
	writeAt(t, dir, "refs/heads/main", oidFrom(t, "11").String()+"\n")
	writeAt(t, dir, "refs/heads/topic/deep/one", oidFrom(t, "22").String()+"\n")
	store := openStore(t, dir)

	if err := store.PackRefs(true); err != nil {
		t.Fatalf("PackRefs returned error %v", err)
	}
	for _, rel := range []string{"refs/heads/main", "refs/heads/topic/deep/one", "refs/heads/topic"} {
		if existsAt(dir, rel) {
			t.Errorf("%s survived pruning", rel)
		}
	}
	if !existsAt(dir, "refs/heads") {
		t.Fatal("pruning removed the top level directory")
	}
	got := collect(t, store, RefsPrefix)
	if !slices.Equal(names(got), []string{"refs/heads/main", "refs/heads/topic/deep/one"}) {
		t.Fatalf("All returned %v", names(got))
	}
}

func TestPackRefsPeelsTagsWithPeeler(t *testing.T) {
	dir := newGitDir(t)
	writeAt(t, dir, "refs/tags/annotated", oidFrom(t, "11").String()+"\n")
	writeAt(t, dir, "refs/tags/light", oidFrom(t, "22").String()+"\n")
	writeAt(t, dir, packedRefsFile, packedHeaderPlain+oidFrom(t, "33").String()+" refs/tags/old\n")
	peeler := fakePeeler{tags: map[hash.ObjectID]hash.ObjectID{
		oidFrom(t, "11"): oidFrom(t, "44"),
		oidFrom(t, "33"): oidFrom(t, "55"),
	}}
	store := openStoreWith(t, Options{GitDir: dir, Peeler: peeler, Committer: testCommitter()})

	if err := store.PackRefs(true); err != nil {
		t.Fatalf("PackRefs returned error %v", err)
	}
	packed := readAt(t, dir, packedRefsFile)
	want := packedHeaderFull +
		oidFrom(t, "11").String() + " refs/tags/annotated\n" +
		"^" + oidFrom(t, "44").String() + "\n" +
		oidFrom(t, "22").String() + " refs/tags/light\n" +
		oidFrom(t, "33").String() + " refs/tags/old\n" +
		"^" + oidFrom(t, "55").String() + "\n"
	if packed != want {
		t.Fatalf("packed-refs is\n%q\nwant\n%q", packed, want)
	}
}

func TestPackRefsPrefersLooseValueOverPackedOne(t *testing.T) {
	dir := newGitDir(t)
	writeAt(t, dir, "refs/heads/main", oidFrom(t, "11").String()+"\n")
	writeAt(t, dir, packedRefsFile, packedHeaderPlain+oidFrom(t, "22").String()+" refs/heads/main\n")
	store := openStore(t, dir)

	if err := store.PackRefs(true); err != nil {
		t.Fatalf("PackRefs returned error %v", err)
	}
	want := packedHeaderPlain + oidFrom(t, "11").String() + " refs/heads/main\n"
	if got := readAt(t, dir, packedRefsFile); got != want {
		t.Fatalf("packed-refs is %q, want %q", got, want)
	}
}

func TestPackRefsSkipsReferenceThatDisappearsDuringTheWalk(t *testing.T) {
	dir := newGitDir(t)
	writeAt(t, dir, "refs/heads/gone", oidFrom(t, "11").String()+"\n")
	writeAt(t, dir, "refs/heads/main", oidFrom(t, "22").String()+"\n")
	store := openStore(t, dir)
	swapLstat(t, func(name string) bool { return name == "refs/heads/gone" })
	if err := store.PackRefs(false); err != nil {
		t.Fatalf("PackRefs returned error %v", err)
	}
	if strings.Contains(readAt(t, dir, packedRefsFile), "refs/heads/gone") {
		t.Fatal("packed-refs holds a reference that disappeared")
	}
}

func TestPruneLooseFailsWhenLockCannotBeCreated(t *testing.T) {
	dir := newGitDir(t)
	writeAt(t, dir, "refs/heads/main", oidFrom(t, "11").String()+"\n")
	store := openStore(t, dir)
	err := store.pruneLoose(Ref{Name: BranchName("main/deep"), Target: oidFrom(t, "11")})
	if !errors.Is(err, ErrWriteFailed) {
		t.Fatalf("pruneLoose returned %v, want ErrWriteFailed", err)
	}
}

func TestPackRefsKeepsPeeledValuesOfFullyPeeledFile(t *testing.T) {
	dir := newGitDir(t)
	writeAt(t, dir, packedRefsFile, packedHeaderFull+
		oidFrom(t, "11").String()+" refs/tags/one\n"+
		"^"+oidFrom(t, "22").String()+"\n")
	store := openStoreWith(t, Options{GitDir: dir, Peeler: fakePeeler{err: errors.New("must not peel")}})
	if err := store.PackRefs(false); err != nil {
		t.Fatalf("PackRefs returned error %v", err)
	}
	if !strings.Contains(readAt(t, dir, packedRefsFile), "^"+oidFrom(t, "22").String()) {
		t.Fatal("peeled value was lost")
	}
}

func TestPackRefsReportsBrokenState(t *testing.T) {
	malformed := newGitDir(t)
	writeAt(t, malformed, "refs/heads/main", "not an object id\n")
	if err := openStore(t, malformed).PackRefs(false); !errors.Is(err, ErrMalformedRef) {
		t.Fatalf("PackRefs returned %v, want ErrMalformedRef", err)
	}

	packed := newGitDir(t)
	writeAt(t, packed, packedRefsFile, "garbage\n")
	if err := openStore(t, packed).PackRefs(false); !errors.Is(err, ErrMalformedPacked) {
		t.Fatalf("PackRefs returned %v, want ErrMalformedPacked", err)
	}

	locked := newGitDir(t)
	writeAt(t, locked, packedRefsFile+lockSuffix, "")
	if err := openStore(t, locked).PackRefs(false); !errors.Is(err, ErrLocked) {
		t.Fatalf("PackRefs returned %v, want ErrLocked", err)
	}

	broken := errors.New("odb is broken")
	tags := newGitDir(t)
	writeAt(t, tags, "refs/tags/v1", oidFrom(t, "11").String()+"\n")
	tagStore := openStoreWith(t, Options{GitDir: tags, Peeler: fakePeeler{err: broken}})
	if err := tagStore.PackRefs(false); !errors.Is(err, broken) {
		t.Fatalf("PackRefs returned %v, want the peeler error", err)
	}

	packedTags := newGitDir(t)
	writeAt(t, packedTags, packedRefsFile, packedHeaderPlain+oidFrom(t, "11").String()+" refs/tags/v1\n")
	packedTagStore := openStoreWith(t, Options{GitDir: packedTags, Peeler: fakePeeler{err: broken}})
	if err := packedTagStore.PackRefs(false); !errors.Is(err, broken) {
		t.Fatalf("PackRefs returned %v, want the peeler error", err)
	}

	walk := newGitDir(t)
	writeAt(t, walk, "refs/heads/main", oidFrom(t, "11").String()+"\n")
	walkStore := openStore(t, walk)
	swapReadFile(t, func(name string) bool { return name == "refs/heads/main" }, errors.New("broken"))
	if err := walkStore.PackRefs(false); !errors.Is(err, ErrReadFailed) {
		t.Fatalf("PackRefs returned %v, want ErrReadFailed", err)
	}
}

func TestPackRefsSkipsReferencesThatChangeDuringPruning(t *testing.T) {
	dir := newGitDir(t)
	writeAt(t, dir, "refs/heads/main", oidFrom(t, "11").String()+"\n")
	writeAt(t, dir, "refs/heads/locked.lock", "")
	store := openStore(t, dir)

	if err := store.pruneLoose(Ref{Name: BranchName("locked"), Target: oidFrom(t, "11")}); err != nil {
		t.Fatalf("pruneLoose of a locked reference returned error %v", err)
	}
	if err := store.pruneLoose(Ref{Name: BranchName("absent"), Target: oidFrom(t, "11")}); err != nil {
		t.Fatalf("pruneLoose of a missing reference returned error %v", err)
	}
	if err := store.pruneLoose(Ref{Name: BranchName("main"), Target: oidFrom(t, "22")}); err != nil {
		t.Fatalf("pruneLoose of a changed reference returned error %v", err)
	}
	if !existsAt(dir, "refs/heads/main") {
		t.Fatal("changed reference was pruned")
	}
	writeAt(t, dir, "refs/heads/broken", "not an object id\n")
	if err := store.pruneLoose(Ref{Name: BranchName("broken")}); !errors.Is(err, ErrMalformedRef) {
		t.Fatalf("pruneLoose returned %v, want ErrMalformedRef", err)
	}
}

func TestPackRefsFailsWhenLooseReferenceCannotBeRemoved(t *testing.T) {
	dir := newGitDir(t)
	writeAt(t, dir, "refs/heads/main", oidFrom(t, "11").String()+"\n")
	store := openStore(t, dir)
	swapRemove(t, func(name string) bool { return name == "refs/heads/main" }, errors.New("busy"))
	if err := store.PackRefs(true); !errors.Is(err, ErrWriteFailed) {
		t.Fatalf("PackRefs returned %v, want ErrWriteFailed", err)
	}
}

func TestPackRefsFailsWhenLooseWalkFails(t *testing.T) {
	dir := t.TempDir()
	writeAt(t, dir, "HEAD", "ref: refs/heads/main\n")
	writeAt(t, dir, "refs", oidFrom(t, "11").String()+"\n")
	store := openStore(t, dir)
	if err := store.PackRefs(false); !errors.Is(err, ErrDirFailed) {
		t.Fatalf("PackRefs returned %v, want ErrDirFailed", err)
	}
}
