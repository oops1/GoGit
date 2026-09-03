package branches

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/repo"
)

func noEnv(string) string { return "" }

func testSignature() object.Signature {
	return object.Signature{
		Name:  "Go Git",
		Email: "gogit@example.com",
		When:  time.Unix(1700000000, 0).UTC(),
	}
}

func initTestRepo(t *testing.T, branch string) *repo.Repository {
	t.Helper()
	dir := t.TempDir()
	r, err := repo.Init(filepath.Join(dir, "work"), repo.InitOptions{
		Env:           noEnv,
		NoSystem:      true,
		GlobalFile:    filepath.Join(dir, "absent-global"),
		InitialBranch: branch,
	})
	if err != nil {
		t.Fatalf("Init returned error %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })
	return r
}

func openTestStore(t *testing.T, r *repo.Repository, peeler refs.ObjectPeeler) *refs.Store {
	t.Helper()
	store, err := refs.Open(refs.Options{
		GitDir:    r.GitDir(),
		CommonDir: r.CommonDir(),
		Committer: testSignature,
		Peeler:    peeler,
	})
	if err != nil {
		t.Fatalf("refs.Open returned error %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func oid(t *testing.T, seed string) hash.ObjectID {
	t.Helper()
	if len(seed) > hash.HexSize {
		t.Fatalf("seed %q is longer than %d", seed, hash.HexSize)
	}
	id, err := hash.Parse(seed + strings.Repeat("0", hash.HexSize-len(seed)))
	if err != nil {
		t.Fatalf("hash.Parse(%q) returned error %v", seed, err)
	}
	return id
}

func setRef(t *testing.T, store *refs.Store, name refs.Name, target hash.ObjectID) {
	t.Helper()
	tx := store.Begin()
	if err := tx.Set(name, target); err != nil {
		t.Fatalf("Set(%s) returned error %v", name, err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit(%s) returned error %v", name, err)
	}
}

func detachHead(t *testing.T, store *refs.Store, target hash.ObjectID) {
	t.Helper()
	tx := store.Begin()
	if err := tx.Detach(refs.HEAD, target); err != nil {
		t.Fatalf("Detach(HEAD) returned error %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit(HEAD) returned error %v", err)
	}
}

func setSymbolicRef(t *testing.T, store *refs.Store, name, target refs.Name) {
	t.Helper()
	tx := store.Begin()
	if err := tx.SetSymbolic(name, target); err != nil {
		t.Fatalf("SetSymbolic(%s) returned error %v", name, err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit(%s) returned error %v", name, err)
	}
}

func corruptRef(t *testing.T, r *repo.Repository, rel string) {
	t.Helper()
	full := filepath.Join(r.CommonDir(), filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o777); err != nil {
		t.Fatalf("MkdirAll returned error %v", err)
	}
	if err := os.WriteFile(full, []byte("not-a-hash\n"), 0o666); err != nil {
		t.Fatalf("WriteFile returned error %v", err)
	}
}

type fakePeeler struct {
	tags map[hash.ObjectID]hash.ObjectID
}

func (p fakePeeler) PeelTag(id hash.ObjectID) (hash.ObjectID, bool, error) {
	target, ok := p.tags[id]
	return target, ok, nil
}

func TestLoadReadsCurrentBranchAndHeadID(t *testing.T) {
	r := initTestRepo(t, "main")
	store := openTestStore(t, r, nil)
	setRef(t, store, refs.BranchName("main"), oid(t, "11"))

	snap, err := Load(store)
	if err != nil {
		t.Fatalf("Load returned error %v", err)
	}
	if snap.Detached {
		t.Fatal("Detached = true, want false")
	}
	if snap.Current != "main" {
		t.Fatalf("Current = %q, want main", snap.Current)
	}
	if snap.HeadID != oid(t, "11") {
		t.Fatalf("HeadID = %s, want %s", snap.HeadID, oid(t, "11"))
	}
}

func TestLoadReportsUnbornBranchAsZeroHeadID(t *testing.T) {
	r := initTestRepo(t, "main")
	store := openTestStore(t, r, nil)

	snap, err := Load(store)
	if err != nil {
		t.Fatalf("Load returned error %v", err)
	}
	if snap.Detached {
		t.Fatal("Detached = true, want false")
	}
	if snap.Current != "main" {
		t.Fatalf("Current = %q, want main", snap.Current)
	}
	if !snap.HeadID.IsZero() {
		t.Fatalf("HeadID = %s, want zero", snap.HeadID)
	}
}

func TestLoadReportsDetachedHeadAsHash(t *testing.T) {
	r := initTestRepo(t, "main")
	store := openTestStore(t, r, nil)
	setRef(t, store, refs.BranchName("main"), oid(t, "11"))
	detachHead(t, store, oid(t, "22"))

	snap, err := Load(store)
	if err != nil {
		t.Fatalf("Load returned error %v", err)
	}
	if !snap.Detached {
		t.Fatal("Detached = false, want true")
	}
	if snap.Current != oid(t, "22").String() {
		t.Fatalf("Current = %q, want %s", snap.Current, oid(t, "22"))
	}
	if snap.HeadID != oid(t, "22") {
		t.Fatalf("HeadID = %s, want %s", snap.HeadID, oid(t, "22"))
	}
}

func TestLoadPropagatesMissingHeadAsError(t *testing.T) {
	r := initTestRepo(t, "main")
	store := openTestStore(t, r, nil)
	if err := os.Remove(filepath.Join(r.GitDir(), "HEAD")); err != nil {
		t.Fatalf("Remove(HEAD) returned error %v", err)
	}

	if _, err := Load(store); !errors.Is(err, refs.ErrNotFound) {
		t.Fatalf("Load returned %v, want ErrNotFound", err)
	}
}

func TestLoadPropagatesMalformedTargetBranchAsError(t *testing.T) {
	r := initTestRepo(t, "main")
	store := openTestStore(t, r, nil)
	corruptRef(t, r, "refs/heads/main")

	if _, err := Load(store); !errors.Is(err, refs.ErrMalformedRef) {
		t.Fatalf("Load returned %v, want ErrMalformedRef", err)
	}
}

func TestLoadListsLocalBranchesInByteOrder(t *testing.T) {
	r := initTestRepo(t, "main")
	store := openTestStore(t, r, nil)
	setRef(t, store, refs.BranchName("zzz"), oid(t, "11"))
	setRef(t, store, refs.BranchName("feature/a"), oid(t, "22"))
	setRef(t, store, refs.BranchName("feature/b"), oid(t, "33"))
	setRef(t, store, refs.BranchName("main"), oid(t, "44"))

	snap, err := Load(store)
	if err != nil {
		t.Fatalf("Load returned error %v", err)
	}
	var got []string
	for _, b := range snap.Local {
		got = append(got, string(b.Name))
	}
	want := []string{"refs/heads/feature/a", "refs/heads/feature/b", "refs/heads/main", "refs/heads/zzz"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("Local names = %v, want %v", got, want)
	}
	if snap.Local[0].SymbolicTarget != "" {
		t.Fatalf("Local[0].SymbolicTarget = %q, want empty", snap.Local[0].SymbolicTarget)
	}
}

func TestLoadPropagatesLocalBranchIterationError(t *testing.T) {
	r := initTestRepo(t, "main")
	store := openTestStore(t, r, nil)
	corruptRef(t, r, "refs/heads/broken")

	if _, err := Load(store); !errors.Is(err, refs.ErrMalformedRef) {
		t.Fatalf("Load returned %v, want ErrMalformedRef", err)
	}
}

func TestLoadGroupsRemoteBranchesByRemoteName(t *testing.T) {
	r := initTestRepo(t, "main")
	store := openTestStore(t, r, nil)
	setRef(t, store, refs.RemoteBranchName("origin", "main"), oid(t, "11"))
	setRef(t, store, refs.RemoteBranchName("origin", "feature/x"), oid(t, "22"))
	setRef(t, store, refs.RemoteBranchName("upstream", "dev"), oid(t, "33"))

	snap, err := Load(store)
	if err != nil {
		t.Fatalf("Load returned error %v", err)
	}
	if len(snap.Remotes) != 2 {
		t.Fatalf("Remotes = %d, want 2", len(snap.Remotes))
	}
	if snap.Remotes[0].Name != "origin" || snap.Remotes[1].Name != "upstream" {
		t.Fatalf("Remotes = %+v", snap.Remotes)
	}
	if len(snap.Remotes[0].Branches) != 2 {
		t.Fatalf("origin branches = %d, want 2", len(snap.Remotes[0].Branches))
	}
	if len(snap.Remotes[1].Branches) != 1 {
		t.Fatalf("upstream branches = %d, want 1", len(snap.Remotes[1].Branches))
	}
}

func TestLoadResolvesRemoteHeadSymbolicTarget(t *testing.T) {
	r := initTestRepo(t, "main")
	store := openTestStore(t, r, nil)
	setRef(t, store, refs.RemoteBranchName("origin", "main"), oid(t, "11"))
	setSymbolicRef(t, store, refs.Name("refs/remotes/origin/HEAD"), refs.RemoteBranchName("origin", "main"))

	snap, err := Load(store)
	if err != nil {
		t.Fatalf("Load returned error %v", err)
	}
	if len(snap.Remotes) != 1 {
		t.Fatalf("Remotes = %d, want 1", len(snap.Remotes))
	}
	var head *Branch
	for i, b := range snap.Remotes[0].Branches {
		if b.Name == refs.Name("refs/remotes/origin/HEAD") {
			head = &snap.Remotes[0].Branches[i]
		}
	}
	if head == nil {
		t.Fatal("origin/HEAD missing from Branches")
	}
	if head.SymbolicTarget != refs.RemoteBranchName("origin", "main") {
		t.Fatalf("SymbolicTarget = %q, want origin/main", head.SymbolicTarget)
	}
	if head.Target != oid(t, "11") {
		t.Fatalf("Target = %s, want %s", head.Target, oid(t, "11"))
	}
}

func TestLoadPropagatesRemoteIterationError(t *testing.T) {
	r := initTestRepo(t, "main")
	store := openTestStore(t, r, nil)
	corruptRef(t, r, "refs/remotes/origin/broken")

	if _, err := Load(store); !errors.Is(err, refs.ErrMalformedRef) {
		t.Fatalf("Load returned %v, want ErrMalformedRef", err)
	}
}

func TestLoadPeelsAnnotatedTagsAndLeavesLightweightTagsUnpeeled(t *testing.T) {
	r := initTestRepo(t, "main")
	peeler := fakePeeler{tags: map[hash.ObjectID]hash.ObjectID{oid(t, "aa"): oid(t, "cc")}}
	store := openTestStore(t, r, peeler)
	setRef(t, store, refs.TagName("light"), oid(t, "bb"))
	setRef(t, store, refs.TagName("annotated"), oid(t, "aa"))

	snap, err := Load(store)
	if err != nil {
		t.Fatalf("Load returned error %v", err)
	}
	if len(snap.Tags) != 2 {
		t.Fatalf("Tags = %d, want 2", len(snap.Tags))
	}
	byName := map[string]Tag{}
	for _, tag := range snap.Tags {
		byName[string(tag.Name)] = tag
	}
	light := byName["refs/tags/light"]
	if !light.Peeled.IsZero() {
		t.Fatalf("light tag Peeled = %s, want zero", light.Peeled)
	}
	annotated := byName["refs/tags/annotated"]
	if annotated.Peeled != oid(t, "cc") {
		t.Fatalf("annotated tag Peeled = %s, want %s", annotated.Peeled, oid(t, "cc"))
	}
	if annotated.Target != oid(t, "aa") {
		t.Fatalf("annotated tag Target = %s, want %s", annotated.Target, oid(t, "aa"))
	}
}

func TestLoadPropagatesTagIterationError(t *testing.T) {
	r := initTestRepo(t, "main")
	store := openTestStore(t, r, nil)
	corruptRef(t, r, "refs/tags/broken")

	if _, err := Load(store); !errors.Is(err, refs.ErrMalformedRef) {
		t.Fatalf("Load returned %v, want ErrMalformedRef", err)
	}
}

func TestLoadDetectsStashPresence(t *testing.T) {
	r := initTestRepo(t, "main")
	store := openTestStore(t, r, nil)
	setRef(t, store, refs.Name("refs/stash"), oid(t, "11"))

	snap, err := Load(store)
	if err != nil {
		t.Fatalf("Load returned error %v", err)
	}
	if !snap.HasStash {
		t.Fatal("HasStash = false, want true")
	}
}

func TestLoadReportsNoStashWhenAbsent(t *testing.T) {
	r := initTestRepo(t, "main")
	store := openTestStore(t, r, nil)

	snap, err := Load(store)
	if err != nil {
		t.Fatalf("Load returned error %v", err)
	}
	if snap.HasStash {
		t.Fatal("HasStash = true, want false")
	}
}

func TestLoadPropagatesStashLookupError(t *testing.T) {
	r := initTestRepo(t, "main")
	store := openTestStore(t, r, nil)
	corruptRef(t, r, "refs/stash")

	if _, err := Load(store); !errors.Is(err, refs.ErrMalformedRef) {
		t.Fatalf("Load returned %v, want ErrMalformedRef", err)
	}
}

func TestLoadReadsBranchesAndTagsFromPackedRefs(t *testing.T) {
	r := initTestRepo(t, "main")
	peeler := fakePeeler{tags: map[hash.ObjectID]hash.ObjectID{oid(t, "aa"): oid(t, "cc")}}
	store := openTestStore(t, r, peeler)
	setRef(t, store, refs.BranchName("main"), oid(t, "11"))
	setRef(t, store, refs.TagName("annotated"), oid(t, "aa"))
	if err := store.PackRefs(true); err != nil {
		t.Fatalf("PackRefs returned error %v", err)
	}

	snap, err := Load(store)
	if err != nil {
		t.Fatalf("Load returned error %v", err)
	}
	if len(snap.Local) != 1 || snap.Local[0].Target != oid(t, "11") {
		t.Fatalf("Local = %+v", snap.Local)
	}
	if len(snap.Tags) != 1 || snap.Tags[0].Peeled != oid(t, "cc") {
		t.Fatalf("Tags = %+v", snap.Tags)
	}
}
