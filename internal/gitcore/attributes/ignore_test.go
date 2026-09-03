package attributes

import (
	"errors"
	"slices"
	"strconv"
	"testing"
)

func ruleStrings(rules []Rule) []string {
	out := make([]string, len(rules))
	for i, r := range rules {
		out[i] = strconv.Itoa(r.Line) + ":" + r.String()
	}
	return out
}

func TestParseIgnoreFileFollowsGitLineRules(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{"empty", "", nil},
		{"comment", "# comment\n", nil},
		{"blankLines", "\n\n", nil},
		{"escapedHashIsPattern", "\\#comment\n", []string{`1:\#comment`}},
		{"lineNumbersCountBlanks", "\n#c\nfoo\n", []string{"3:foo"}},
		{"negation", "!foo\n", []string{"1:!foo"}},
		{"directoryOnly", "foo/\n", []string{"1:foo/"}},
		{"negatedDirectory", "!foo/\n", []string{"1:!foo/"}},
		{"trailingSpacesDropped", "foo   \n", []string{"1:foo"}},
		{"escapedTrailingSpaceKept", "foo\\ \n", []string{`1:foo\ `}},
		{"carriageReturnStripped", "foo\r\n", []string{"1:foo"}},
		{"carriageReturnOnlyLine", "\r\n", []string{"1:"}},
		{"missingFinalNewline", "foo", []string{"1:foo"}},
		{"byteOrderMark", utf8BOM + "foo\n", []string{"1:foo"}},
		{"tabsAreNotTrimmed", "foo\t\n", []string{"1:foo\t"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ruleStrings(parseIgnoreFile(".gitignore", "", []byte(tc.input)))
			if !slices.Equal(got, tc.want) {
				t.Fatalf("parseIgnoreFile(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestRuleStringIsEmptyWhenUnset(t *testing.T) {
	var rule Rule
	if rule.Valid() || rule.String() != "" {
		t.Fatalf("zero rule reports Valid %v and String %q", rule.Valid(), rule.String())
	}
}

func testMatcher(files map[string]string, opts IgnoreOptions) *Matcher {
	opts.Work = MemoryLoader(files)
	opts.Global = MemoryLoader(files)
	return NewMatcher(opts)
}

var ignoreFiles = map[string]string{
	".gitignore": "*.log\n!important.log\nbuild/\n/rootonly.txt\ndoc/**/draft.txt\n**/tmp\n" +
		"space\\ file.txt\n[Cc]ache\na**b.txt\n",
	"sub/.gitignore":      "keep.txt\n!keep.txt\nnested/\n/anchored.txt\n",
	"sub/deep/.gitignore": "*.tmp\n!special.tmp\n",
	"build/.gitignore":    "!kept.me\n",
	"info/exclude":        "info-only.txt\n*.excl\n",
	"global/ignore":       "global-only.txt\n*.excl\nglobal-only.txt\n",
}

func TestIgnoredFollowsGitPrecedence(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		isDir   bool
		ignored bool
		rule    string
	}{
		{"suffixPattern", "a.log", false, true, ".gitignore:1:*.log"},
		{"negationWins", "important.log", false, false, ".gitignore:2:!important.log"},
		{"directoryPattern", "build", true, true, ".gitignore:3:build/"},
		{"directoryPatternSkipsFile", "build", false, false, ""},
		{"childOfExcludedDirectory", "build/kept.me", false, true, ".gitignore:3:build/"},
		{"grandchildOfExcludedDirectory", "build/x/y.txt", false, true, ".gitignore:3:build/"},
		{"directoryInsideExcludedDirectory", "build/x", true, true, ".gitignore:3:build/"},
		{"anchoredAtRoot", "rootonly.txt", false, true, ".gitignore:4:/rootonly.txt"},
		{"anchoredAtRootMissesNested", "sub/rootonly.txt", false, false, ""},
		{"doubleStarInside", "doc/x/y/draft.txt", false, true, ".gitignore:5:doc/**/draft.txt"},
		{"doubleStarMatchesNothing", "doc/draft.txt", false, true, ".gitignore:5:doc/**/draft.txt"},
		{"leadingDoubleStar", "a/b/tmp", false, true, ".gitignore:6:**/tmp"},
		{"escapedSpace", "space file.txt", false, true, `.gitignore:7:space\ file.txt`},
		{"characterClass", "Cache", false, true, ".gitignore:8:[Cc]ache"},
		{"doubleStarInBasename", "axxb.txt", false, true, ".gitignore:9:a**b.txt"},
		{"nestedFileOverridesParent", "sub/keep.txt", false, false, "sub/.gitignore:2:!keep.txt"},
		{"nestedAnchored", "sub/anchored.txt", false, true, "sub/.gitignore:4:/anchored.txt"},
		{"nestedAnchoredMissesDeeper", "sub/deep/anchored.txt", false, false, ""},
		{"deepestFileWins", "sub/deep/x.tmp", false, true, "sub/deep/.gitignore:1:*.tmp"},
		{"deepestNegationWins", "sub/deep/special.tmp", false, false, "sub/deep/.gitignore:2:!special.tmp"},
		{"parentPatternStillApplies", "sub/deep/a.log", false, true, ".gitignore:1:*.log"},
		{"infoExclude", "info-only.txt", false, true, "info/exclude:1:info-only.txt"},
		{"infoExcludeBeatsExcludesFile", "x.excl", false, true, "info/exclude:2:*.excl"},
		{"excludesFile", "global-only.txt", false, true, "global/ignore:3:global-only.txt"},
		{"noMatch", "plain.txt", false, false, ""},
		{"emptyPath", "", false, false, ""},
		{"trailingSlashMeansDirectory", "build/", false, true, ".gitignore:3:build/"},
		{"currentDirectoryPrefix", "./a.log", false, true, ".gitignore:1:*.log"},
	}
	matcher := testMatcher(ignoreFiles, IgnoreOptions{
		InfoExclude:  "info/exclude",
		ExcludesFile: "global/ignore",
	})
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ignored, rule := matcher.Ignored(tc.path, tc.isDir)
			got := ""
			if rule.Valid() {
				got = rule.Source + ":" + strconv.Itoa(rule.Line) + ":" + rule.String()
			}
			if ignored != tc.ignored || got != tc.rule {
				t.Fatalf("Ignored(%q, %v) = (%v, %q), want (%v, %q)",
					tc.path, tc.isDir, ignored, got, tc.ignored, tc.rule)
			}
		})
	}
}

func TestIgnoredFoldsCaseWhenRequested(t *testing.T) {
	files := map[string]string{".gitignore": "*.log\nBUILD/\n"}
	sensitive := testMatcher(files, IgnoreOptions{})
	folded := testMatcher(files, IgnoreOptions{IgnoreCase: true})
	if ignored, _ := sensitive.Ignored("a.LOG", false); ignored {
		t.Fatal("case sensitive matcher ignored a.LOG")
	}
	if ignored, _ := folded.Ignored("a.LOG", false); !ignored {
		t.Fatal("case folding matcher did not ignore a.LOG")
	}
	if ignored, _ := folded.Ignored("build/inside", false); !ignored {
		t.Fatal("case folding matcher did not ignore a file below build/")
	}
}

func TestIgnoredKeepsNegatedDirectoryReadable(t *testing.T) {
	matcher := testMatcher(map[string]string{
		".gitignore":      "*\n!keep/\n",
		"keep/.gitignore": "!inside.txt\n",
	}, IgnoreOptions{})
	if ignored, rule := matcher.Ignored("keep", true); ignored {
		t.Fatalf("Ignored(keep, true) = true with rule %q, want false", rule.String())
	}
	if ignored, rule := matcher.Ignored("keep/inside.txt", false); ignored {
		t.Fatalf("Ignored(keep/inside.txt) = true with rule %q, want false", rule.String())
	}
}

func TestMatcherUsesConfiguredPerDirectoryFile(t *testing.T) {
	matcher := testMatcher(map[string]string{
		".ignoreme":     "*.log\n",
		"sub/.ignoreme": "*.tmp\n",
	}, IgnoreOptions{PerDirectory: ".ignoreme"})
	if ignored, _ := matcher.Ignored("sub/a.tmp", false); !ignored {
		t.Fatal("the configured per directory file was not read")
	}
	if ignored, _ := matcher.Ignored("sub/a.log", false); !ignored {
		t.Fatal("the configured per directory file was not read at the root")
	}
}

func TestLookupReportsLoaderErrors(t *testing.T) {
	matcher := NewMatcher(IgnoreOptions{
		Work:   failingLoader("sub/.gitignore"),
		Global: failingLoader("info/exclude"),
	})
	matcher.opts.InfoExclude = "info/exclude"
	if _, err := matcher.Lookup("sub/a.txt", false); !errors.Is(err, errLoader) {
		t.Fatalf("Lookup returned %v, want %v", err, errLoader)
	}
	if _, err := matcher.Lookup("sub/deep/a.txt", false); !errors.Is(err, errLoader) {
		t.Fatalf("Lookup below a failing directory returned %v, want %v", err, errLoader)
	}
}

func TestLookupReportsExcludesFileErrors(t *testing.T) {
	matcher := NewMatcher(IgnoreOptions{
		Work:         MemoryLoader(nil),
		Global:       failingLoader("global/ignore"),
		ExcludesFile: "global/ignore",
	})
	if _, err := matcher.Lookup("a.txt", false); !errors.Is(err, errLoader) {
		t.Fatalf("Lookup returned %v, want %v", err, errLoader)
	}
}

func TestCheckVisitsDirectoriesBeforeTheirContents(t *testing.T) {
	matcher := testMatcher(ignoreFiles, IgnoreOptions{})
	paths := []Path{
		{Name: "build/kept.me"},
		{Name: "a.log"},
		{Name: "build"},
		{Name: "build", IsDir: true},
		{Name: "build", IsDir: true},
	}
	var order []string
	var ignored []bool
	for match, err := range matcher.Check(paths) {
		if err != nil {
			t.Fatalf("Check returned error %v", err)
		}
		order = append(order, match.Path+":"+strconv.FormatBool(match.IsDir))
		ignored = append(ignored, match.Ignored)
	}
	want := []string{"a.log:false", "build:true", "build:true", "build:false", "build/kept.me:false"}
	if !slices.Equal(order, want) {
		t.Fatalf("Check visited %q, want %q", order, want)
	}
	if !slices.Equal(ignored, []bool{true, true, true, false, true}) {
		t.Fatalf("Check reported %v, want [true true true false true]", ignored)
	}
}

func TestCheckStopsWhenTheCallerBreaks(t *testing.T) {
	matcher := testMatcher(ignoreFiles, IgnoreOptions{})
	seen := 0
	for range matcher.Check([]Path{{Name: "a.log"}, {Name: "b.log"}, {Name: "c.log"}}) {
		seen++
		break
	}
	if seen != 1 {
		t.Fatalf("Check yielded %d matches after a break, want 1", seen)
	}
}

func TestCheckSurfacesLoaderErrors(t *testing.T) {
	matcher := NewMatcher(IgnoreOptions{Work: failingLoader(".gitignore")})
	for _, err := range matcher.Check([]Path{{Name: "a.txt"}}) {
		if !errors.Is(err, errLoader) {
			t.Fatalf("Check returned %v, want %v", err, errLoader)
		}
	}
}
