package refs

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

func TestOpenFailsWhenGitDirIsMissing(t *testing.T) {
	if _, err := Open(Options{GitDir: filepath.Join(t.TempDir(), "absent")}); err == nil {
		t.Fatal("Open of a missing directory returned no error")
	}
}

func TestOpenFailsWhenCommonDirIsMissing(t *testing.T) {
	dir := newGitDir(t)
	_, err := Open(Options{GitDir: dir, CommonDir: filepath.Join(dir, "absent")})
	if err == nil {
		t.Fatal("Open of a missing common directory returned no error")
	}
}

func TestOpenSharesOneRootWhenCommonDirEqualsGitDir(t *testing.T) {
	dir := newGitDir(t)
	store := openStoreWith(t, Options{GitDir: dir, CommonDir: dir + string(filepath.Separator)})
	if store.split() {
		t.Fatal("store opened two roots for one directory")
	}
	if store.treeFor(HEAD).root != store.treeFor(BranchName("main")).root {
		t.Fatal("per worktree and common trees differ")
	}
}

func TestLookupReadsLooseAndPackedReferences(t *testing.T) {
	dir := newGitDir(t)
	writeAt(t, dir, "refs/heads/main", oidFrom(t, "11").String()+"\n")
	writeAt(t, dir, packedRefsFile, packedHeaderPlain+
		oidFrom(t, "22").String()+" refs/heads/main\n"+
		oidFrom(t, "33").String()+" refs/heads/packed\n")
	store := openStore(t, dir)

	loose, err := store.Lookup(BranchName("main"))
	if err != nil {
		t.Fatalf("Lookup returned error %v", err)
	}
	if loose.Target != oidFrom(t, "11") {
		t.Fatalf("loose reference wins with %s", loose.Target)
	}
	packed, err := store.Lookup(BranchName("packed"))
	if err != nil {
		t.Fatalf("Lookup returned error %v", err)
	}
	if packed.Target != oidFrom(t, "33") {
		t.Fatalf("packed reference is %s", packed.Target)
	}
	if _, err := store.Lookup(BranchName("absent")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Lookup of a missing reference returned %v", err)
	}
	if _, err := store.Lookup(Name("refs/heads/bad name")); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("Lookup of an invalid name returned %v", err)
	}
}

func TestLookupReadsSymbolicAndDetachedHead(t *testing.T) {
	dir := newGitDir(t)
	writeAt(t, dir, "refs/heads/main", oidFrom(t, "11").String()+"\n")
	store := openStore(t, dir)

	head, err := store.Lookup(HEAD)
	if err != nil {
		t.Fatalf("Lookup returned error %v", err)
	}
	if !head.IsSymbolic() || head.SymbolicTarget != BranchName("main") {
		t.Fatalf("HEAD is %+v", head)
	}
	resolved, err := store.Resolve(HEAD)
	if err != nil {
		t.Fatalf("Resolve returned error %v", err)
	}
	if resolved.Name != BranchName("main") || resolved.Target != oidFrom(t, "11") {
		t.Fatalf("resolved HEAD is %+v", resolved)
	}

	writeAt(t, dir, "HEAD", oidFrom(t, "44").String()+"\n")
	detached, err := store.Lookup(HEAD)
	if err != nil {
		t.Fatalf("Lookup returned error %v", err)
	}
	if detached.IsSymbolic() || detached.Target != oidFrom(t, "44") {
		t.Fatalf("detached HEAD is %+v", detached)
	}
}

func TestLookupAcceptsExtraDataAfterObjectID(t *testing.T) {
	dir := newGitDir(t)
	writeAt(t, dir, "FETCH_HEAD", oidFrom(t, "11").String()+"\t\tbranch 'main' of example\n")
	writeAt(t, dir, "ORIG_HEAD", oidFrom(t, "22").String()+"   \n")
	writeAt(t, dir, "refs/heads/spaced", "ref:   refs/heads/main  \n")
	store := openStore(t, dir)

	for name, want := range map[Name]hash.ObjectID{
		FetchHead: oidFrom(t, "11"),
		OrigHead:  oidFrom(t, "22"),
	} {
		ref, err := store.Lookup(name)
		if err != nil {
			t.Fatalf("Lookup(%s) returned error %v", name, err)
		}
		if ref.Target != want {
			t.Fatalf("%s is %s", name, ref.Target)
		}
	}
	spaced, err := store.Lookup(BranchName("spaced"))
	if err != nil {
		t.Fatalf("Lookup returned error %v", err)
	}
	if spaced.SymbolicTarget != BranchName("main") {
		t.Fatalf("symbolic target is %q", spaced.SymbolicTarget)
	}
}

func TestLookupRejectsMalformedReferenceFiles(t *testing.T) {
	dir := newGitDir(t)
	broken := map[Name]string{
		BranchName("short"):    "1111\n",
		BranchName("bad-hex"):  "111111111111111111111111111111111111111z\n",
		BranchName("trailing"): oidFrom(t, "11").String() + "junk\n",
		BranchName("symbolic"): "ref: refs/heads/bad name\n",
	}
	for name, content := range broken {
		writeAt(t, dir, string(name), content)
	}
	store := openStore(t, dir)
	for name := range broken {
		if _, err := store.Lookup(name); !errors.Is(err, ErrMalformedRef) {
			t.Errorf("Lookup(%s) returned %v, want ErrMalformedRef", name, err)
		}
	}
}

func TestLookupTreatsDirectoryAsMissingReference(t *testing.T) {
	dir := newGitDir(t)
	writeAt(t, dir, "refs/heads/feature/work", oidFrom(t, "11").String()+"\n")
	store := openStore(t, dir)
	if _, err := store.Lookup(BranchName("feature")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Lookup of a directory returned %v", err)
	}
}

func TestLookupFailsWhenFileCannotBeRead(t *testing.T) {
	dir := newGitDir(t)
	writeAt(t, dir, "refs/heads/main", oidFrom(t, "11").String()+"\n")
	store := openStore(t, dir)
	swapReadFile(t, func(name string) bool { return name == "refs/heads/main" }, errors.New("broken"))
	if _, err := store.Lookup(BranchName("main")); !errors.Is(err, ErrReadFailed) {
		t.Fatalf("Lookup returned %v, want ErrReadFailed", err)
	}
}

func TestLookupFailsWhenPackedRefsIsMalformed(t *testing.T) {
	dir := newGitDir(t)
	writeAt(t, dir, packedRefsFile, "garbage\n")
	store := openStore(t, dir)
	if _, err := store.Lookup(BranchName("main")); !errors.Is(err, ErrMalformedPacked) {
		t.Fatalf("Lookup returned %v, want ErrMalformedPacked", err)
	}
}

func TestLookupIgnoresPackedEntriesForPerWorktreeNames(t *testing.T) {
	dir := newGitDir(t)
	writeAt(t, dir, packedRefsFile, packedHeaderPlain+oidFrom(t, "11").String()+" refs/bisect/good\n")
	store := openStore(t, dir)
	if _, err := store.Lookup(Name("refs/bisect/good")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Lookup returned %v, want ErrNotFound", err)
	}
}

func TestResolveFollowsChainAndStopsAtLoops(t *testing.T) {
	dir := newGitDir(t)
	writeAt(t, dir, "refs/heads/main", oidFrom(t, "11").String()+"\n")
	writeAt(t, dir, "refs/heads/first", "ref: refs/heads/second\n")
	writeAt(t, dir, "refs/heads/second", "ref: refs/heads/main\n")
	writeAt(t, dir, "refs/heads/loop", "ref: refs/heads/loop\n")
	store := openStore(t, dir)

	ref, err := store.Resolve(BranchName("first"))
	if err != nil {
		t.Fatalf("Resolve returned error %v", err)
	}
	if ref.Name != BranchName("main") {
		t.Fatalf("Resolve returned %s", ref.Name)
	}
	if _, err := store.Resolve(BranchName("loop")); !errors.Is(err, ErrTooManySymlinks) {
		t.Fatalf("Resolve of a loop returned %v", err)
	}
	if _, err := store.ResolveName(Name("refs/heads/bad name")); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("ResolveName of an invalid name returned %v", err)
	}
}

func TestResolveNameReturnsUnbornBranchOfHead(t *testing.T) {
	store := openStore(t, newGitDir(t))
	name, err := store.ResolveName(HEAD)
	if err != nil {
		t.Fatalf("ResolveName returned error %v", err)
	}
	if name != BranchName("main") {
		t.Fatalf("ResolveName returned %s", name)
	}
	if _, err := store.Resolve(HEAD); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Resolve of an unborn branch returned %v", err)
	}
	target, err := store.resolveTarget(HEAD)
	if err != nil || !target.IsZero() {
		t.Fatalf("resolveTarget returned %v, %v", target, err)
	}
	if _, err := store.resolveTarget(Name("refs/heads/bad name")); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("resolveTarget of an invalid name returned %v", err)
	}
}

func TestPeelUsesPackedValueThenPeeler(t *testing.T) {
	dir := newGitDir(t)
	writeAt(t, dir, packedRefsFile, packedHeaderFull+
		oidFrom(t, "22").String()+" refs/tags/packed\n"+
		"^"+oidFrom(t, "33").String()+"\n")
	writeAt(t, dir, "refs/tags/loose", oidFrom(t, "44").String()+"\n")
	writeAt(t, dir, "refs/tags/light", oidFrom(t, "55").String()+"\n")
	peeler := fakePeeler{tags: map[hash.ObjectID]hash.ObjectID{oidFrom(t, "44"): oidFrom(t, "66")}}
	store := openStoreWith(t, Options{GitDir: dir, Peeler: peeler})

	packed, err := store.Peel(TagName("packed"))
	if err != nil || packed != oidFrom(t, "33") {
		t.Fatalf("Peel returned %v, %v", packed, err)
	}
	loose, err := store.Peel(TagName("loose"))
	if err != nil || loose != oidFrom(t, "66") {
		t.Fatalf("Peel returned %v, %v", loose, err)
	}
	if _, err := store.Peel(TagName("light")); !errors.Is(err, ErrNotTag) {
		t.Fatalf("Peel of a lightweight tag returned %v", err)
	}
	if _, err := store.Peel(TagName("absent")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Peel of a missing reference returned %v", err)
	}
}

func TestPeelFailsWithoutPeelerAndOnPeelerErrors(t *testing.T) {
	dir := newGitDir(t)
	writeAt(t, dir, "refs/tags/loose", oidFrom(t, "44").String()+"\n")
	plain := openStore(t, dir)
	if _, err := plain.Peel(TagName("loose")); !errors.Is(err, ErrNoPeeler) {
		t.Fatalf("Peel without a peeler returned %v", err)
	}
	broken := errors.New("odb is broken")
	failing := openStoreWith(t, Options{GitDir: dir, Peeler: fakePeeler{err: broken}})
	if _, err := failing.Peel(TagName("loose")); !errors.Is(err, broken) {
		t.Fatalf("Peel returned %v, want the peeler error", err)
	}
}

func TestReflogPolicyFromConfigReadsCoreValue(t *testing.T) {
	cases := map[string]ReflogPolicy{
		"":       ReflogDefault,
		"true":   ReflogEnabled,
		"false":  ReflogDisabled,
		"always": ReflogAlways,
	}
	for value, want := range cases {
		cfg := loadConfig(t, value)
		policy, err := ReflogPolicyFromConfig(cfg)
		if err != nil {
			t.Fatalf("ReflogPolicyFromConfig(%q) returned error %v", value, err)
		}
		if policy != want {
			t.Fatalf("ReflogPolicyFromConfig(%q) returned %v, want %v", value, policy, want)
		}
	}
	if _, err := ReflogPolicyFromConfig(loadConfig(t, "maybe")); err == nil {
		t.Fatal("ReflogPolicyFromConfig accepted a broken value")
	}
}
