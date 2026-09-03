package revision

import (
	"errors"
	"slices"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/refs"
)

func TestRangesTranslatesRevisionArguments(t *testing.T) {
	tests := []struct {
		name    string
		specs   []string
		include []string
		exclude []string
		paths   []string
	}{
		{"single revision", []string{"main"}, []string{"c6"}, nil, nil},
		{"tag", []string{"v2"}, []string{"c5"}, nil, nil},
		{"exclusion", []string{"main", "^topic"}, []string{"c6"}, []string{"c5"}, nil},
		{"two dots", []string{"topic..main"}, []string{"c6"}, []string{"c5"}, nil},
		{"two dots with a default left side", []string{"..topic"}, []string{"c5"}, []string{"c6"}, nil},
		{"two dots with a default right side", []string{"topic.."}, []string{"c6"}, []string{"c5"}, nil},
		{"three dots", []string{"topic...main"}, []string{"c5", "c6"}, []string{"c5"}, nil},
		{"not", []string{"--not", "main", "topic"}, nil, []string{"c6", "c5"}, nil},
		{"not twice", []string{"--not", "main", "--not", "topic"}, []string{"c5"}, []string{"c6"}, nil},
		{"not inverts a range", []string{"--not", "topic..main"}, []string{"c5"}, []string{"c6"}, nil},
		{"not inverts an exclusion", []string{"--not", "^main"}, []string{"c6"}, nil, nil},
		{
			"not inverts a symmetric range",
			[]string{"--not", "topic...main"},
			[]string{"c5"},
			[]string{"c5", "c6"},
			nil,
		},
		{"branches", []string{"--branches"}, []string{"c6", "c5"}, nil, nil},
		{"branches with a glob", []string{"--branches=t*"}, []string{"c5"}, nil, nil},
		{"branches with a plain name", []string{"--branches=main"}, nil, nil, nil},
		{"tags", []string{"--tags"}, []string{"c3", "c5"}, nil, nil},
		{"tags with a glob", []string{"--tags=v1*"}, []string{"c3"}, nil, nil},
		{"remotes", []string{"--remotes"}, []string{"c3", "c3"}, nil, nil},
		{"glob", []string{"--glob=refs/heads/*"}, []string{"c6", "c5"}, nil, nil},
		{"glob relative to refs", []string{"--glob=heads/*"}, []string{"c6", "c5"}, nil, nil},
		{"glob with a question mark", []string{"--glob=heads/mai?"}, []string{"c6"}, nil, nil},
		{"glob across slashes", []string{"--glob=**/main"}, []string{"c6", "c3"}, nil, nil},
		{"paths", []string{"main", "--", "file.txt", "dir"}, []string{"c6"}, nil, []string{"file.txt", "dir"}},
		{"only paths", []string{"--", "file.txt"}, nil, nil, []string{"file.txt"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			b := parseFixture(t)
			opts, err := Ranges(test.specs, b.context())
			if err != nil {
				t.Fatalf("Ranges(%v) returned error %v", test.specs, err)
			}
			if got := names(t, b, opts.Include); !slices.Equal(got, test.include) {
				t.Errorf("Ranges(%v) includes %v, want %v", test.specs, got, test.include)
			}
			if got := names(t, b, opts.Exclude); !slices.Equal(got, test.exclude) {
				t.Errorf("Ranges(%v) excludes %v, want %v", test.specs, got, test.exclude)
			}
			if !slices.Equal(opts.Paths, test.paths) {
				t.Errorf("Ranges(%v) collected paths %v, want %v", test.specs, opts.Paths, test.paths)
			}
		})
	}
}

func TestRangesCollectsEveryReferenceForAll(t *testing.T) {
	b := parseFixture(t)
	opts, err := Ranges([]string{"--all"}, b.context())
	if err != nil {
		t.Fatalf("Ranges returned error %v", err)
	}
	got := names(t, b, opts.Include)
	slices.Sort(got)
	got = slices.Compact(got)
	if want := []string{"c3", "c5", "c6"}; !slices.Equal(got, want) {
		t.Errorf("Ranges collected %v, want %v", got, want)
	}
}

func TestRangesAllWithoutHeadUsesOnlyReferences(t *testing.T) {
	b := parseFixture(t)
	delete(b.refs.values, refs.HEAD)
	opts, err := Ranges([]string{"--all"}, b.context())
	if err != nil {
		t.Fatalf("Ranges returned error %v", err)
	}
	if len(opts.Include) != 6 {
		t.Errorf("Ranges collected %v, want one entry per reference", names(t, b, opts.Include))
	}
}

func TestRangesSkipsReferencesThatAreNotCommits(t *testing.T) {
	b := parseFixture(t)
	name := refs.TagName("blobtag")
	b.refs.values[name] = refs.Ref{Name: name, Target: b.blob("one")}
	opts, err := Ranges([]string{"--tags"}, b.context())
	if err != nil {
		t.Fatalf("Ranges returned error %v", err)
	}
	if got := names(t, b, opts.Include); !slices.Equal(got, []string{"c3", "c5"}) {
		t.Errorf("Ranges collected %v, want the commit tags only", got)
	}
}

func TestRangesRejectsMalformedArguments(t *testing.T) {
	tests := []struct {
		name  string
		specs []string
		want  error
	}{
		{"unknown option", []string{"--pretty"}, ErrSyntax},
		{"glob without a pattern", []string{"--glob"}, ErrSyntax},
		{"unknown revision", []string{"nosuch"}, ErrNotFound},
		{"unknown exclusion", []string{"^nosuch"}, ErrNotFound},
		{"unknown left side", []string{"nosuch..main"}, ErrNotFound},
		{"unknown right side", []string{"main..nosuch"}, ErrNotFound},
		{"unknown symmetric left side", []string{"nosuch...main"}, ErrNotFound},
		{"unknown symmetric right side", []string{"main...nosuch"}, ErrNotFound},
		{"tree instead of a commit", []string{"main^{tree}"}, ErrNotCommit},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			b := parseFixture(t)
			if _, err := Ranges(test.specs, b.context()); !errors.Is(err, test.want) {
				t.Fatalf("Ranges(%v) returned %v, want %v", test.specs, err, test.want)
			}
		})
	}
}

func TestRangesReportsReferenceErrors(t *testing.T) {
	for _, specs := range [][]string{{"--all"}, {"--branches"}} {
		b := parseFixture(t)
		broken := errors.New("refs are broken")
		b.refs.err = broken
		if _, err := Ranges(specs, b.context()); !errors.Is(err, broken) {
			t.Fatalf("Ranges(%v) returned %v, want %v", specs, err, broken)
		}
	}
}

func TestRangesReportsMissingObjectsBehindReferences(t *testing.T) {
	b := parseFixture(t)
	b.objects.fail[b.id("c6")] = errors.New("gone")
	if _, err := Ranges([]string{"--all"}, b.context()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Ranges returned %v, want %v", err, ErrNotFound)
	}
}

func TestRangesFeedTheWalk(t *testing.T) {
	b := parseFixture(t)
	opts, err := Ranges([]string{"topic..main"}, b.context())
	if err != nil {
		t.Fatalf("Ranges returned error %v", err)
	}
	got := collect(t, b, Walk(t.Context(), opts))
	if want := []string{"c6", "c3"}; !slices.Equal(got, want) {
		t.Errorf("Walk visited %v, want %v", got, want)
	}
}

func TestGlobMatchHandlesPatterns(t *testing.T) {
	tests := []struct {
		pattern string
		text    string
		want    bool
	}{
		{"", "", true},
		{"", "main", false},
		{"main", "main", true},
		{"main", "mai", false},
		{"ma?n", "main", true},
		{"ma?n", "ma/n", false},
		{"*", "main", true},
		{"*", "topic/main", false},
		{"**", "topic/main", true},
		{"topic/*", "topic/main", true},
		{"*/main", "topic/deep/main", false},
		{"**/main", "topic/deep/main", true},
		{"main*", "main", true},
		{"x*", "main", false},
	}
	for _, test := range tests {
		t.Run(test.pattern+" "+test.text, func(t *testing.T) {
			if got := globMatch(test.pattern, test.text); got != test.want {
				t.Errorf("globMatch(%q, %q) = %v, want %v", test.pattern, test.text, got, test.want)
			}
		})
	}
}

func TestRangesReportsFailuresWhileComputingMergeBases(t *testing.T) {
	b := parseFixture(t)
	b.objects.fail[b.id("c3")] = errors.New("gone")
	if _, err := Ranges([]string{"topic...main"}, b.context()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Ranges returned %v, want %v", err, ErrNotFound)
	}
}
