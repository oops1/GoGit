package blame

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
)

type store struct {
	objects map[hash.ObjectID][]byte
	kinds   map[hash.ObjectID]object.Type
	fail    map[hash.ObjectID]error
}

var errInjected = errors.New("injected")

func newStore() *store {
	return &store{objects: map[hash.ObjectID][]byte{}, kinds: map[hash.ObjectID]object.Type{}, fail: map[hash.ObjectID]error{}}
}

func (s *store) Get(id hash.ObjectID) (object.Type, []byte, error) {
	if err := s.fail[id]; err != nil {
		return 0, nil, err
	}
	data, known := s.objects[id]
	if !known {
		return 0, nil, errors.New("blame: no such object " + id.String())
	}
	return s.kinds[id], data, nil
}

func (s *store) put(o object.Object) hash.ObjectID {
	id := hash.SumSHA1(o.Type().String(), o.Encode())
	s.objects[id], s.kinds[id] = o.Encode(), o.Type()
	return id
}

func (s *store) blob(text string) hash.ObjectID {
	id := hash.SumSHA1(object.TypeBlob.String(), []byte(text))
	s.objects[id], s.kinds[id] = []byte(text), object.TypeBlob
	return id
}

func (s *store) tree(files map[string]hash.ObjectID) hash.ObjectID {
	tree := &object.Tree{}
	for name, id := range files {
		tree.Entries = append(tree.Entries, object.TreeEntry{Mode: object.ModeBlob, Name: name, ID: id})
	}
	sortEntries(tree)
	return s.put(tree)
}

func sortEntries(tree *object.Tree) {
	for i := 1; i < len(tree.Entries); i++ {
		for j := i; j > 0 && tree.Entries[j-1].Name > tree.Entries[j].Name; j-- {
			tree.Entries[j-1], tree.Entries[j] = tree.Entries[j], tree.Entries[j-1]
		}
	}
}

func (s *store) commit(message string, tree hash.ObjectID, when int64, parents ...hash.ObjectID) hash.ObjectID {
	who := object.Signature{Name: "ann", Email: "ann@example.com", When: time.Unix(when, 0).UTC()}
	return s.put(&object.Commit{Tree: tree, Parents: parents, Author: who, Committer: who, Message: message})
}

func blamedOn(t *testing.T, result Result) []string {
	t.Helper()
	var out []string
	for _, line := range result.Lines {
		out = append(out, line.Summary+":"+strconv.Itoa(line.Source))
	}
	return out
}

func TestABlameOfTheFirstCommitGivesItEveryLine(t *testing.T) {
	s := newStore()
	tree := s.tree(map[string]hash.ObjectID{"f": s.blob("one\ntwo\n")})
	head := s.commit("base", tree, 1000)

	result, err := File(t.Context(), s, head, "f", Options{})

	if err != nil {
		t.Fatalf("File returned error %v", err)
	}
	if got := blamedOn(t, result); strings.Join(got, " ") != "base:1 base:2" {
		t.Fatalf("blame = %v", got)
	}
	if result.Path != "f" || result.Lines[1].Text != "two\n" {
		t.Fatalf("result = %+v", result)
	}
}

func TestABlameSplitsTheLinesBetweenCommits(t *testing.T) {
	s := newStore()
	first := s.commit("base", s.tree(map[string]hash.ObjectID{"f": s.blob("one\ntwo\n")}), 1000)
	second := s.commit("edit", s.tree(map[string]hash.ObjectID{"f": s.blob("one\nchanged\nthree\n")}), 2000, first)

	result, err := File(t.Context(), s, second, "f", Options{})

	if err != nil {
		t.Fatalf("File returned error %v", err)
	}
	if got := blamedOn(t, result); strings.Join(got, " ") != "base:1 edit:2 edit:3" {
		t.Fatalf("blame = %v", got)
	}
}

func TestABlameOfAnEmptyFileHasNoLines(t *testing.T) {
	s := newStore()
	head := s.commit("base", s.tree(map[string]hash.ObjectID{"f": s.blob("")}), 1000)

	result, err := File(t.Context(), s, head, "f", Options{})

	if err != nil || len(result.Lines) != 0 || result.Path != "f" {
		t.Fatalf("result = %+v, %v", result, err)
	}
}

func TestABlameOfAMissingPathSaysSo(t *testing.T) {
	s := newStore()
	head := s.commit("base", s.tree(map[string]hash.ObjectID{"f": s.blob("one\n")}), 1000)

	for _, path := range []string{"nope", "f/deeper", "dir/f"} {
		if _, err := File(t.Context(), s, head, path, Options{}); !errors.Is(err, ErrPathNotFound) {
			t.Errorf("%s: err = %v", path, err)
		}
	}
}

func TestABlameOfAPathThatIsATreeSaysSo(t *testing.T) {
	s := newStore()
	inner := s.tree(map[string]hash.ObjectID{"f": s.blob("one\n")})
	outer := s.put(&object.Tree{Entries: []object.TreeEntry{{Mode: object.ModeTree, Name: "dir", ID: inner}}})
	head := s.commit("base", outer, 1000)

	if _, err := File(t.Context(), s, head, "dir", Options{}); !errors.Is(err, ErrPathNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestABlameNeedsACommit(t *testing.T) {
	s := newStore()
	blob := s.blob("one\n")

	if _, err := File(t.Context(), s, blob, "f", Options{}); err == nil {
		t.Fatal("a blob was taken for a commit")
	}
}

func TestABlameReportsAnUnreadableObject(t *testing.T) {
	s := newStore()
	tree := s.tree(map[string]hash.ObjectID{"f": s.blob("one\n")})
	head := s.commit("base", tree, 1000)
	s.fail[tree] = errInjected

	if _, err := File(t.Context(), s, head, "f", Options{}); !errors.Is(err, errInjected) {
		t.Fatalf("err = %v", err)
	}
}

func TestABlameReportsACommitItCannotParse(t *testing.T) {
	s := newStore()
	broken := hash.SumSHA1(object.TypeCommit.String(), []byte("nonsense\n"))
	s.objects[broken], s.kinds[broken] = []byte("nonsense\n"), object.TypeCommit

	if _, err := File(t.Context(), s, broken, "f", Options{}); err == nil {
		t.Fatal("a broken commit was accepted")
	}
}

func TestABlameReportsATreeItCannotParse(t *testing.T) {
	s := newStore()
	broken := hash.SumSHA1(object.TypeTree.String(), []byte("nonsense"))
	s.objects[broken], s.kinds[broken] = []byte("nonsense"), object.TypeTree
	head := s.commit("base", broken, 1000)

	if _, err := File(t.Context(), s, head, "f", Options{}); err == nil {
		t.Fatal("a broken tree was accepted")
	}
}

func TestABlameStopsWhenTheContextIsCancelled(t *testing.T) {
	s := newStore()
	head := s.commit("base", s.tree(map[string]hash.ObjectID{"f": s.blob("one\n")}), 1000)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, err := File(ctx, s, head, "f", Options{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}

func TestWithoutFollowingRenamesTheMoveTakesTheBlame(t *testing.T) {
	s := newStore()
	text := "one\ntwo\nthree\n"
	first := s.commit("base", s.tree(map[string]hash.ObjectID{"f": s.blob(text)}), 1000)
	second := s.commit("move", s.tree(map[string]hash.ObjectID{"moved": s.blob(text)}), 2000, first)

	plain, err := File(t.Context(), s, second, "moved", Options{})
	if err != nil {
		t.Fatalf("File returned error %v", err)
	}
	followed, err := File(t.Context(), s, second, "moved", Options{FollowRenames: true})
	if err != nil {
		t.Fatalf("File returned error %v", err)
	}

	if got := blamedOn(t, plain); strings.Join(got, " ") != "move:1 move:2 move:3" {
		t.Fatalf("without following = %v", got)
	}
	if got := blamedOn(t, followed); strings.Join(got, " ") != "base:1 base:2 base:3" {
		t.Fatalf("following = %v", got)
	}
	if followed.Lines[0].Path != "f" {
		t.Fatalf("path = %q", followed.Lines[0].Path)
	}
}

func TestAMergeKeepsTheBlameOfEachSide(t *testing.T) {
	s := newStore()
	base := s.commit("base", s.tree(map[string]hash.ObjectID{"f": s.blob("one\ntwo\nthree\n")}), 1000)
	ours := s.commit("ours", s.tree(map[string]hash.ObjectID{"f": s.blob("OURS\ntwo\nthree\n")}), 2000, base)
	theirs := s.commit("theirs", s.tree(map[string]hash.ObjectID{"f": s.blob("one\ntwo\nTHEIRS\n")}), 2000, base)
	merged := s.commit("merge", s.tree(map[string]hash.ObjectID{"f": s.blob("OURS\ntwo\nTHEIRS\n")}), 3000, ours, theirs)

	result, err := File(t.Context(), s, merged, "f", Options{})

	if err != nil {
		t.Fatalf("File returned error %v", err)
	}
	if got := blamedOn(t, result); strings.Join(got, " ") != "ours:1 base:2 theirs:3" {
		t.Fatalf("blame = %v", got)
	}
}

func TestAMergeThatAddedTheFileOnOneSideOnly(t *testing.T) {
	s := newStore()
	base := s.commit("base", s.tree(map[string]hash.ObjectID{"keep": s.blob("keep\n")}), 1000)
	added := s.commit("add", s.tree(map[string]hash.ObjectID{"keep": s.blob("keep\n"), "f": s.blob("one\n")}), 2000, base)
	merged := s.commit("merge", s.tree(map[string]hash.ObjectID{"keep": s.blob("keep\n"), "f": s.blob("one\n")}), 3000, base, added)

	result, err := File(t.Context(), s, merged, "f", Options{})

	if err != nil {
		t.Fatalf("File returned error %v", err)
	}
	if got := blamedOn(t, result); strings.Join(got, " ") != "add:1" {
		t.Fatalf("blame = %v", got)
	}
}

func TestABlameMeetsTheSameCommitOnlyOnce(t *testing.T) {
	s := newStore()
	text := "one\ntwo\nthree\nfour\n"
	base := s.commit("base", s.tree(map[string]hash.ObjectID{"f": s.blob(text)}), 1000)
	left := s.commit("left", s.tree(map[string]hash.ObjectID{"f": s.blob("LEFT\ntwo\nthree\nfour\n")}), 2000, base)
	right := s.commit("right", s.tree(map[string]hash.ObjectID{"f": s.blob("one\ntwo\nthree\nRIGHT\n")}), 2000, base)
	merged := s.commit("merge", s.tree(map[string]hash.ObjectID{"f": s.blob("LEFT\ntwo\nthree\nRIGHT\n")}), 3000, left, right)
	top := s.commit("top", s.tree(map[string]hash.ObjectID{"f": s.blob("LEFT\nTOP\nthree\nRIGHT\n")}), 4000, merged)

	result, err := File(t.Context(), s, top, "f", Options{})

	if err != nil {
		t.Fatalf("File returned error %v", err)
	}
	if got := blamedOn(t, result); strings.Join(got, " ") != "left:1 top:2 base:3 right:4" {
		t.Fatalf("blame = %v", got)
	}
}

func TestASummaryIsTheFirstLineOfTheMessage(t *testing.T) {
	if got := summaryOf("\n\nsubject\n\nbody\n"); got != "subject" {
		t.Fatalf("summary = %q", got)
	}
	if got := summaryOf("only\n"); got != "only" {
		t.Fatalf("summary = %q", got)
	}
}

func TestSpansGrowWhenTheyTouch(t *testing.T) {
	spans := add(nil, span{result: 1, source: 1, count: 1})
	spans = add(spans, span{result: 2, source: 2, count: 1})
	spans = add(spans, span{result: 4, source: 4, count: 1})

	if len(spans) != 2 || spans[0].count != 2 || spans[1].result != 4 {
		t.Fatalf("spans = %+v", spans)
	}
}

func TestABlameMergesTheSpansThatMeetAtTheSameCommit(t *testing.T) {
	s := newStore()
	text := "one\ntwo\nthree\nfour\n"
	base := s.commit("base", s.tree(map[string]hash.ObjectID{"f": s.blob(text)}), 1000)
	left := s.commit("left", s.tree(map[string]hash.ObjectID{"f": s.blob(text)}), 2000, base)
	right := s.commit("right", s.tree(map[string]hash.ObjectID{"f": s.blob(text)}), 2500, base)
	merged := s.commit("merge", s.tree(map[string]hash.ObjectID{"f": s.blob(text)}), 3000, left, right)

	result, err := File(t.Context(), s, merged, "f", Options{})

	if err != nil {
		t.Fatalf("File returned error %v", err)
	}
	if got := blamedOn(t, result); strings.Join(got, " ") != "base:1 base:2 base:3 base:4" {
		t.Fatalf("blame = %v", got)
	}
}

func TestAMergeStopsAskingOnceEveryLineIsExplained(t *testing.T) {
	s := newStore()
	text := "one\ntwo\n"
	base := s.commit("base", s.tree(map[string]hash.ObjectID{"f": s.blob(text)}), 1000)
	ours := s.commit("ours", s.tree(map[string]hash.ObjectID{"f": s.blob(text)}), 2000, base)
	theirs := s.commit("theirs", s.tree(map[string]hash.ObjectID{"f": s.blob("other\n")}), 2000, base)
	merged := s.commit("merge", s.tree(map[string]hash.ObjectID{"f": s.blob(text)}), 3000, ours, theirs)

	result, err := File(t.Context(), s, merged, "f", Options{})

	if err != nil {
		t.Fatalf("File returned error %v", err)
	}
	if got := blamedOn(t, result); strings.Join(got, " ") != "base:1 base:2" {
		t.Fatalf("blame = %v", got)
	}
}

func TestABlameReportsAParentItCannotRead(t *testing.T) {
	s := newStore()
	missing := hash.SumSHA1(object.TypeCommit.String(), []byte("gone"))
	head := s.commit("base", s.tree(map[string]hash.ObjectID{"f": s.blob("one\n")}), 1000, missing)

	if _, err := File(t.Context(), s, head, "f", Options{}); err == nil {
		t.Fatal("a missing parent was ignored")
	}
}

func TestABlameReportsAParentTreeItCannotRead(t *testing.T) {
	s := newStore()
	parentTree := s.tree(map[string]hash.ObjectID{"f": s.blob("one\n")})
	parent := s.commit("base", parentTree, 1000)
	head := s.commit("edit", s.tree(map[string]hash.ObjectID{"f": s.blob("two\n")}), 2000, parent)
	s.fail[parentTree] = errInjected

	if _, err := File(t.Context(), s, head, "f", Options{}); !errors.Is(err, errInjected) {
		t.Fatalf("err = %v", err)
	}
}

func TestFollowingRenamesKeepsTheCommitThatAddedTheFile(t *testing.T) {
	s := newStore()
	base := s.commit("base", s.tree(map[string]hash.ObjectID{"keep": s.blob("keep\n")}), 1000)
	head := s.commit("add", s.tree(map[string]hash.ObjectID{"keep": s.blob("keep\n"), "f": s.blob("one\ntwo\n")}), 2000, base)

	result, err := File(t.Context(), s, head, "f", Options{FollowRenames: true})

	if err != nil {
		t.Fatalf("File returned error %v", err)
	}
	if got := blamedOn(t, result); strings.Join(got, " ") != "add:1 add:2" {
		t.Fatalf("blame = %v", got)
	}
}

func TestFollowingRenamesReportsATreeItCannotWalk(t *testing.T) {
	s := newStore()
	text := "one\ntwo\nthree\n"
	bogus := s.put(&object.Tree{Entries: []object.TreeEntry{{Mode: object.ModeTree, Name: "dir", ID: s.blob("not a tree")}}})
	parent := s.commit("base", bogus, 1000)
	head := s.commit("move", s.tree(map[string]hash.ObjectID{"moved": s.blob(text)}), 2000, parent)

	if _, err := File(t.Context(), s, head, "moved", Options{FollowRenames: true}); err == nil {
		t.Fatal("a broken tree was walked for renames")
	}
}

func TestTwoBranchesThatMeetAtTheSameCommitShareTheWork(t *testing.T) {
	s := newStore()
	base := s.commit("base", s.tree(map[string]hash.ObjectID{"f": s.blob("one\ntwo\nthree\nfour\n")}), 1000)
	first := s.commit("first", s.tree(map[string]hash.ObjectID{"f": s.blob("one\ntwo\nFIRST\nfour\n")}), 2000, base)
	second := s.commit("second", s.tree(map[string]hash.ObjectID{"f": s.blob("one\ntwo\nthree\nSECOND\n")}), 2500, base)
	merged := s.commit("merge", s.tree(map[string]hash.ObjectID{"f": s.blob("one\ntwo\nthree\nSECOND\n")}), 3000, first, second)

	result, err := File(t.Context(), s, merged, "f", Options{})

	if err != nil {
		t.Fatalf("File returned error %v", err)
	}
	if got := blamedOn(t, result); strings.Join(got, " ") != "base:1 base:2 base:3 second:4" {
		t.Fatalf("blame = %v", got)
	}
}

func TestABlameReportsABlobItCannotRead(t *testing.T) {
	s := newStore()
	blob := s.blob("one\ntwo\n")
	head := s.commit("base", s.tree(map[string]hash.ObjectID{"f": blob}), 1000)
	s.fail[blob] = errInjected

	if _, err := File(t.Context(), s, head, "f", Options{}); !errors.Is(err, errInjected) {
		t.Fatalf("err = %v", err)
	}
}
