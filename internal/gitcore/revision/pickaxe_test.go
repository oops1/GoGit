package revision

import (
	"errors"
	"regexp"
	"slices"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
)

type flakyObjects struct {
	inner   Objects
	fail    hash.ObjectID
	after   int
	fetched int
}

var errFlaky = errors.New("flaky object store")

func (f *flakyObjects) Get(id hash.ObjectID) (object.Type, []byte, error) {
	if id == f.fail {
		f.fetched++
		if f.fetched > f.after {
			return 0, nil, errFlaky
		}
	}
	return f.inner.Get(id)
}

func pickaxeHistory(t *testing.T) *builder {
	t.Helper()
	b := newBuilder(t)
	b.commitFiles("born", map[string]string{"a.txt": "alpha needle\n", "b.txt": "beta\n"})
	b.commitFiles("same count", map[string]string{"a.txt": "alpha needle!\n", "b.txt": "beta\n"}, "born")
	b.commitFiles("doubled", map[string]string{"a.txt": "needle needle\n", "b.txt": "beta\n"}, "same count")
	b.commitFiles("elsewhere", map[string]string{"a.txt": "needle needle\n", "b.txt": "beta needle\n"}, "doubled")
	b.commitFiles("gone", map[string]string{"b.txt": "beta needle\n"}, "elsewhere")
	return b
}

func pickaxeWalk(t *testing.T, b *builder, opts Options) []string {
	t.Helper()
	return collect(t, b, Walk(t.Context(), opts))
}

func TestTheStringPickaxeShowsCommitsThatChangeHowOftenTheStringOccurs(t *testing.T) {
	b := pickaxeHistory(t)
	opts := b.options("gone")
	opts.Pickaxe = "needle"

	got := pickaxeWalk(t, b, opts)

	if want := []string{"gone", "elsewhere", "doubled", "born"}; !slices.Equal(got, want) {
		t.Fatalf("Walk visited %v, want %v", got, want)
	}
}

func TestThePickaxeLooksOnlyAtTheGivenPaths(t *testing.T) {
	b := pickaxeHistory(t)
	opts := b.options("gone")
	opts.Pickaxe = "needle"
	opts.Paths = []string{"b.txt"}

	got := pickaxeWalk(t, b, opts)

	if want := []string{"elsewhere"}; !slices.Equal(got, want) {
		t.Fatalf("Walk visited %v, want %v", got, want)
	}
}

func TestTheRegexpPickaxeShowsCommitsWhoseAddedOrRemovedLinesMatch(t *testing.T) {
	b := newBuilder(t)
	b.commitFiles("start", map[string]string{"a.txt": "keep needle\nother\n"})
	b.commitFiles("context only", map[string]string{"a.txt": "keep needle\nchanged\n"}, "start")
	b.commitFiles("adds", map[string]string{"a.txt": "keep needle\nchanged\nfresh needle\n"}, "context only")
	b.commitFiles("removes", map[string]string{"a.txt": "keep needle\nchanged\n"}, "adds")
	opts := b.options("removes")
	opts.PickaxeRegexp = regexp.MustCompile(`fresh|^keep`)

	got := pickaxeWalk(t, b, opts)

	if want := []string{"removes", "adds", "start"}; !slices.Equal(got, want) {
		t.Fatalf("Walk visited %v, want %v", got, want)
	}
}

func TestThePickaxeSkipsMergesAndPureRenamesAndLeavesBinariesToTheStringSearch(t *testing.T) {
	b := newBuilder(t)
	b.commitFiles("root", map[string]string{"plain.txt": "nothing here\n"})
	b.commitFiles("side", map[string]string{"plain.txt": "nothing here\n", "side.txt": "needle\n"}, "root")
	b.commitFiles("main", map[string]string{"plain.txt": "nothing here\n", "main.txt": "text\n"}, "root")
	b.commitFiles("merge", map[string]string{"plain.txt": "needle in the merge\n", "side.txt": "needle\n", "main.txt": "text\n"}, "main", "side")
	b.commitFiles("binary", map[string]string{"plain.txt": "needle in the merge\n", "side.txt": "needle\n", "main.txt": "text\n", "data.bin": "needle\x00\x01"}, "merge")
	b.commitFiles("renamed", map[string]string{"plain.txt": "needle in the merge\n", "moved.txt": "needle\n", "main.txt": "text\n", "data.bin": "needle\x00\x01"}, "binary")

	byString := b.options("renamed")
	byString.Pickaxe = "needle"
	byRegexp := b.options("renamed")
	byRegexp.PickaxeRegexp = regexp.MustCompile("needle")

	if got, want := pickaxeWalk(t, b, byString), []string{"binary", "side"}; !slices.Equal(got, want) {
		t.Fatalf("the string search visited %v, want %v", got, want)
	}
	if got, want := pickaxeWalk(t, b, byRegexp), []string{"side"}; !slices.Equal(got, want) {
		t.Fatalf("the regexp search visited %v, want %v", got, want)
	}
}

func TestTheStringPickaxeReadsASubmoduleAsTheCommitItPointsAt(t *testing.T) {
	b := newBuilder(t)
	first := hash.SumSHA1("commit", []byte("first submodule commit"))
	second := hash.SumSHA1("commit", []byte("second submodule commit"))
	gitlink := func(name string, target hash.ObjectID, parents ...string) {
		tree := b.objects.put(&object.Tree{Entries: []object.TreeEntry{{Mode: object.ModeSubmodule, Name: "lib", ID: target}}})
		b.clock += 60
		commit := &object.Commit{Tree: tree, Author: b.signature("ann", b.clock), Committer: b.signature("cody", b.clock), Message: name + "\n"}
		for _, parent := range parents {
			commit.Parents = append(commit.Parents, b.id(parent))
		}
		b.ids[name] = b.objects.put(commit)
	}
	gitlink("pinned", first)
	gitlink("bumped", second, "pinned")
	opts := b.options("bumped")
	opts.Pickaxe = second.String()[:12]

	if got, want := pickaxeWalk(t, b, opts), []string{"bumped"}; !slices.Equal(got, want) {
		t.Fatalf("Walk visited %v, want %v", got, want)
	}
}

func TestAShallowCommitIsSearchedAsIfItHadNoParent(t *testing.T) {
	b := pickaxeHistory(t)
	opts := b.options("same count")
	opts.Pickaxe = "needle"
	opts.Context.Shallow = map[hash.ObjectID]struct{}{b.id("same count"): {}}

	got := pickaxeWalk(t, b, opts)

	if want := []string{"same count"}; !slices.Equal(got, want) {
		t.Fatalf("Walk visited %v, want %v", got, want)
	}
}

func TestThePickaxeRefusesAStringAndARegexpAtOnce(t *testing.T) {
	b := pickaxeHistory(t)
	opts := b.options("gone")
	opts.Pickaxe = "needle"
	opts.PickaxeRegexp = regexp.MustCompile("needle")

	for _, err := range Walk(t.Context(), opts) {
		if !errors.Is(err, ErrPickaxeConflict) {
			t.Fatalf("err = %v, want ErrPickaxeConflict", err)
		}
		return
	}
	t.Fatal("Walk yielded nothing")
}

func TestThePickaxeReportsObjectsItCannotRead(t *testing.T) {
	b := pickaxeHistory(t)
	before := b.files["born"]["a.txt"]
	after := b.files["same count"]["a.txt"]
	for _, tt := range []struct {
		name  string
		fail  hash.ObjectID
		after int
		order Order
	}{
		{"a blob the diff needs", b.blob(after), 0, Default},
		{"the old blob to count in", b.blob(before), 1, Default},
		{"the new blob to count in", b.blob(after), 1, Default},
		{"a blob in a buffered walk", b.blob(after), 0, DateOrder},
	} {
		t.Run(tt.name, func(t *testing.T) {
			opts := b.options("same count")
			opts.Order = tt.order
			opts.Pickaxe = "needle"
			opts.Context.Objects = &flakyObjects{inner: b.objects, fail: tt.fail, after: tt.after}

			var failure error
			for _, err := range Walk(t.Context(), opts) {
				if err != nil {
					failure = err
				}
			}

			if !errors.Is(failure, errFlaky) {
				t.Fatalf("err = %v, want the store failure", failure)
			}
		})
	}
}
