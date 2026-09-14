package remote

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/transport"
)

func acceptingPush(t *testing.T, adv transport.Advertisement, updates *[]transport.Update) {
	t.Helper()
	fake := &fakeSession{adv: adv}
	fake.pushFunc = func(_ context.Context, req transport.PushRequest) (*transport.PushResult, error) {
		*updates = req.Updates
		drainPack(t, req)
		statuses := make([]transport.RefStatus, 0, len(req.Updates))
		for _, update := range req.Updates {
			statuses = append(statuses, transport.RefStatus{Name: update.Name, OK: true})
		}
		return &transport.PushResult{UnpackOK: true, Refs: statuses}, nil
	}
	withDial(t, fake, nil)
}

func updateNames(updates []transport.Update) []string {
	names := make([]string, 0, len(updates))
	for _, update := range updates {
		names = append(names, update.Name)
	}
	return names
}

var tagRemote = Remote{Name: "origin", URLs: []string{"fake://x"}}

func TestPushFollowsAnnotatedTagsReachableFromThePushedBranch(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	store := openTestRefs(t, r, db)
	tree := putTree(t, db, object.TreeEntry{Mode: object.ModeBlob, Name: "a.txt", ID: putBlob(t, db, "a\n")})
	first := putCommitWithTree(t, db, testWhen(), "first", tree)
	second := putCommitWithTree(t, db, testWhen(), "second", tree, first)
	unrelated := putCommitWithTree(t, db, testWhen(), "unrelated", tree)
	setLocalBranch(t, store, refs.BranchName("main"), second)
	setLocalBranch(t, store, refs.TagName("v2"), putTag(t, db, "v2", first, testWhen()))
	setLocalBranch(t, store, refs.TagName("v1"), putTag(t, db, "v1", first, testWhen()))
	setLocalBranch(t, store, refs.TagName("light"), first)
	setLocalBranch(t, store, refs.TagName("elsewhere"), putTag(t, db, "elsewhere", unrelated, testWhen()))
	known := putTag(t, db, "known", second, testWhen())
	setLocalBranch(t, store, refs.TagName("known"), known)
	treeTag := &object.Tag{Object: tree, ObjectType: object.TypeTree, Name: "tree", Tagger: &object.Signature{Name: "T", Email: "t@t", When: testWhen()}, Message: "tree"}
	treeTagID, err := db.Put(object.TypeTag, treeTag.Encode())
	if err != nil {
		t.Fatal(err)
	}
	setLocalBranch(t, store, refs.TagName("tree"), treeTagID)
	adv := transport.Advertisement{Refs: []transport.Ref{{Name: "refs/tags/known", ID: known}}}
	specs := mustParseSpecs(t, "refs/heads/main:refs/heads/main")

	var plain []transport.Update
	acceptingPush(t, adv, &plain)
	result, err := Push(t.Context(), r, tagRemote, PushOptions{Refspecs: specs})
	if err != nil || !slices.Equal(updateNames(plain), []string{"refs/heads/main"}) || result.Tags != nil {
		t.Fatalf("push without following tags sent %v, tags %v, err %v", updateNames(plain), result.Tags, err)
	}

	var followed []transport.Update
	acceptingPush(t, adv, &followed)
	result, err = Push(t.Context(), r, tagRemote, PushOptions{Refspecs: specs, FollowTags: true})
	if err != nil {
		t.Fatalf("Push returned error %v", err)
	}
	want := []string{"refs/heads/main", "refs/tags/v1", "refs/tags/v2"}
	if !slices.Equal(updateNames(followed), want) {
		t.Fatalf("push sent %v, want %v", updateNames(followed), want)
	}
	if !slices.Equal(result.Tags, []refs.Name{refs.TagName("v1"), refs.TagName("v2")}) {
		t.Fatalf("result tags = %v", result.Tags)
	}
}

func TestPushFollowsAMissingTagEvenWhenTheBranchIsUpToDate(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	store := openTestRefs(t, r, db)
	tree := putTree(t, db, object.TreeEntry{Mode: object.ModeBlob, Name: "a.txt", ID: putBlob(t, db, "a\n")})
	commit := putCommitWithTree(t, db, testWhen(), "first", tree)
	setLocalBranch(t, store, refs.BranchName("main"), commit)
	setLocalBranch(t, store, refs.TagName("v1"), putTag(t, db, "v1", commit, testWhen()))
	adv := transport.Advertisement{Refs: []transport.Ref{{Name: "refs/heads/main", ID: commit}}}

	var updates []transport.Update
	acceptingPush(t, adv, &updates)
	result, err := Push(t.Context(), r, tagRemote, PushOptions{Refspecs: mustParseSpecs(t, "refs/heads/main:refs/heads/main"), FollowTags: true})

	if err != nil || !slices.Equal(updateNames(updates), []string{"refs/tags/v1"}) || len(result.Changes) != 0 || len(result.Tags) != 1 {
		t.Fatalf("Push = %+v, sent %v, err %v", result, updateNames(updates), err)
	}
}

func TestPushFollowingTagsSkipsTreesAndDeletions(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	store := openTestRefs(t, r, db)
	tree := putTree(t, db, object.TreeEntry{Mode: object.ModeBlob, Name: "a.txt", ID: putBlob(t, db, "a\n")})
	commit := putCommitWithTree(t, db, testWhen(), "first", tree)
	unrelated := putCommitWithTree(t, db, testWhen(), "unrelated", tree)
	setLocalBranch(t, store, refs.BranchName("main"), commit)
	setLocalBranch(t, store, refs.BranchName("tree"), tree)
	setLocalBranch(t, store, refs.TagName("elsewhere"), putTag(t, db, "elsewhere", unrelated, testWhen()))

	var updates []transport.Update
	acceptingPush(t, transport.Advertisement{Refs: []transport.Ref{{Name: "refs/heads/old", ID: unrelated}}}, &updates)
	specs := mustParseSpecs(t, "refs/heads/main:refs/heads/main", "refs/heads/tree:refs/heads/tree", ":refs/heads/old")
	if _, err := Push(t.Context(), r, tagRemote, PushOptions{Refspecs: specs, FollowTags: true}); err != nil {
		t.Fatalf("Push returned error %v", err)
	}
	if !slices.Equal(updateNames(updates), []string{"refs/heads/main", "refs/heads/tree", "refs/heads/old"}) {
		t.Fatalf("push sent %v", updateNames(updates))
	}

	var deletions []transport.Update
	acceptingPush(t, transport.Advertisement{Refs: []transport.Ref{{Name: "refs/heads/old", ID: unrelated}}}, &deletions)
	if _, err := Push(t.Context(), r, tagRemote, PushOptions{Refspecs: mustParseSpecs(t, ":refs/heads/old"), FollowTags: true}); err != nil {
		t.Fatalf("deleting Push returned error %v", err)
	}
	if !slices.Equal(updateNames(deletions), []string{"refs/heads/old"}) {
		t.Fatalf("deleting push sent %v", updateNames(deletions))
	}
}

func TestPushFollowingTagsWithoutTagsSendsOnlyTheBranch(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	store := openTestRefs(t, r, db)
	tree := putTree(t, db, object.TreeEntry{Mode: object.ModeBlob, Name: "a.txt", ID: putBlob(t, db, "a\n")})
	setLocalBranch(t, store, refs.BranchName("main"), putCommitWithTree(t, db, testWhen(), "first", tree))

	var updates []transport.Update
	acceptingPush(t, transport.Advertisement{}, &updates)
	result, err := Push(t.Context(), r, tagRemote, PushOptions{Refspecs: mustParseSpecs(t, "refs/heads/main:refs/heads/main"), FollowTags: true})

	if err != nil || !slices.Equal(updateNames(updates), []string{"refs/heads/main"}) || len(result.Tags) != 0 {
		t.Fatalf("Push = %+v, sent %v, err %v", result, updateNames(updates), err)
	}
}

func TestReachableAmongStopsAtMissingHistory(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	tree := putTree(t, db, object.TreeEntry{Mode: object.ModeBlob, Name: "a.txt", ID: putBlob(t, db, "a\n")})
	shallow := putCommitWithTree(t, db, testWhen(), "shallow", tree, bogusID(t))
	unrelated := putCommitWithTree(t, db, testWhen(), "unrelated", tree)

	base := putCommitWithTree(t, db, testWhen(), "base", tree)
	left := putCommitWithTree(t, db, testWhen(), "left", tree, base)
	right := putCommitWithTree(t, db, testWhen(), "right", tree, base)
	merge := putCommitWithTree(t, db, testWhen(), "merge", tree, left, right)
	if found, err := reachableAmong(db, []hash.ObjectID{merge}, map[hash.ObjectID][]refs.Ref{unrelated: nil}); err != nil || len(found) != 0 {
		t.Fatalf("reachableAmong over a merge = %v, %v", found, err)
	}

	found, err := reachableAmong(db, []hash.ObjectID{shallow}, map[hash.ObjectID][]refs.Ref{unrelated: nil})

	if err != nil || len(found) != 0 {
		t.Fatalf("reachableAmong = %v, %v", found, err)
	}
}

func TestPushFollowingTagsReportsCorruptObjectsAndRefs(t *testing.T) {
	cases := map[string]func(t *testing.T, r string, db interface {
		Put(object.Type, []byte) (hash.ObjectID, error)
	}, store *refs.Store, commit hash.ObjectID){
		"corrupt tag": func(t *testing.T, _ string, db interface {
			Put(object.Type, []byte) (hash.ObjectID, error)
		}, store *refs.Store, _ hash.ObjectID) {
			id, err := db.Put(object.TypeTag, []byte("garbage"))
			if err != nil {
				t.Fatal(err)
			}
			setLocalBranch(t, store, refs.TagName("bad"), id)
		},
		"corrupt history": func(t *testing.T, _ string, db interface {
			Put(object.Type, []byte) (hash.ObjectID, error)
		}, store *refs.Store, commit hash.ObjectID) {
			id, err := db.Put(object.TypeCommit, []byte("junk"))
			if err != nil {
				t.Fatal(err)
			}
			setLocalBranch(t, store, refs.BranchName("main"), id)
			setLocalBranch(t, store, refs.TagName("v1"), commit)
		},
		"dangling tag": func(t *testing.T, _ string, _ interface {
			Put(object.Type, []byte) (hash.ObjectID, error)
		}, store *refs.Store, _ hash.ObjectID) {
			setLocalBranch(t, store, refs.TagName("gone"), bogusID(t))
		},
		"missing tip": func(t *testing.T, _ string, _ interface {
			Put(object.Type, []byte) (hash.ObjectID, error)
		}, store *refs.Store, _ hash.ObjectID) {
			setLocalBranch(t, store, refs.BranchName("main"), bogusID(t))
		},
		"broken tag ref": func(t *testing.T, gitDir string, _ interface {
			Put(object.Type, []byte) (hash.ObjectID, error)
		}, _ *refs.Store, _ hash.ObjectID) {
			if err := os.WriteFile(filepath.Join(gitDir, "refs", "tags", "broken"), []byte("not a hash\n"), 0o666); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			r := newTestRepo(t, "")
			db := openTestODB(t, r)
			store := openTestRefs(t, r, db)
			tree := putTree(t, db, object.TreeEntry{Mode: object.ModeBlob, Name: "a.txt", ID: putBlob(t, db, "a\n")})
			commit := putCommitWithTree(t, db, testWhen(), "first", tree)
			setLocalBranch(t, store, refs.BranchName("main"), commit)
			setLocalBranch(t, store, refs.TagName("v1"), putTag(t, db, "v1", putCommitWithTree(t, db, testWhen(), "other", tree), testWhen()))
			setup(t, r.GitDir(), db, store, putTag(t, db, "v0", commit, testWhen()))

			var updates []transport.Update
			acceptingPush(t, transport.Advertisement{}, &updates)
			if _, err := Push(t.Context(), r, tagRemote, PushOptions{Refspecs: mustParseSpecs(t, "refs/heads/main:refs/heads/main"), FollowTags: true}); err == nil {
				t.Fatal("Push ignored a corrupt object or ref")
			}
		})
	}
}

func bogusID(t *testing.T) hash.ObjectID {
	t.Helper()
	id, err := hash.Sum(hash.SHA1, "commit", []byte("this object was never stored"))
	if err != nil {
		t.Fatal(err)
	}
	return id
}
