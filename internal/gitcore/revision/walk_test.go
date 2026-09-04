package revision

import (
	"context"
	"errors"
	"iter"
	"regexp"
	"slices"
	"testing"
	"time"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/refs"
)

func linearFixture(t testing.TB) *builder {
	t.Helper()
	b := newBuilder(t)
	b.commit("a")
	b.commit("b", "a")
	b.commit("c", "b")
	b.branch("main", "c")
	b.head(refs.BranchName("main"))
	return b
}

func mergeFixture(t testing.TB) *builder {
	t.Helper()
	b := newBuilder(t)
	b.commit("a")
	b.author = 1700009999
	b.commit("side", "a")
	b.commit("main1", "a")
	b.commit("merge", "main1", "side")
	b.branch("main", "merge")
	b.branch("topic", "side")
	b.head(refs.BranchName("main"))
	return b
}

func (b *builder) options(include ...string) Options {
	b.t.Helper()
	opts := Options{Context: b.context()}
	for _, name := range include {
		opts.Include = append(opts.Include, b.id(name))
	}
	return opts
}

func TestWalkVisitsLinearHistoryNewestFirst(t *testing.T) {
	b := linearFixture(t)
	got := collect(t, b, Walk(t.Context(), b.options("c")))
	if want := []string{"c", "b", "a"}; !slices.Equal(got, want) {
		t.Errorf("Walk visited %v, want %v", got, want)
	}
}

func TestWalkOrdersMergesByRequestedSort(t *testing.T) {
	tests := []struct {
		name  string
		order Order
		want  []string
	}{
		{"default", Default, []string{"merge", "main1", "side", "a"}},
		{"date", DateOrder, []string{"merge", "main1", "side", "a"}},
		{"topological", Topo, []string{"merge", "side", "main1", "a"}},
		{"author date", AuthorDate, []string{"merge", "side", "main1", "a"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			b := mergeFixture(t)
			opts := b.options("merge")
			opts.Order = test.order
			got := collect(t, b, Walk(t.Context(), opts))
			if !slices.Equal(got, test.want) {
				t.Errorf("Walk visited %v, want %v", got, test.want)
			}
		})
	}
}

func TestWalkReverseEmitsOldestFirst(t *testing.T) {
	b := linearFixture(t)
	opts := b.options("c")
	opts.Reverse = true
	got := collect(t, b, Walk(t.Context(), opts))
	if want := []string{"a", "b", "c"}; !slices.Equal(got, want) {
		t.Errorf("Walk visited %v, want %v", got, want)
	}
}

func TestWalkReverseAppliesMaxCountBeforeReversing(t *testing.T) {
	b := linearFixture(t)
	opts := b.options("c")
	opts.Reverse = true
	opts.MaxCount = 2
	got := collect(t, b, Walk(t.Context(), opts))
	if want := []string{"b", "c"}; !slices.Equal(got, want) {
		t.Errorf("Walk visited %v, want %v", got, want)
	}
}

func TestWalkFollowsOnlyFirstParents(t *testing.T) {
	b := mergeFixture(t)
	opts := b.options("merge")
	opts.FirstParent = true
	got := collect(t, b, Walk(t.Context(), opts))
	if want := []string{"merge", "main1", "a"}; !slices.Equal(got, want) {
		t.Errorf("Walk visited %v, want %v", got, want)
	}
}

func TestWalkHonoursSkipAndMaxCount(t *testing.T) {
	b := linearFixture(t)
	tests := []struct {
		name  string
		skip  int
		count int
		want  []string
	}{
		{"skip one", 1, 0, []string{"b", "a"}},
		{"first two", 0, 2, []string{"c", "b"}},
		{"skip and limit", 1, 1, []string{"b"}},
		{"skip past the end", 5, 0, nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			opts := b.options("c")
			opts.Skip, opts.MaxCount = test.skip, test.count
			got := collect(t, b, Walk(t.Context(), opts))
			if !slices.Equal(got, test.want) {
				t.Errorf("Walk visited %v, want %v", got, test.want)
			}
		})
	}
}

func TestWalkExcludesReachableHistory(t *testing.T) {
	b := mergeFixture(t)
	opts := b.options("merge")
	opts.Exclude = append(opts.Exclude, b.id("main1"))
	got := collect(t, b, Walk(t.Context(), opts))
	if want := []string{"merge", "side"}; !slices.Equal(got, want) {
		t.Errorf("Walk visited %v, want %v", got, want)
	}
}

func TestWalkStopsWhenNothingInterestingRemains(t *testing.T) {
	b := newBuilder(t)
	b.commit("root")
	previous := "root"
	for index := range 20 {
		name := "deep" + string(rune('a'+index))
		b.commit(name, previous)
		previous = name
	}
	b.commit("tip", previous)
	opts := b.options("tip")
	opts.Exclude = append(opts.Exclude, b.id(previous))
	before := b.objects.gets
	got := collect(t, b, Walk(t.Context(), opts))
	if want := []string{"tip"}; !slices.Equal(got, want) {
		t.Fatalf("Walk visited %v, want %v", got, want)
	}
	if read := b.objects.gets - before; read > 10 {
		t.Errorf("Walk read %d objects, want the traversal to stop early", read)
	}
}

func TestWalkFiltersByAuthorCommitterAndMessage(t *testing.T) {
	b := newBuilder(t)
	b.message("first", "add parser\n")
	b.message("second", "fix parser\nsecond line\n", "first")
	b.message("third", "drop parser\n", "second")
	tests := []struct {
		name    string
		options func(*Options)
		want    []string
	}{
		{"message", func(o *Options) { o.Grep = regexp.MustCompile("^fix") }, []string{"second"}},
		{"message line", func(o *Options) { o.Grep = regexp.MustCompile("^second line$") }, []string{"second"}},
		{"author", func(o *Options) { o.Author = regexp.MustCompile("ann") }, []string{"third", "second", "first"}},
		{"other author", func(o *Options) { o.Author = regexp.MustCompile("zoe") }, nil},
		{"committer", func(o *Options) { o.Committer = regexp.MustCompile("cody@") }, []string{"third", "second", "first"}},
		{"other committer", func(o *Options) { o.Committer = regexp.MustCompile("^zoe") }, nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			opts := b.options("third")
			test.options(&opts)
			got := collect(t, b, Walk(t.Context(), opts))
			if !slices.Equal(got, test.want) {
				t.Errorf("Walk visited %v, want %v", got, test.want)
			}
		})
	}
}

func TestWalkLimitsHistoryByDate(t *testing.T) {
	b := linearFixture(t)
	middle := time.Unix(b.clock-60, 0).UTC()
	t.Run("since", func(t *testing.T) {
		opts := b.options("c")
		opts.Since = middle
		got := collect(t, b, Walk(t.Context(), opts))
		if want := []string{"c", "b"}; !slices.Equal(got, want) {
			t.Errorf("Walk visited %v, want %v", got, want)
		}
	})
	t.Run("until", func(t *testing.T) {
		opts := b.options("c")
		opts.Until = middle
		got := collect(t, b, Walk(t.Context(), opts))
		if want := []string{"b", "a"}; !slices.Equal(got, want) {
			t.Errorf("Walk visited %v, want %v", got, want)
		}
	})
}

func TestWalkSimplifiesHistoryByPath(t *testing.T) {
	b := newBuilder(t)
	b.commitFiles("p1", map[string]string{"a.txt": "1", "dir/b.txt": "1"})
	b.commitFiles("p2", map[string]string{"a.txt": "1", "dir/b.txt": "2"}, "p1")
	b.commitFiles("p3", map[string]string{"a.txt": "2", "dir/b.txt": "2"}, "p2")
	tests := []struct {
		name  string
		paths []string
		want  []string
	}{
		{"file", []string{"a.txt"}, []string{"p3", "p1"}},
		{"directory", []string{"dir"}, []string{"p2", "p1"}},
		{"nested file", []string{"./dir/b.txt/"}, []string{"p2", "p1"}},
		{"both", []string{"a.txt", "dir"}, []string{"p3", "p2", "p1"}},
		{"missing", []string{"gone.txt"}, nil},
		{"empty entry", []string{""}, []string{"p3", "p2", "p1"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			opts := b.options("p3")
			opts.Paths = test.paths
			got := collect(t, b, Walk(t.Context(), opts))
			if !slices.Equal(got, test.want) {
				t.Errorf("Walk visited %v, want %v", got, test.want)
			}
		})
	}
}

func TestWalkFollowsTheParentThatCarriesThePathChange(t *testing.T) {
	b := newBuilder(t)
	b.commitFiles("root", map[string]string{"a.txt": "1"})
	b.commitFiles("changed", map[string]string{"a.txt": "2"}, "root")
	b.commitFiles("other", map[string]string{"a.txt": "1", "z.txt": "1"}, "root")
	b.commitFiles("merge", map[string]string{"a.txt": "2", "z.txt": "1"}, "changed", "other")
	opts := b.options("merge")
	opts.Paths = []string{"a.txt"}
	got := collect(t, b, Walk(t.Context(), opts))
	if want := []string{"changed", "root"}; !slices.Equal(got, want) {
		t.Errorf("Walk visited %v, want %v", got, want)
	}
}

func TestWalkKeepsMergesThatChangeThePathOnEverySide(t *testing.T) {
	b := newBuilder(t)
	b.commitFiles("root", map[string]string{"a.txt": "1"})
	b.commitFiles("left", map[string]string{"a.txt": "2"}, "root")
	b.commitFiles("right", map[string]string{"a.txt": "3"}, "root")
	b.commitFiles("merge", map[string]string{"a.txt": "4"}, "left", "right")
	opts := b.options("merge")
	opts.Paths = []string{"a.txt"}
	got := collect(t, b, Walk(t.Context(), opts))
	if want := []string{"merge", "right", "left", "root"}; !slices.Equal(got, want) {
		t.Errorf("Walk visited %v, want %v", got, want)
	}
}

func TestWalkKeepsExcludedParentsWhenSimplifying(t *testing.T) {
	b := newBuilder(t)
	b.commitFiles("root", map[string]string{"a.txt": "1"})
	b.commitFiles("left", map[string]string{"a.txt": "2"}, "root")
	b.commitFiles("right", map[string]string{"a.txt": "2", "z.txt": "1"}, "left")
	b.commitFiles("merge", map[string]string{"a.txt": "2", "z.txt": "1"}, "left", "right")
	opts := b.options("merge")
	opts.Exclude = append(opts.Exclude, b.id("left"))
	opts.Paths = []string{"a.txt"}
	got := collect(t, b, Walk(t.Context(), opts))
	if got != nil {
		t.Errorf("Walk visited %v, want nothing", got)
	}
}

func TestWalkWithFirstParentComparesOnlyTheFirstParentTree(t *testing.T) {
	b := newBuilder(t)
	b.commitFiles("root", map[string]string{"a.txt": "1"})
	b.commitFiles("left", map[string]string{"a.txt": "1", "z.txt": "1"}, "root")
	b.commitFiles("right", map[string]string{"a.txt": "2"}, "root")
	b.commitFiles("merge", map[string]string{"a.txt": "2", "z.txt": "1"}, "left", "right")
	opts := b.options("merge")
	opts.Paths = []string{"a.txt"}
	opts.FirstParent = true
	got := collect(t, b, Walk(t.Context(), opts))
	if want := []string{"merge", "root"}; !slices.Equal(got, want) {
		t.Errorf("Walk visited %v, want %v", got, want)
	}
}

func TestWalkTreatsEmptyRootAsUnchanged(t *testing.T) {
	b := newBuilder(t)
	b.commitFiles("root", map[string]string{"other.txt": "1"})
	b.commitFiles("added", map[string]string{"other.txt": "1", "a.txt": "1"}, "root")
	opts := b.options("added")
	opts.Paths = []string{"a.txt"}
	got := collect(t, b, Walk(t.Context(), opts))
	if want := []string{"added"}; !slices.Equal(got, want) {
		t.Errorf("Walk visited %v, want %v", got, want)
	}
}

func TestWalkYieldsCommitsBeforeTheWholeGraphIsRead(t *testing.T) {
	b := newBuilder(t)
	previous := ""
	for index := range 40 {
		name := "n" + string(rune('a'+index))
		if previous == "" {
			b.commit(name)
		} else {
			b.commit(name, previous)
		}
		previous = name
	}
	before := b.objects.gets
	for commit, err := range Walk(t.Context(), b.options(previous)) {
		if err != nil {
			t.Fatalf("Walk returned error %v", err)
		}
		if commit.ID != b.id(previous) {
			t.Fatalf("Walk started at %s, want %s", commit.ID, b.id(previous))
		}
		break
	}
	if read := b.objects.gets - before; read > 4 {
		t.Errorf("Walk read %d objects before the first commit, want a lazy traversal", read)
	}
}

func TestWalkStopsOnContextCancellation(t *testing.T) {
	b := linearFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for _, err := range Walk(ctx, b.options("c")) {
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Walk returned %v, want %v", err, context.Canceled)
		}
		return
	}
	t.Fatal("Walk did not report the cancelled context")
}

func TestWalkStopsOnContextCancellationWhileBuffering(t *testing.T) {
	b := mergeFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	opts := b.options("merge")
	opts.Order = Topo
	for _, err := range Walk(ctx, opts) {
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Walk returned %v, want %v", err, context.Canceled)
		}
		return
	}
	t.Fatal("Walk did not report the cancelled context")
}

func TestWalkStopsWhenTheConsumerStops(t *testing.T) {
	for _, order := range []Order{Default, Topo} {
		b := linearFixture(t)
		opts := b.options("c")
		opts.Order = order
		seen := 0
		for range Walk(t.Context(), opts) {
			seen++
			break
		}
		if seen != 1 {
			t.Fatalf("Walk yielded %d commits after the consumer stopped, want 1", seen)
		}
	}
}

func walkError(t testing.TB, sequence iter.Seq2[*Commit, error]) error {
	t.Helper()
	for _, err := range sequence {
		if err != nil {
			return err
		}
	}
	return nil
}

func TestWalkReportsMissingAndUnexpectedObjects(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(*builder, *Options)
		want    error
	}{
		{
			"tip is not a commit",
			func(b *builder, o *Options) { o.Include = []hash.ObjectID{b.blob("data")} },
			ErrNotCommit,
		},
		{
			"excluded tip is missing",
			func(b *builder, o *Options) {
				o.Exclude = []hash.ObjectID{hash.SumSHA1("commit", []byte("nothing"))}
				_ = b
			},
			ErrNotFound,
		},
		{
			"parent is missing",
			func(b *builder, o *Options) {
				b.objects.fail[b.id("a")] = errors.New("gone")
				_ = o
			},
			ErrNotFound,
		},
		{
			"parent is missing while buffering",
			func(b *builder, o *Options) {
				b.objects.fail[b.id("a")] = errors.New("gone")
				o.Order = Topo
			},
			ErrNotFound,
		},
		{
			"excluded history is missing",
			func(b *builder, o *Options) {
				o.Exclude = append(o.Exclude, b.id("main1"))
				b.objects.fail[b.id("a")] = errors.New("gone")
			},
			ErrNotFound,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			b := mergeFixture(t)
			opts := b.options("merge")
			test.prepare(b, &opts)
			if err := walkError(t, Walk(t.Context(), opts)); !errors.Is(err, test.want) {
				t.Fatalf("Walk returned %v, want %v", err, test.want)
			}
		})
	}
}

func TestWalkReportsBrokenTreesWhilePruningPaths(t *testing.T) {
	b := newBuilder(t)
	b.commitFiles("root", map[string]string{"a.txt": "1"})
	b.commitFiles("next", map[string]string{"a.txt": "2"}, "root")
	broken := b.objects.putRaw(object.TypeTree, []byte("garbage"))
	commit := &object.Commit{
		Tree:      broken,
		Author:    b.signature("ann", b.clock),
		Committer: b.signature("cody", b.clock+60),
		Message:   "broken\n",
		Parents:   []hash.ObjectID{b.id("next")},
	}
	b.ids["broken"] = b.objects.put(commit)
	opts := b.options("broken")
	opts.Paths = []string{"a.txt"}
	for _, err := range Walk(t.Context(), opts) {
		if !errors.Is(err, object.ErrMalformed) {
			t.Fatalf("Walk returned %v, want %v", err, object.ErrMalformed)
		}
		return
	}
	t.Fatal("Walk did not report the broken tree")
}

func TestWalkAcceptsRepeatedTips(t *testing.T) {
	b := linearFixture(t)
	opts := b.options("c", "c", "b")
	got := collect(t, b, Walk(t.Context(), opts))
	if want := []string{"c", "b", "a"}; !slices.Equal(got, want) {
		t.Errorf("Walk visited %v, want %v", got, want)
	}
}

func TestWalkWithoutTipsYieldsNothing(t *testing.T) {
	b := linearFixture(t)
	got := collect(t, b, Walk(t.Context(), b.options()))
	if got != nil {
		t.Errorf("Walk visited %v, want nothing", got)
	}
}

func TestWalkExposesParentsOfEveryCommit(t *testing.T) {
	b := mergeFixture(t)
	for commit, err := range Walk(t.Context(), b.options("merge")) {
		if err != nil {
			t.Fatalf("Walk returned error %v", err)
		}
		if commit.ID != b.id("merge") {
			continue
		}
		want := []hash.ObjectID{b.id("main1"), b.id("side")}
		if !slices.Equal(commit.Parents, want) {
			t.Fatalf("merge has parents %v, want %v", commit.Parents, want)
		}
		if commit.Message != "merge\n" {
			t.Fatalf("merge carries message %q", commit.Message)
		}
	}
}

func TestWalkTopologicalOrderIgnoresExcludedParents(t *testing.T) {
	b := mergeFixture(t)
	opts := b.options("merge")
	opts.Exclude = append(opts.Exclude, b.id("side"))
	opts.Order = Topo
	got := collect(t, b, Walk(t.Context(), opts))
	if want := []string{"merge", "main1"}; !slices.Equal(got, want) {
		t.Errorf("Walk visited %v, want %v", got, want)
	}
}

func TestWalkKeepsLookingWhileTheQueueHoldsSameDatedCommits(t *testing.T) {
	b := newBuilder(t)
	b.commit("root")
	b.commit("left", "root")
	b.clock -= 60
	b.commit("right", "root")
	b.clock -= 60
	b.commit("third", "root")
	opts := b.options("left", "third")
	opts.Exclude = append(opts.Exclude, b.id("right"))
	got := collect(t, b, Walk(t.Context(), opts))
	if want := []string{"left", "third"}; !slices.Equal(got, want) {
		t.Errorf("Walk visited %v, want %v", got, want)
	}
}

func TestWalkMarksAlreadyLoadedAncestorsUninteresting(t *testing.T) {
	b := newBuilder(t)
	b.commit("bottom")
	b.commit("root", "bottom")
	b.commit("mid", "root")
	b.commit("tip", "mid")
	b.clock -= 600
	b.commit("old", "mid")
	opts := b.options("tip")
	opts.Exclude = append(opts.Exclude, b.id("old"))
	got := collect(t, b, Walk(t.Context(), opts))
	if want := []string{"tip"}; !slices.Equal(got, want) {
		t.Errorf("Walk visited %v, want %v", got, want)
	}
}

func TestWalkSharesExcludedParentsBetweenTips(t *testing.T) {
	b := linearFixture(t)
	opts := b.options("c")
	opts.Exclude = append(opts.Exclude, b.id("b"), b.id("a"))
	got := collect(t, b, Walk(t.Context(), opts))
	if want := []string{"c"}; !slices.Equal(got, want) {
		t.Errorf("Walk visited %v, want %v", got, want)
	}
}

func TestWalkOrdersCommitsWithEqualAuthorDatesByArrival(t *testing.T) {
	b := newBuilder(t)
	b.author = 1700000500
	b.commit("root")
	b.author = 1700000500
	b.commit("left", "root")
	b.author = 1700000500
	b.commit("right", "root")
	b.author = 1700000500
	b.commit("merge", "left", "right")
	opts := b.options("merge")
	opts.Order = AuthorDate
	got := collect(t, b, Walk(t.Context(), opts))
	if want := []string{"merge", "left", "right", "root"}; !slices.Equal(got, want) {
		t.Errorf("Walk visited %v, want %v", got, want)
	}
}

func TestWalkReportsBrokenHistoryWhilePruningPaths(t *testing.T) {
	broken := errors.New("gone")
	tests := []struct {
		name    string
		prepare func(*builder) hash.ObjectID
		want    error
	}{
		{
			"missing parent",
			func(b *builder) hash.ObjectID {
				b.commitFiles("root", map[string]string{"a.txt": "1"})
				b.commitFiles("tip", map[string]string{"a.txt": "2"}, "root")
				b.objects.fail[b.id("root")] = broken
				return b.id("tip")
			},
			ErrNotFound,
		},
		{
			"broken parent tree",
			func(b *builder) hash.ObjectID {
				b.commitFiles("root", map[string]string{"a.txt": "1"})
				b.commitFiles("tip", map[string]string{"a.txt": "2"}, "root")
				b.objects.fail[b.tree(map[string]string{"a.txt": "1"})] = broken
				return b.id("tip")
			},
			ErrNotFound,
		},
		{
			"broken root tree",
			func(b *builder) hash.ObjectID {
				b.commitFiles("root", map[string]string{"a.txt": "1"})
				b.objects.fail[b.tree(map[string]string{"a.txt": "1"})] = broken
				return b.id("root")
			},
			ErrNotFound,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			b := newBuilder(t)
			tip := test.prepare(b)
			opts := Options{Context: b.context(), Include: []hash.ObjectID{tip}, Paths: []string{"a.txt"}}
			if err := walkError(t, Walk(t.Context(), opts)); !errors.Is(err, test.want) {
				t.Fatalf("Walk returned %v, want %v", err, test.want)
			}
		})
	}
}

func TestWalkKeepsSearchingWhenAQueuedParentIsNewerThanTheLastCommit(t *testing.T) {
	b := newBuilder(t)
	b.clock = 1700000200 - 60
	b.commit("skewed")
	b.clock = 1700000090 - 60
	b.commit("excluded", "skewed")
	b.clock = 1700000100 - 60
	b.commit("wanted")
	opts := b.options("wanted")
	opts.Exclude = append(opts.Exclude, b.id("excluded"))
	got := collect(t, b, Walk(t.Context(), opts))
	if want := []string{"wanted"}; !slices.Equal(got, want) {
		t.Errorf("Walk visited %v, want %v", got, want)
	}
}
