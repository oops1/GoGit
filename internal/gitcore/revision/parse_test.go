package revision

import (
	"errors"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/refs"
)

func parseFixture(t testing.TB) *builder {
	t.Helper()
	b := newBuilder(t)
	b.commitFiles("c1", map[string]string{"file.txt": "one", "dir/nested.txt": "n1"})
	b.commitFiles("c2", map[string]string{"file.txt": "two", "dir/nested.txt": "n1"}, "c1")
	b.commitFiles("c3", map[string]string{"file.txt": "three", "dir/nested.txt": "n1"}, "c2")
	b.commitFiles("c4", map[string]string{"file.txt": "two", "dir/nested.txt": "n2"}, "c2")
	b.commitFiles("c5", map[string]string{"file.txt": "two", "dir/nested.txt": "n3"}, "c4")
	b.commitFiles("c6", map[string]string{"file.txt": "three", "dir/nested.txt": "n3"}, "c3", "c5")
	b.branch("main", "c6")
	b.branch("topic", "c5")
	b.lightTag("v1", "c3")
	b.annotatedTag("v2", "c5")
	b.remoteBranch("origin", "main", "c3")
	b.refs.values["refs/remotes/origin/HEAD"] = refs.Ref{
		Name:           "refs/remotes/origin/HEAD",
		SymbolicTarget: "refs/remotes/origin/main",
	}
	b.head(refs.BranchName("main"))
	b.reflog(refs.BranchName("main"),
		b.logEntry("", "c1", "branch: Created from HEAD"),
		b.logEntry("c1", "c2", "commit: c2"),
		b.logEntry("c2", "c3", "commit: c3"),
		b.logEntry("c3", "c6", "merge topic: Merge made by the 'ort' strategy."),
	)
	b.reflog(refs.HEAD,
		b.logEntry("", "c1", "commit (initial): c1"),
		b.logEntry("c3", "c5", "checkout: moving from main to topic"),
		b.logEntry("c5", "c6", "checkout: moving from topic to main"),
	)
	return b
}

func TestParseResolvesEveryDocumentedForm(t *testing.T) {
	b := parseFixture(t)
	ctx := b.context()
	ctx.Config = pushConfig{fakeConfig: fakeConfig{
		upstream: map[string]string{"main": "refs/remotes/origin/main"},
		push:     map[string]string{"main": "refs/remotes/origin/main"},
	}}
	tree := func(commit string) hash.ObjectID {
		parsed, err := ctx.Objects.(*objectStore).parseCommit(t, b.id(commit))
		if err != nil {
			t.Fatalf("parseCommit returned error %v", err)
		}
		return parsed.Tree
	}
	tests := []struct {
		name string
		spec string
		want hash.ObjectID
		kind object.Type
		ref  refs.Name
	}{
		{"full hash", b.id("c3").String(), b.id("c3"), object.TypeCommit, ""},
		{"abbreviated hash", b.id("c3").String()[:8], b.id("c3"), object.TypeCommit, ""},
		{"head", "HEAD", b.id("c6"), object.TypeCommit, refs.HEAD},
		{"at sign", "@", b.id("c6"), object.TypeCommit, refs.HEAD},
		{"branch", "main", b.id("c6"), object.TypeCommit, refs.BranchName("main")},
		{"full ref", "refs/heads/main", b.id("c6"), object.TypeCommit, refs.BranchName("main")},
		{"tag", "v1", b.id("c3"), object.TypeCommit, refs.TagName("v1")},
		{"annotated tag", "v2", b.id("tag:v2"), object.TypeTag, refs.TagName("v2")},
		{"remote branch", "origin/main", b.id("c3"), object.TypeCommit, "refs/remotes/origin/main"},
		{"remote head", "origin", b.id("c3"), object.TypeCommit, "refs/remotes/origin/HEAD"},
		{"peel empty", "v2^{}", b.id("c5"), object.TypeCommit, ""},
		{"peel commit", "v2^{commit}", b.id("c5"), object.TypeCommit, ""},
		{"peel tag", "v2^{tag}", b.id("tag:v2"), object.TypeTag, ""},
		{"peel object", "v2^{object}", b.id("tag:v2"), object.TypeTag, ""},
		{"peel tree of tag", "v2^{tree}", tree("c5"), object.TypeTree, ""},
		{"peel tree", "main^{tree}", tree("c6"), object.TypeTree, ""},
		{"first parent", "HEAD^", b.id("c3"), object.TypeCommit, ""},
		{"second parent", "HEAD^2", b.id("c5"), object.TypeCommit, ""},
		{"self", "HEAD^0", b.id("c6"), object.TypeCommit, ""},
		{"ancestor", "HEAD~2", b.id("c2"), object.TypeCommit, ""},
		{"ancestor zero", "HEAD~0", b.id("c6"), object.TypeCommit, ""},
		{"repeated caret", "HEAD^^", b.id("c2"), object.TypeCommit, ""},
		{"repeated tilde", "main~~", b.id("c2"), object.TypeCommit, ""},
		{"mixed suffixes", "HEAD^2~1", b.id("c4"), object.TypeCommit, ""},
		{"tilde on tag", "v2~1", b.id("c4"), object.TypeCommit, ""},
		{"reflog head", "HEAD@{0}", b.id("c6"), object.TypeCommit, refs.HEAD},
		{"reflog previous", "HEAD@{1}", b.id("c5"), object.TypeCommit, refs.HEAD},
		{"reflog branch", "main@{1}", b.id("c3"), object.TypeCommit, refs.BranchName("main")},
		{"reflog branch older", "main@{3}", b.id("c1"), object.TypeCommit, refs.BranchName("main")},
		{"reflog default ref", "@{1}", b.id("c5"), object.TypeCommit, refs.HEAD},
		{"previous branch", "@{-1}", b.id("c5"), object.TypeCommit, refs.BranchName("topic")},
		{"previous branch twice", "@{-2}", b.id("c6"), object.TypeCommit, refs.BranchName("main")},
		{"previous branch suffix", "@{-1}~1", b.id("c4"), object.TypeCommit, ""},
		{"upstream", "main@{upstream}", b.id("c3"), object.TypeCommit, "refs/remotes/origin/main"},
		{"upstream short", "@{u}", b.id("c3"), object.TypeCommit, "refs/remotes/origin/main"},
		{"upstream mixed case", "main@{U}", b.id("c3"), object.TypeCommit, "refs/remotes/origin/main"},
		{"push", "main@{push}", b.id("c3"), object.TypeCommit, "refs/remotes/origin/main"},
		{"previous branch upstream", "@{-2}@{u}", b.id("c3"), object.TypeCommit, "refs/remotes/origin/main"},
		{"message search", ":/c2", b.id("c2"), object.TypeCommit, ""},
		{"message search from rev", "HEAD^{/c4}", b.id("c4"), object.TypeCommit, ""},
		{"path blob", "HEAD:file.txt", b.blob("three"), object.TypeBlob, ""},
		{"path tree", "HEAD:dir", b.tree(map[string]string{"nested.txt": "n3"}), object.TypeTree, ""},
		{"path nested", "HEAD:dir/nested.txt", b.blob("n3"), object.TypeBlob, ""},
		{"path root", "HEAD:", tree("c6"), object.TypeTree, ""},
		{"path through tag", "v2:file.txt", b.blob("two"), object.TypeBlob, ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rev, err := Parse(test.spec, ctx)
			if err != nil {
				t.Fatalf("Parse(%q) returned error %v", test.spec, err)
			}
			if rev.ID != test.want {
				t.Errorf("Parse(%q) resolved to %s, want %s (%s)",
					test.spec, rev.ID, test.want, strings.Join(names(t, b, []hash.ObjectID{test.want}), ""))
			}
			if rev.Type != test.kind {
				t.Errorf("Parse(%q) has type %s, want %s", test.spec, rev.Type, test.kind)
			}
			if rev.Ref != test.ref {
				t.Errorf("Parse(%q) reports ref %q, want %q", test.spec, rev.Ref, test.ref)
			}
		})
	}
}

func (s *objectStore) parseCommit(t *testing.T, id hash.ObjectID) (*object.Commit, error) {
	t.Helper()
	kind, data, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	if kind != object.TypeCommit {
		return nil, errors.New("not a commit")
	}
	return object.ParseCommit(data)
}

func TestParseRejectsMalformedAndMissingRevisions(t *testing.T) {
	b := parseFixture(t)
	ctx := b.context()
	ctx.Config = fakeConfig{upstream: map[string]string{"topic": "refs/remotes/origin/topic"}}
	tests := []struct {
		name string
		spec string
		want error
	}{
		{"empty", "", ErrSyntax},
		{"index stage", ":0:file.txt", ErrUnsupported},
		{"index path", ":file.txt", ErrUnsupported},
		{"reflog date", "HEAD@{yesterday}", ErrUnsupported},
		{"negative reflog", "HEAD@{-0}", ErrSyntax},
		{"prior on a name", "main@{-1}", ErrSyntax},
		{"unknown ref", "nosuchref", ErrNotFound},
		{"short unknown", "abcdef01", ErrNotFound},
		{"too short", "abc", ErrNotFound},
		{"not hex", "zzzzzzzz", ErrNotFound},
		{"missing parent", "HEAD^9", ErrNotFound},
		{"root has no parent", "HEAD~99", ErrNotFound},
		{"peel tree to commit", "main^{tree}^{commit}", ErrNotCommit},
		{"path with a suffix", "HEAD:file.txt^{commit}", ErrNotFound},
		{"peel to blob", "main^{blob}", ErrNotFound},
		{"peel unknown type", "main^{widget}", ErrNotFound},
		{"missing path", "HEAD:nosuch", ErrNotFound},
		{"path through blob", "HEAD:file.txt/deeper", ErrNotFound},
		{"missing reflog", "topic@{1}", ErrNotFound},
		{"reflog too old", "main@{4}", ErrNotFound},
		{"prior missing", "@{-9}", ErrNotFound},
		{"prior malformed", "@{-x}", ErrSyntax},
		{"upstream missing", "topic@{u}", ErrNotFound},
		{"no message match", ":/nothing here", ErrNotFound},
		{"bad message pattern", ":/!oops", ErrSyntax},
		{"bad regexp", ":/(", ErrSyntax},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rev, err := Parse(test.spec, ctx)
			if !errors.Is(err, test.want) {
				t.Fatalf("Parse(%q) returned (%v, %v), want %v", test.spec, rev, err, test.want)
			}
		})
	}
}

func TestParseNegatedMessageSearchSkipsMatchingCommits(t *testing.T) {
	b := parseFixture(t)
	rev, err := Parse("HEAD^{/!-c6}", b.context())
	if err != nil {
		t.Fatalf("Parse returned error %v", err)
	}
	if rev.ID != b.id("c5") {
		t.Errorf("Parse resolved to %s, want c5 %s", rev.ID, b.id("c5"))
	}
}

func TestParseLiteralExclamationMessageSearch(t *testing.T) {
	b := newBuilder(t)
	b.message("bang", "fix !important\n")
	b.branch("main", "bang")
	b.head(refs.BranchName("main"))
	rev, err := Parse(":/!!important", b.context())
	if err != nil {
		t.Fatalf("Parse returned error %v", err)
	}
	if rev.ID != b.id("bang") {
		t.Errorf("Parse resolved to %s, want %s", rev.ID, b.id("bang"))
	}
}

func TestParseUsesConfiguredHeadBranchWhenHeadIsUnborn(t *testing.T) {
	b := parseFixture(t)
	ctx := b.context()
	ctx.Head = refs.BranchName("main")
	ctx.Config = fakeConfig{upstream: map[string]string{"main": "refs/remotes/origin/main"}}
	delete(b.refs.values, refs.HEAD)
	rev, err := Parse("@{u}", ctx)
	if err != nil {
		t.Fatalf("Parse returned error %v", err)
	}
	if rev.ID != b.id("c3") {
		t.Errorf("Parse resolved to %s, want %s", rev.ID, b.id("c3"))
	}
}

func TestParseUpstreamRequiresBranchAndConfiguration(t *testing.T) {
	tests := []struct {
		name string
		spec string
		ctx  func(Context) Context
		want error
	}{
		{"no config", "@{u}", func(c Context) Context { return c }, ErrNotFound},
		{
			"detached head",
			"@{u}",
			func(c Context) Context {
				c.Head = refs.HEAD
				return c
			},
			ErrNotFound,
		},
		{
			"not a branch",
			"v1@{u}",
			func(c Context) Context {
				c.Config = fakeConfig{}
				return c
			},
			ErrNotFound,
		},
		{
			"upstream ref missing",
			"main@{u}",
			func(c Context) Context {
				c.Config = fakeConfig{upstream: map[string]string{"main": "refs/remotes/origin/gone"}}
				return c
			},
			ErrNotFound,
		},
		{
			"upstream name invalid",
			"main@{u}",
			func(c Context) Context {
				c.Config = fakeConfig{upstream: map[string]string{"main": "bad name"}}
				return c
			},
			ErrNotFound,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := parseFixture(t)
			ctx := test.ctx(fixture.context())
			if _, err := Parse(test.spec, ctx); !errors.Is(err, test.want) {
				t.Fatalf("Parse(%q) returned %v, want %v", test.spec, err, test.want)
			}
		})
	}
}

func TestParsePreviousCheckoutOfDetachedHeadUsesObjectID(t *testing.T) {
	b := parseFixture(t)
	b.refs.logs[refs.HEAD] = []refs.ReflogEntry{
		b.logEntry("c6", "c3", "checkout: moving from "+b.id("c6").String()+" to main"),
	}
	rev, err := Parse("@{-1}", b.context())
	if err != nil {
		t.Fatalf("Parse returned error %v", err)
	}
	if rev.ID != b.id("c6") {
		t.Errorf("Parse resolved to %s, want %s", rev.ID, b.id("c6"))
	}
}

func TestParsePreviousCheckoutIgnoresOtherReflogMessages(t *testing.T) {
	b := parseFixture(t)
	b.refs.logs[refs.HEAD] = []refs.ReflogEntry{
		b.logEntry("c1", "c2", "commit: c2"),
		b.logEntry("c2", "c3", "checkout: moving from without a target"),
	}
	if _, err := Parse("@{-1}", b.context()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Parse returned %v, want %v", err, ErrNotFound)
	}
}

func TestParseUnknownBranchInPreviousCheckoutIsNotFound(t *testing.T) {
	b := parseFixture(t)
	b.refs.logs[refs.HEAD] = []refs.ReflogEntry{
		b.logEntry("c6", "c3", "checkout: moving from vanished to main"),
	}
	if _, err := Parse("@{-1}", b.context()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Parse returned %v, want %v", err, ErrNotFound)
	}
	if _, err := Parse("@{-1}@{u}", b.context()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Parse of upstream returned %v, want %v", err, ErrNotFound)
	}
}

func TestParseAmbiguousAbbreviationIsReported(t *testing.T) {
	b := parseFixture(t)
	ctx := b.context()
	ctx.Objects = ambiguousObjects{objectStore: b.objects, ids: []hash.ObjectID{b.id("c1"), b.id("c2")}}
	if _, err := Parse("abcd1234", ctx); !errors.Is(err, ErrAmbiguous) {
		t.Fatalf("Parse returned %v, want %v", err, ErrAmbiguous)
	}
}

type ambiguousObjects struct {
	*objectStore
	ids []hash.ObjectID
}

func (a ambiguousObjects) ResolveShort(string) ([]hash.ObjectID, error) { return a.ids, nil }

func TestParseWithoutPrefixResolverRejectsAbbreviations(t *testing.T) {
	b := parseFixture(t)
	ctx := b.context()
	ctx.Objects = plainObjects{inner: b.objects}
	if _, err := Parse(b.id("c1").String()[:8], ctx); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Parse returned %v, want %v", err, ErrNotFound)
	}
	if _, err := Parse(b.id("c1").String(), ctx); err != nil {
		t.Fatalf("Parse of the full hash returned error %v", err)
	}
}

func TestParseReportsErrorsFromTheObjectSource(t *testing.T) {
	b := parseFixture(t)
	b.objects.shortErr = errors.New("index is broken")
	if _, err := Parse("abcd1234", b.context()); err == nil || !strings.Contains(err.Error(), "index is broken") {
		t.Fatalf("Parse returned %v, want the object source error", err)
	}
	b.objects.fail[b.id("c6")] = errors.New("object file is unreadable")
	if _, err := Parse("HEAD", b.context()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Parse returned %v, want %v", err, ErrNotFound)
	}
}

func TestParseReportsErrorsFromTheReferenceSource(t *testing.T) {
	b := parseFixture(t)
	broken := errors.New("refs are broken")
	b.refs.err = broken
	if _, err := Parse("main", b.context()); !errors.Is(err, broken) {
		t.Fatalf("Parse returned %v, want %v", err, broken)
	}
	if _, err := Parse(":/c1", b.context()); !errors.Is(err, broken) {
		t.Fatalf("Parse of a message search returned %v, want %v", err, broken)
	}
	if _, err := Parse("@{u}", b.context()); !errors.Is(err, broken) {
		t.Fatalf("Parse of the upstream returned %v, want %v", err, broken)
	}
	b.refs.err = nil
	b.refs.logErr = broken
	if _, err := Parse("HEAD@{1}", b.context()); !errors.Is(err, broken) {
		t.Fatalf("Parse of a reflog returned %v, want %v", err, broken)
	}
	if _, err := Parse("@{-1}", b.context()); !errors.Is(err, broken) {
		t.Fatalf("Parse of a previous checkout returned %v, want %v", err, broken)
	}
}

func TestParseRejectsMalformedObjects(t *testing.T) {
	b := parseFixture(t)
	broken := b.objects.putRaw(object.TypeCommit, []byte("not a commit"))
	b.refs.values[refs.BranchName("broken")] = refs.Ref{Name: refs.BranchName("broken"), Target: broken}
	if _, err := Parse("broken", b.context()); !errors.Is(err, object.ErrMalformed) {
		t.Fatalf("Parse returned %v, want %v", err, object.ErrMalformed)
	}
	if _, err := Parse("broken^", b.context()); !errors.Is(err, object.ErrMalformed) {
		t.Fatalf("Parse of a parent returned %v, want %v", err, object.ErrMalformed)
	}
}

func TestParseRejectsCyclicTags(t *testing.T) {
	b := parseFixture(t)
	tagger := b.signature("ann", b.clock)
	first := &object.Tag{Object: b.id("c1"), ObjectType: object.TypeTag, Name: "loop", Tagger: &tagger, Message: "loop\n"}
	id := b.objects.put(first)
	for range maxPeelDepth {
		next := &object.Tag{Object: id, ObjectType: object.TypeTag, Name: "loop", Tagger: &tagger, Message: "loop\n"}
		id = b.objects.put(next)
	}
	b.refs.values[refs.TagName("loop")] = refs.Ref{Name: refs.TagName("loop"), Target: id}
	if _, err := Parse("loop^{}", b.context()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Parse returned %v, want %v", err, ErrNotFound)
	}
}

func TestParseSkipsDanglingSymbolicReferences(t *testing.T) {
	b := parseFixture(t)
	b.refs.values[refs.BranchName("dangling")] = refs.Ref{
		Name:           refs.BranchName("dangling"),
		SymbolicTarget: refs.BranchName("gone"),
	}
	if _, err := Parse("dangling", b.context()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Parse returned %v, want %v", err, ErrNotFound)
	}
}

func TestParseTreeLookupReportsBrokenTrees(t *testing.T) {
	b := parseFixture(t)
	broken := b.objects.putRaw(object.TypeTree, []byte("garbage"))
	commit := &object.Commit{
		Tree:      broken,
		Author:    b.signature("ann", b.clock),
		Committer: b.signature("cody", b.clock),
		Message:   "broken tree\n",
	}
	id := b.objects.put(commit)
	b.ids["broken"] = id
	b.branch("brokentree", "broken")
	if _, err := Parse("brokentree:file.txt", b.context()); !errors.Is(err, object.ErrMalformed) {
		t.Fatalf("Parse returned %v, want %v", err, object.ErrMalformed)
	}
}

func TestParseSkipsReferencesWithoutAResolvedTarget(t *testing.T) {
	b := parseFixture(t)
	name := refs.BranchName("empty")
	b.refs.values[name] = refs.Ref{Name: name}
	if _, err := Parse("empty", b.context()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Parse returned %v, want %v", err, ErrNotFound)
	}
}

func TestParseReportsObjectsMissingBehindReflogsAndUpstreams(t *testing.T) {
	tests := []struct {
		name    string
		spec    string
		prepare func(*builder) Context
	}{
		{
			"reflog entry",
			"main@{1}",
			func(b *builder) Context {
				b.objects.fail[b.id("c3")] = errors.New("gone")
				return b.context()
			},
		},
		{
			"upstream branch",
			"main@{u}",
			func(b *builder) Context {
				b.objects.fail[b.id("c3")] = errors.New("gone")
				ctx := b.context()
				ctx.Config = fakeConfig{upstream: map[string]string{"main": "refs/remotes/origin/main"}}
				return ctx
			},
		},
		{
			"previous checkout",
			"@{-1}",
			func(b *builder) Context {
				b.objects.fail[b.id("c5")] = errors.New("gone")
				return b.context()
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			b := parseFixture(t)
			if _, err := Parse(test.spec, test.prepare(b)); !errors.Is(err, ErrNotFound) {
				t.Fatalf("Parse(%q) returned %v, want %v", test.spec, err, ErrNotFound)
			}
		})
	}
}

func TestParseReportsReferenceErrorsBehindMarks(t *testing.T) {
	broken := errors.New("refs are broken")
	tests := []struct {
		name    string
		spec    string
		prepare func(*builder) Context
	}{
		{
			"previous checkout target",
			"@{-1}",
			func(b *builder) Context {
				b.refs.err = broken
				return b.context()
			},
		},
		{
			"upstream of a previous checkout",
			"@{-1}@{u}",
			func(b *builder) Context {
				b.refs.logErr = broken
				ctx := b.context()
				ctx.Config = fakeConfig{}
				return ctx
			},
		},
		{
			"upstream of a named branch",
			"topic@{u}",
			func(b *builder) Context {
				b.refs.err = broken
				ctx := b.context()
				ctx.Config = fakeConfig{}
				return ctx
			},
		},
		{
			"message search from HEAD",
			":/c1",
			func(b *builder) Context {
				b.refs.headErr = broken
				return b.context()
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			b := parseFixture(t)
			if _, err := Parse(test.spec, test.prepare(b)); !errors.Is(err, broken) {
				t.Fatalf("Parse(%q) returned %v, want %v", test.spec, err, broken)
			}
		})
	}
}

func TestParseRejectsHugeAncestorNumbers(t *testing.T) {
	b := parseFixture(t)
	if _, err := Parse("HEAD~99999999999999999999", b.context()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Parse returned %v, want %v", err, ErrNotFound)
	}
}

func TestParseRejectsParentsOfObjectsThatAreNotCommits(t *testing.T) {
	b := parseFixture(t)
	for _, spec := range []string{"main^{tree}^1", "main^{tree}~1"} {
		if _, err := Parse(spec, b.context()); !errors.Is(err, ErrNotCommit) {
			t.Fatalf("Parse(%q) returned %v, want %v", spec, err, ErrNotCommit)
		}
	}
}

func TestParseReportsMissingAncestors(t *testing.T) {
	b := parseFixture(t)
	b.objects.fail[b.id("c3")] = errors.New("gone")
	if _, err := Parse("main~2", b.context()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Parse returned %v, want %v", err, ErrNotFound)
	}
}

func TestParsePathsNeedARevisionThatHoldsATree(t *testing.T) {
	b := parseFixture(t)
	name := refs.TagName("blobtag")
	b.refs.values[name] = refs.Ref{Name: name, Target: b.blob("one")}
	if _, err := Parse("nosuch:file.txt", b.context()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Parse of a missing revision returned %v, want %v", err, ErrNotFound)
	}
	if _, err := Parse("blobtag:file.txt", b.context()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Parse of a blob returned %v, want %v", err, ErrNotFound)
	}
}

func TestParseRejectsTreeEntriesThatLieAboutTheirMode(t *testing.T) {
	b := parseFixture(t)
	tree := &object.Tree{Entries: []object.TreeEntry{
		{Mode: object.ModeTree, Name: "dir", ID: b.blob("one")},
	}}
	commit := &object.Commit{
		Tree:      b.objects.put(tree),
		Author:    b.signature("ann", b.clock),
		Committer: b.signature("cody", b.clock),
		Message:   "lying tree\n",
	}
	b.ids["lying"] = b.objects.put(commit)
	b.branch("lying", "lying")
	if _, err := Parse("lying:dir/file.txt", b.context()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Parse returned %v, want %v", err, ErrNotFound)
	}
}

func TestParseMessageSearchNeedsCommits(t *testing.T) {
	b := parseFixture(t)
	if _, err := Parse("main^{tree}^{/c1}", b.context()); !errors.Is(err, ErrNotCommit) {
		t.Fatalf("Parse returned %v, want %v", err, ErrNotCommit)
	}
	b.objects.fail[b.id("c5")] = errors.New("gone")
	if _, err := Parse("v2^{/c1}", b.context()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Parse behind a tag returned %v, want %v", err, ErrNotFound)
	}
}

func TestParseMessageSearchReportsMissingHistory(t *testing.T) {
	b := parseFixture(t)
	b.objects.fail[b.id("c4")] = errors.New("gone")
	if _, err := Parse("topic^{/nothing}", b.context()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Parse returned %v, want %v", err, ErrNotFound)
	}
	if _, err := Parse("nosuch^{/c1}", b.context()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Parse of a missing revision returned %v, want %v", err, ErrNotFound)
	}
	name := refs.BranchName("brokenref")
	b.refs.values[name] = refs.Ref{Name: name, Target: hash.SumSHA1("commit", []byte("nothing"))}
	if _, err := Parse(":/nothing", b.context()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Parse of a message search returned %v, want %v", err, ErrNotFound)
	}
}

func TestParseMessageSearchVisitsSharedTipsOnce(t *testing.T) {
	b := parseFixture(t)
	b.lightTag("same", "c6")
	rev, err := Parse(":/c6", b.context())
	if err != nil {
		t.Fatalf("Parse returned error %v", err)
	}
	if rev.ID != b.id("c6") {
		t.Errorf("Parse resolved to %s, want %s", rev.ID, b.id("c6"))
	}
}

func TestParseResolvesDetachedHead(t *testing.T) {
	b := parseFixture(t)
	b.detach("c3")
	ctx := b.context()
	ctx.Config = fakeConfig{upstream: map[string]string{"main": "refs/remotes/origin/main"}}
	rev, err := Parse("HEAD", ctx)
	if err != nil {
		t.Fatalf("Parse returned error %v", err)
	}
	if rev.ID != b.id("c3") {
		t.Errorf("Parse resolved HEAD to %s, want %s", rev.ID, b.id("c3"))
	}
	if _, err := Parse("@{u}", ctx); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Parse of the upstream returned %v, want %v", err, ErrNotFound)
	}
}
