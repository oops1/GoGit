package remote

import (
	"context"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/transport"
)

func TestClassifyPushTargetsSkipsZeroIDs(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	out, err := classifyPushTargets(db, []hash.ObjectID{hash.Zero})
	if err != nil {
		t.Fatalf("classifyPushTargets returned error %v", err)
	}
	if len(out.commits)+len(out.tags)+len(out.trees)+len(out.blobs) != 0 {
		t.Fatalf("classifyPushTargets returned %+v, want everything empty", out)
	}
}

func TestClassifyPushTargetsPropagatesTypeErrors(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	unknown := hash.SumSHA1("commit", []byte("unknown"))
	if _, err := classifyPushTargets(db, []hash.ObjectID{unknown}); err == nil {
		t.Fatal("classifyPushTargets returned no error for an unknown object")
	}
}

func TestClassifyPushTargetsPropagatesDanglingTagErrors(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	unknown := hash.SumSHA1("commit", []byte("unknown"))
	tag := putTag(t, db, "v1", unknown, testWhen())
	if _, err := classifyPushTargets(db, []hash.ObjectID{tag}); err == nil {
		t.Fatal("classifyPushTargets returned no error for a tag pointing at a missing object")
	}
}

func TestClassifyPushTargetsResolvesATagToATree(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	tree := putTree(t, db)
	tag := putTag(t, db, "v1", tree, testWhen())
	out, err := classifyPushTargets(db, []hash.ObjectID{tag})
	if err != nil {
		t.Fatalf("classifyPushTargets returned error %v", err)
	}
	if len(out.tags) != 1 || out.tags[0] != tag {
		t.Fatalf("classifyPushTargets returned tags %v", out.tags)
	}
	if len(out.trees) != 1 || out.trees[0] != tree {
		t.Fatalf("classifyPushTargets returned trees %v", out.trees)
	}
}

func TestClassifyPushTargetsResolvesATagToABlob(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	blob := putBlob(t, db, "content")
	tag := putTag(t, db, "v1", blob, testWhen())
	out, err := classifyPushTargets(db, []hash.ObjectID{tag})
	if err != nil {
		t.Fatalf("classifyPushTargets returned error %v", err)
	}
	if len(out.blobs) != 1 || out.blobs[0] != blob {
		t.Fatalf("classifyPushTargets returned blobs %v", out.blobs)
	}
}

func TestKnownLocallySkipsZeroAndUnknownIDs(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	known := putBlob(t, db, "known")
	unknown := hash.SumSHA1("blob", []byte("unknown"))
	got := knownLocally(db, []hash.ObjectID{hash.Zero, unknown, known})
	if len(got) != 1 || got[0] != known {
		t.Fatalf("knownLocally returned %v, want only %s", got, known)
	}
}

func TestCollectPushObjectsMarksADirectHaveBlobAsThin(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	blob := putBlob(t, db, "have-blob")
	next := putCommitWithTree(t, db, testWhen(), "next", putTree(t, db))

	ids, thin, err := collectPushObjects(t.Context(), db, []hash.ObjectID{blob}, []hash.ObjectID{next}, nil)
	if err != nil {
		t.Fatalf("collectPushObjects returned error %v", err)
	}
	if containsID(ids, blob) {
		t.Fatalf("collectPushObjects returned %v, want the have blob excluded", ids)
	}
	if _, ok := thin[blob]; !ok {
		t.Fatalf("collectPushObjects thin bases %v, want the have blob included", thin)
	}
}

func TestCollectPushObjectsMarksAHaveTagAsVisited(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	base := putCommitWithTree(t, db, testWhen(), "base", putTree(t, db))
	tag := putTag(t, db, "v1", base, testWhen())
	next := putCommitWithTree(t, db, testWhen(), "next", putTree(t, db), base)

	ids, _, err := collectPushObjects(t.Context(), db, []hash.ObjectID{tag}, []hash.ObjectID{next}, nil)
	if err != nil {
		t.Fatalf("collectPushObjects returned error %v", err)
	}
	if containsID(ids, base) {
		t.Fatalf("collectPushObjects returned %v, want the tagged base excluded", ids)
	}
	if !containsID(ids, next) {
		t.Fatalf("collectPushObjects returned %v, missing the new commit", ids)
	}
}

func TestCollectPushObjectsPropagatesAMissingHaveTree(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	missingTree := hash.SumSHA1("tree", []byte("missing"))
	have := putCommitWithTree(t, db, testWhen(), "have", missingTree)
	next := putCommitWithTree(t, db, testWhen(), "next", putTree(t, db))
	if _, _, err := collectPushObjects(t.Context(), db, []hash.ObjectID{have}, []hash.ObjectID{next}, nil); err == nil {
		t.Fatal("collectPushObjects returned no error for a have-side commit with a missing tree")
	}
}

func TestCollectPushObjectsPropagatesAMissingNewTree(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	missingTree := hash.SumSHA1("tree", []byte("missing"))
	next := putCommitWithTree(t, db, testWhen(), "next", missingTree)
	if _, _, err := collectPushObjects(t.Context(), db, nil, []hash.ObjectID{next}, nil); err == nil {
		t.Fatal("collectPushObjects returned no error for a new commit with a missing tree")
	}
}

func TestCollectPushObjectsSkipsSubmoduleEntriesOnBothSides(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	submoduleCommit := hash.SumSHA1("commit", []byte("submodule"))
	haveTree := putTree(t, db, object.TreeEntry{Mode: object.ModeSubmodule, Name: "sub", ID: submoduleCommit})
	haveCommit := putCommitWithTree(t, db, testWhen(), "have", haveTree)
	newBlob := putBlob(t, db, "distinguishes the new tree from the have tree")
	newTree := putTree(t, db,
		object.TreeEntry{Mode: object.ModeSubmodule, Name: "sub", ID: submoduleCommit},
		object.TreeEntry{Mode: object.ModeBlob, Name: "f", ID: newBlob})
	newCommit := putCommitWithTree(t, db, testWhen(), "next", newTree, haveCommit)

	ids, _, err := collectPushObjects(t.Context(), db, []hash.ObjectID{haveCommit}, []hash.ObjectID{newCommit}, nil)
	if err != nil {
		t.Fatalf("collectPushObjects returned error %v", err)
	}
	if containsID(ids, submoduleCommit) {
		t.Fatalf("collectPushObjects returned %v, want the submodule commit excluded", ids)
	}
	if !containsID(ids, newTree) || !containsID(ids, newBlob) {
		t.Fatalf("collectPushObjects returned %v, want the new tree and blob walked", ids)
	}
}

func TestCollectPushObjectsWalksNestedSubtreesOnBothSides(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	sharedBlob := putBlob(t, db, "shared nested content")
	sharedInner := putTree(t, db, object.TreeEntry{Mode: object.ModeBlob, Name: "f", ID: sharedBlob})
	sharedOuter := putTree(t, db, object.TreeEntry{Mode: object.ModeTree, Name: "dir", ID: sharedInner})
	have := putCommitWithTree(t, db, testWhen(), "have", sharedOuter)

	newBlob := putBlob(t, db, "new nested content")
	newInner := putTree(t, db, object.TreeEntry{Mode: object.ModeBlob, Name: "g", ID: newBlob})
	newOuter := putTree(t, db, object.TreeEntry{Mode: object.ModeTree, Name: "dir2", ID: newInner})
	next := putCommitWithTree(t, db, testWhen(), "next", newOuter, have)

	ids, thin, err := collectPushObjects(t.Context(), db, []hash.ObjectID{have}, []hash.ObjectID{next}, nil)
	if err != nil {
		t.Fatalf("collectPushObjects returned error %v", err)
	}
	for _, want := range []hash.ObjectID{next, newOuter, newInner, newBlob} {
		if !containsID(ids, want) {
			t.Fatalf("collectPushObjects returned %v, missing %s", ids, want)
		}
	}
	for _, want := range []hash.ObjectID{sharedOuter, sharedInner, sharedBlob} {
		if _, ok := thin[want]; !ok {
			t.Fatalf("collectPushObjects thin bases %v, missing %s", thin, want)
		}
	}
}

func TestCollectPushObjectsPropagatesANewTagsClassificationError(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	unknown := hash.SumSHA1("commit", []byte("unknown"))
	if _, _, err := collectPushObjects(t.Context(), db, nil, []hash.ObjectID{unknown}, nil); err == nil {
		t.Fatal("collectPushObjects returned no error for an unresolvable new object")
	}
}

func TestCollectPushObjectsHandlesADirectNewTreeAndBlob(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	blob := putBlob(t, db, "direct blob")
	tree := putTree(t, db, object.TreeEntry{Mode: object.ModeBlob, Name: "f", ID: blob})
	ids, _, err := collectPushObjects(t.Context(), db, nil, []hash.ObjectID{tree}, nil)
	if err != nil {
		t.Fatalf("collectPushObjects returned error %v", err)
	}
	if !containsID(ids, tree) || !containsID(ids, blob) {
		t.Fatalf("collectPushObjects returned %v, want the tree and blob", ids)
	}
}

func TestCollectPushObjectsPropagatesAnUndecodableHaveCommit(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	broken, err := db.Put(object.TypeCommit, []byte("not a real commit"))
	if err != nil {
		t.Fatalf("db.Put returned error %v", err)
	}
	next := putCommitWithTree(t, db, testWhen(), "next", putTree(t, db))
	if _, _, err := collectPushObjects(t.Context(), db, []hash.ObjectID{broken}, []hash.ObjectID{next}, nil); err == nil {
		t.Fatal("collectPushObjects returned no error for an undecodable have-side commit")
	}
}

func TestCollectPushObjectsHandlesADirectNewBlob(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	blob := putBlob(t, db, "direct new blob with no enclosing tree")
	ids, _, err := collectPushObjects(t.Context(), db, nil, []hash.ObjectID{blob}, nil)
	if err != nil {
		t.Fatalf("collectPushObjects returned error %v", err)
	}
	if !containsID(ids, blob) {
		t.Fatalf("collectPushObjects returned %v, want the direct blob", ids)
	}
}

func TestCollectPushObjectsPropagatesAMissingNestedNewSubtree(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	missingSubtree := hash.SumSHA1("tree", []byte("missing-subtree"))
	outer := putTree(t, db, object.TreeEntry{Mode: object.ModeTree, Name: "dir", ID: missingSubtree})
	if _, _, err := collectPushObjects(t.Context(), db, nil, []hash.ObjectID{outer}, nil); err == nil {
		t.Fatal("collectPushObjects returned no error for a new tree with a missing nested subtree")
	}
}

func TestCollectPushObjectsPropagatesAMissingNestedHaveSubtree(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	missingSubtree := hash.SumSHA1("tree", []byte("missing-have-subtree"))
	outer := putTree(t, db, object.TreeEntry{Mode: object.ModeTree, Name: "dir", ID: missingSubtree})
	next := putCommitWithTree(t, db, testWhen(), "next", putTree(t, db))
	if _, _, err := collectPushObjects(t.Context(), db, []hash.ObjectID{outer}, []hash.ObjectID{next}, nil); err == nil {
		t.Fatal("collectPushObjects returned no error for a have tree with a missing nested subtree")
	}
}

func TestHaveTreeStopsOnACancelledContext(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	tree := putTree(t, db, object.TreeEntry{Mode: object.ModeBlob, Name: "f", ID: putBlob(t, db, "content")})

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	collector := newPushCollector(db, nil)
	if err := collector.haveTree(ctx, tree); err == nil {
		t.Fatal("haveTree returned no error for a cancelled context")
	}
}

func TestIncludeTreeStopsOnACancelledContext(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	tree := putTree(t, db, object.TreeEntry{Mode: object.ModeBlob, Name: "f", ID: putBlob(t, db, "content")})

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	collector := newPushCollector(db, nil)
	if err := collector.includeTree(ctx, tree); err == nil {
		t.Fatal("includeTree returned no error for a cancelled context")
	}
}

func TestGatherHaveIDsIncludesPeeledTags(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	store := openTestRefs(t, r, db)
	rem := Remote{Name: "origin"}
	tagID := hash.SumSHA1("tag", []byte("tag"))
	commitID := hash.SumSHA1("commit", []byte("commit"))
	adv := transport.Advertisement{Refs: []transport.Ref{{Name: "refs/tags/v1", ID: tagID, Peeled: commitID}}}
	ids, err := gatherHaveIDs(adv, store, rem)
	if err != nil {
		t.Fatalf("gatherHaveIDs returned error %v", err)
	}
	if !containsID(ids, tagID) || !containsID(ids, commitID) {
		t.Fatalf("gatherHaveIDs returned %v, want both the tag and its peeled commit", ids)
	}
}

func TestPushPlanErrorPropagatesFromLookupCurrent(t *testing.T) {
	r := newTestRepo(t, "")
	mustWriteFile(t, r.CommonPath("refs/heads/main"), "not-a-valid-ref\n")
	db := openTestODB(t, r)
	store := openTestRefs(t, r, db)
	planner := newPushPlanner(store, db, Remote{Name: "origin"}, PushOptions{}, transport.Advertisement{})
	if err := planner.plan(mustParseSpecs(t, "refs/heads/main:refs/heads/main")); err == nil {
		t.Fatal("plan returned no error for a malformed local ref")
	}
}
