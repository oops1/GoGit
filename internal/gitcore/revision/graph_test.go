package revision

import (
	"context"
	"errors"
	"math/rand/v2"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/oops1/gogit/internal/gitcore/commitgraph"
	"github.com/oops1/gogit/internal/gitcore/diff"
	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
)

func (b *builder) graphOf(t *testing.T, names ...string) *commitgraph.Graph {
	t.Helper()
	opts := diff.Defaults()
	opts.DetectRenames = false
	var commits []commitgraph.Commit
	for _, name := range names {
		id := b.id(name)
		kind, data, err := b.objects.Get(id)
		if err != nil || kind != object.TypeCommit {
			t.Fatalf("commit %s is unreadable: %v", name, err)
		}
		parsed, err := object.ParseCommit(data)
		if err != nil {
			t.Fatal(err)
		}
		parent := hash.Zero
		if len(parsed.Parents) > 0 {
			_, parentData, err := b.objects.Get(parsed.Parents[0])
			if err != nil {
				t.Fatal(err)
			}
			parentCommit, err := object.ParseCommit(parentData)
			if err != nil {
				t.Fatal(err)
			}
			parent = parentCommit.Tree
		}
		files, err := diff.TreeChanges(t.Context(), b.objects, parent, parsed.Tree, opts)
		if err != nil {
			t.Fatal(err)
		}
		changed := &commitgraph.ChangedPaths{}
		for _, file := range files {
			changed.Paths = append(changed.Paths, file.NewPath)
		}
		commits = append(commits, commitgraph.Commit{ID: id, Tree: parsed.Tree, Parents: parsed.Parents, Time: parsed.Committer.When.Unix(), Changed: changed})
	}
	dir := t.TempDir()
	if err := commitgraph.WriteFile(filepath.Join(dir, commitgraph.InfoDir), hash.SHA1, commits, commitgraph.EncodeOptions{ChangedPaths: true}); err != nil {
		t.Fatal(err)
	}
	graph, err := commitgraph.Open([]string{dir}, commitgraph.OpenOptions{})
	if err != nil || graph == nil {
		t.Fatalf("commitgraph.Open returned %v, %v", graph, err)
	}
	return graph
}

func randomFixture(t *testing.T, seed uint64, count int) (*builder, []string) {
	t.Helper()
	b := newBuilder(t)
	rng := rand.New(rand.NewPCG(seed, seed+11))
	paths := []string{"a.txt", "d/b.txt", "d/e/c.txt"}
	var names []string
	for at := range count {
		name := "n" + strconv.Itoa(at)
		var parents []string
		if at > 0 {
			switch roll := rng.IntN(10); {
			case roll < 6:
				parents = []string{names[rng.IntN(at)]}
			case roll < 9:
				parents = slices.Compact([]string{names[rng.IntN(at)], names[rng.IntN(at)]})
			default:
				parents = slices.Compact([]string{names[rng.IntN(at)], names[rng.IntN(at)], names[rng.IntN(at)]})
			}
		}
		files := map[string]string{}
		if len(parents) > 0 {
			for path, content := range b.files[parents[0]] {
				files[path] = content
			}
		}
		if rng.IntN(3) > 0 {
			files[paths[rng.IntN(len(paths))]] = strconv.Itoa(rng.IntN(5))
		}
		b.clock += int64(rng.IntN(200)) - 120
		b.author = b.clock - int64(rng.IntN(1000))
		b.commitFiles(name, files, parents...)
		names = append(names, name)
	}
	return b, names
}

type walkRecord struct {
	names   []string
	parents [][]hash.ObjectID
	message []string
}

func record(t *testing.T, b *builder, opts Options) (walkRecord, error) {
	t.Helper()
	var ids []hash.ObjectID
	var out walkRecord
	for commit, err := range Walk(t.Context(), opts) {
		if err != nil {
			return out, err
		}
		ids = append(ids, commit.ID)
		out.parents = append(out.parents, commit.Parents)
		out.message = append(out.message, commit.Message)
	}
	out.names = names(t, b, ids)
	return out, nil
}

func sameRecords(a, b walkRecord) bool {
	return slices.Equal(a.names, b.names) && slices.Equal(a.message, b.message) &&
		slices.EqualFunc(a.parents, b.parents, func(x, y []hash.ObjectID) bool { return slices.Equal(x, y) })
}

func TestWalksThroughTheCommitGraphMatchWalksThroughObjects(t *testing.T) {
	for _, seed := range []uint64{1, 2, 3, 4} {
		b, all := randomFixture(t, seed, 70)
		graphs := map[string]*commitgraph.Graph{"full": b.graphOf(t, all...), "older half": b.graphOf(t, all[:35]...)}
		tip, others := all[len(all)-1], []string{all[len(all)-1], all[50], all[20]}
		variants := map[string]func(*Options){
			"default":            nil,
			"topo":               func(o *Options) { o.Order = Topo },
			"date":               func(o *Options) { o.Order = DateOrder },
			"author date":        func(o *Options) { o.Order = AuthorDate },
			"topo first parent":  func(o *Options) { o.Order, o.FirstParent = Topo, true },
			"topo limited":       func(o *Options) { o.Order, o.MaxCount, o.Skip = Topo, 9, 2 },
			"topo until":         func(o *Options) { o.Order, o.Until = Topo, time.Unix(b.clock-900, 0) },
			"topo author filter": func(o *Options) { o.Order, o.Author = Topo, regexp.MustCompile("ann") },
			"paths":              func(o *Options) { o.Paths = []string{"d/e"} },
			"two paths":          func(o *Options) { o.Paths = []string{"a.txt", "d/b.txt"} },
			"first parent paths": func(o *Options) { o.FirstParent, o.Paths = true, []string{"d"} },
			"topo paths":         func(o *Options) { o.Order, o.Paths = Topo, []string{"a.txt"} },
			"since":              func(o *Options) { o.Since = time.Unix(b.clock-1500, 0) },
			"reverse topo":       func(o *Options) { o.Order, o.Reverse = Topo, true },
			"excluded":           func(o *Options) { o.Order, o.Exclude = Topo, []hash.ObjectID{b.id(all[30])} },
		}
		for name, variant := range variants {
			for _, tips := range [][]string{{tip}, others} {
				opts := Options{Context: b.context()}
				for _, tipName := range tips {
					opts.Include = append(opts.Include, b.id(tipName))
				}
				if variant != nil {
					variant(&opts)
				}
				want, err := record(t, b, opts)
				if err != nil {
					t.Fatal(err)
				}
				for graphName, graph := range graphs {
					withGraph := opts
					withGraph.Context.Graph = graph
					got, err := record(t, b, withGraph)
					if err != nil || !sameRecords(got, want) {
						t.Errorf("seed %d, %s, %s, tips %v: %v, %v\nwant %v", seed, name, graphName, tips, got.names, err, want.names)
					}
				}
			}
		}
		for at := 0; at < len(all); at += 7 {
			for other := 3; other < len(all); other += 11 {
				left, right := b.id(all[at]), b.id(all[other])
				plainBases, err := MergeBase(b.context(), left, right)
				if err != nil {
					t.Fatal(err)
				}
				plainAncestor, err := IsAncestor(b.context(), left, right)
				if err != nil {
					t.Fatal(err)
				}
				for graphName, graph := range graphs {
					ctx := b.context()
					ctx.Graph = graph
					bases, err := MergeBase(ctx, left, right)
					if err != nil || !slices.Equal(bases, plainBases) {
						t.Errorf("seed %d, %s: merge bases of %s and %s = %v, want %v", seed, graphName, all[at], all[other], bases, plainBases)
					}
					if ancestor, err := IsAncestor(ctx, left, right); err != nil || ancestor != plainAncestor {
						t.Errorf("seed %d, %s: %s ancestor of %s = %v, want %v", seed, graphName, all[at], all[other], ancestor, plainAncestor)
					}
				}
			}
		}
	}
}

func TestGraphWalksReportObjectsTheGraphDoesNotHold(t *testing.T) {
	b := linearFixture(t)
	b.commit("d", "c")
	b.commit("e", "c")
	graph := b.graphOf(t, "a", "b", "c")
	twoTips := b.graphOf(t, "a", "b", "c", "d", "e")
	ctx := b.context()
	ctx.Graph = graph
	broken := errors.New("broken")
	b.objects.fail[b.id("b")] = broken
	cases := map[string]Options{
		"emitting":         {Context: ctx, Include: []hash.ObjectID{b.id("d")}},
		"filtering":        {Context: ctx, Include: []hash.ObjectID{b.id("d")}, Author: regexp.MustCompile("ann")},
		"author date sort": {Context: ctx, Include: []hash.ObjectID{b.id("d")}, Order: AuthorDate},
		"buffered sort":    {Context: ctx, Include: []hash.ObjectID{b.id("d")}, Order: AuthorDate, Reverse: true},
		"buffered output":  {Context: ctx, Include: []hash.ObjectID{b.id("d")}, Order: Topo, Reverse: true},
	}
	for name, opts := range cases {
		if _, err := record(t, b, opts); !errors.Is(err, broken) {
			t.Errorf("%s returned %v", name, err)
		}
	}
	b.objects.fail[b.id("d")] = broken
	b.objects.fail[b.id("e")] = broken
	tipsCtx := b.context()
	tipsCtx.Graph = twoTips
	if _, err := record(t, b, Options{Context: tipsCtx, Include: []hash.ObjectID{b.id("d"), b.id("e")}, Order: AuthorDate}); !errors.Is(err, broken) {
		t.Errorf("unreadable tips returned %v", err)
	}
	missing := newBuilder(t)
	missing.commit("root")
	missing.commit("child", "root")
	missingCtx := missing.context()
	missingCtx.Graph = b.graphOf(t, "a")
	missing.objects.fail[missing.id("root")] = broken
	if _, err := record(t, missing, Options{Context: missingCtx, Include: []hash.ObjectID{missing.id("child")}, Order: Topo}); !errors.Is(err, broken) {
		t.Errorf("a missing parent in a topological walk returned %v", err)
	}
	if _, err := IsAncestor(missingCtx, missing.id("root"), missing.id("child")); !errors.Is(err, broken) {
		t.Errorf("IsAncestor with a missing ancestor returned %v", err)
	}
	if _, err := IsAncestor(missingCtx, missing.id("child"), missing.id("root")); !errors.Is(err, broken) {
		t.Errorf("IsAncestor with a missing descendant returned %v", err)
	}
}

func TestCountVisitsEveryReachableCommitOnce(t *testing.T) {
	b, all := randomFixture(t, 7, 30)
	want := len(collect(t, b, Walk(t.Context(), b.options(all[29], all[10]))))
	for _, graph := range []*commitgraph.Graph{nil, b.graphOf(t, all[:15]...)} {
		ctx := b.context()
		ctx.Graph = graph
		got, err := Count(t.Context(), ctx, []hash.ObjectID{b.id(all[29]), b.id(all[10]), b.id(all[29])})
		if err != nil || got != want {
			t.Fatalf("Count = %d, %v; want %d", got, err, want)
		}
	}
	broken := errors.New("broken")
	b.objects.fail[b.id(all[0])] = broken
	if _, err := Count(t.Context(), b.context(), []hash.ObjectID{b.id(all[29])}); !errors.Is(err, broken) {
		t.Errorf("a missing ancestor returned %v", err)
	}
	if _, err := Count(t.Context(), b.context(), []hash.ObjectID{b.id(all[0])}); !errors.Is(err, broken) {
		t.Errorf("a missing tip returned %v", err)
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := Count(cancelled, b.context(), []hash.ObjectID{b.id(all[29])}); !errors.Is(err, context.Canceled) {
		t.Errorf("a cancelled count returned %v", err)
	}
}

func TestGraphWalksStopOnCancellation(t *testing.T) {
	b, all := randomFixture(t, 9, 20)
	ctx := b.context()
	ctx.Graph = b.graphOf(t, all...)
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	for _, err := range Walk(cancelled, Options{Context: ctx, Include: []hash.ObjectID{b.id(all[19])}, Order: Topo}) {
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Walk returned %v", err)
		}
		return
	}
	t.Fatal("a cancelled walk yielded nothing")
}

func TestGraphWalkStopsWhenTheConsumerStops(t *testing.T) {
	b, all := randomFixture(t, 5, 20)
	ctx := b.context()
	ctx.Graph = b.graphOf(t, all...)
	seen := 0
	for range Walk(t.Context(), Options{Context: ctx, Include: []hash.ObjectID{b.id(all[19])}, Order: DateOrder}) {
		seen++
		break
	}
	if seen != 1 {
		t.Fatalf("seen = %d", seen)
	}
}
