package branches

import (
	"errors"
	"strconv"
	"testing"
	"time"

	gitconfig "github.com/oops1/gogit/internal/gitcore/config"
	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/refs"
)

type fakeObjects struct {
	kinds map[hash.ObjectID]object.Type
	bytes map[hash.ObjectID][]byte
	reads int
}

var errNoSuchObject = errors.New("no such object")

func (f *fakeObjects) Get(id hash.ObjectID) (object.Type, []byte, error) {
	f.reads++
	data, ok := f.bytes[id]
	if !ok {
		return 0, nil, errNoSuchObject
	}
	return f.kinds[id], data, nil
}

func commitBytes(when time.Time, message string) []byte {
	stamp := strconv.FormatInt(when.Unix(), 10) + " +0000"
	return []byte("tree " + "4b825dc642cb6eb9a060e54bf8d69288fbee4904" + "\n" +
		"author A <a@example.com> " + stamp + "\n" +
		"committer A <a@example.com> " + stamp + "\n\n" + message + "\n")
}

func objectsFor(t *testing.T, when map[string]time.Time) *fakeObjects {
	t.Helper()
	out := &fakeObjects{kinds: map[hash.ObjectID]object.Type{}, bytes: map[hash.ObjectID][]byte{}}
	for seed, moment := range when {
		id := oid(t, seed)
		out.kinds[id] = object.TypeCommit
		out.bytes[id] = commitBytes(moment, "commit "+seed)
	}
	return out
}

func TestLoadTimesFillsBranchesRemotesAndTags(t *testing.T) {
	snap := Snapshot{
		Local:   []Branch{{Name: refs.BranchName("main"), Target: oid(t, "11")}},
		Remotes: []Remote{{Name: "origin", Branches: []Branch{{Name: refs.RemoteBranchName("origin", "main"), Target: oid(t, "22")}}}},
		Tags: []Tag{
			{Name: refs.TagName("plain"), Target: oid(t, "33")},
			{Name: refs.TagName("annotated"), Target: oid(t, "44"), Peeled: oid(t, "11")},
		},
	}
	src := objectsFor(t, map[string]time.Time{"11": at(1), "22": at(2), "33": at(3)})

	LoadTimes(src, &snap)
	if !snap.Local[0].When.Equal(at(1)) {
		t.Fatalf("local time = %v, want %v", snap.Local[0].When, at(1))
	}
	if !snap.Remotes[0].Branches[0].When.Equal(at(2)) {
		t.Fatalf("remote time = %v, want %v", snap.Remotes[0].Branches[0].When, at(2))
	}
	if !snap.Tags[0].When.Equal(at(3)) {
		t.Fatalf("tag time = %v, want %v", snap.Tags[0].When, at(3))
	}
	if !snap.Tags[1].When.Equal(at(1)) {
		t.Fatalf("annotated tag must take the time of the commit it peels to, got %v", snap.Tags[1].When)
	}
	if src.reads != 3 {
		t.Fatalf("object reads = %d, want one per distinct commit", src.reads)
	}
}

func TestLoadTimesLeavesUnreadableAndNonCommitTargetsAtZero(t *testing.T) {
	snap := Snapshot{Local: []Branch{
		{Name: refs.BranchName("missing"), Target: oid(t, "11")},
		{Name: refs.BranchName("blob"), Target: oid(t, "22")},
		{Name: refs.BranchName("broken"), Target: oid(t, "33")},
	}}
	src := &fakeObjects{
		kinds: map[hash.ObjectID]object.Type{oid(t, "22"): object.TypeBlob, oid(t, "33"): object.TypeCommit},
		bytes: map[hash.ObjectID][]byte{oid(t, "22"): []byte("x"), oid(t, "33"): []byte("not a commit")},
	}

	LoadTimes(src, &snap)
	for _, b := range snap.Local {
		if !b.When.IsZero() {
			t.Fatalf("%s time = %v, want zero", b.Name, b.When)
		}
	}
}

type fakeBranchConfig map[string]gitconfig.Branch

func (f fakeBranchConfig) Branch(name string) (gitconfig.Branch, bool) {
	branch, ok := f[name]
	return branch, ok
}

func TestLoadUpstreamsTakesTheTrackedRemoteBranch(t *testing.T) {
	snap := Snapshot{Local: []Branch{
		{Name: refs.BranchName("main")},
		{Name: refs.BranchName("no-entry")},
		{Name: refs.BranchName("no-remote")},
		{Name: refs.BranchName("no-merge")},
	}}
	src := fakeBranchConfig{
		"main":      {Name: "main", Remote: "origin", Merge: []string{"refs/heads/main"}},
		"no-remote": {Name: "no-remote", Merge: []string{"refs/heads/x"}},
		"no-merge":  {Name: "no-merge", Remote: "origin"},
	}

	LoadUpstreams(src, &snap)
	if got := snap.Local[0].Upstream; got != refs.RemoteBranchName("origin", "main") {
		t.Fatalf("upstream = %q, want refs/remotes/origin/main", got)
	}
	for _, b := range snap.Local[1:] {
		if b.Upstream != "" {
			t.Fatalf("%s upstream = %q, want none", b.Name, b.Upstream)
		}
	}
}
