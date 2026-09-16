package pathspec

import (
	"errors"
	"strings"
	"testing"
)

func TestParseRejectsBrokenPathspecs(t *testing.T) {
	cases := []struct {
		spec string
		want error
	}{
		{"", ErrEmpty},
		{":(glob", ErrMagic},
		{":(nonsense)x", ErrMagic},
		{":(literal,glob)x", ErrMagic},
		{":(attr:foo)x", ErrUnsupportedMagic},
		{":(prefix:1)x", ErrUnsupportedMagic},
		{":#x", ErrUnsupportedMagic},
		{"/abs", ErrOutsideTree},
		{"../up", ErrOutsideTree},
		{"a/../../up", ErrOutsideTree},
	}
	for _, c := range cases {
		t.Run(c.spec, func(t *testing.T) {
			if _, err := Parse([]string{"ok", c.spec}); !errors.Is(err, c.want) {
				t.Fatalf("Parse(%q) returned %v instead of %v", c.spec, err, c.want)
			}
		})
	}
}

func TestParseItemReadsTheMagic(t *testing.T) {
	cases := []struct {
		spec  string
		match string
		magic Magic
	}{
		{"plain", "plain", 0},
		{":/top", "top", Top},
		{":!no", "no", Exclude},
		{":^no", "no", Exclude},
		{":/!:both", "both", Top | Exclude},
		{"::colon", "colon", 0},
		{":", "", 0},
		{":/", "", Top},
		{":(top,,icase)Dir", "Dir", Top | ICase},
		{":(exclude,literal)a*b", "a*b", Exclude | Literal},
		{":(glob)./a//b/.", "a/b/", Glob},
		{":(top)./kept", "./kept", Top},
		{"a/b/..", "a/", 0},
		{".", "", 0},
		{"a/./", "a/", 0},
	}
	for _, c := range cases {
		t.Run(c.spec, func(t *testing.T) {
			item, err := ParseItem(c.spec)
			if err != nil {
				t.Fatalf("ParseItem returned error %v", err)
			}
			if item.Match != c.match || item.Magic != c.magic {
				t.Fatalf("ParseItem gave %q with magic %b instead of %q with %b", item.Match, item.Magic, c.match, c.magic)
			}
		})
	}
}

func TestSetMatchesFilesLikeATreeWalk(t *testing.T) {
	cases := []struct {
		specs []string
		yes   []string
		no    []string
	}{
		{nil, []string{"anything", "a/b"}, nil},
		{[]string{"dir"}, []string{"dir", "dir/a", "dir/sub/b"}, []string{"dirx", "di", "other/dir"}},
		{[]string{"dir/"}, []string{"dir/a"}, []string{"dir"}},
		{[]string{"*.go"}, []string{"a.go", "x/y/b.go"}, []string{"a.goo", "go"}},
		{[]string{":(glob)*.go"}, []string{"a.go"}, []string{"x/b.go"}},
		{[]string{":(glob)**/*.go"}, []string{"a.go", "x/y/b.go"}, []string{"a.c"}},
		{[]string{"a?c"}, []string{"abc", "a/c"}, []string{"ac"}},
		{[]string{":(icase)DIR/*.TXT"}, []string{"dir/a.txt", "Dir/sub/B.Txt"}, []string{"dire/a.txt"}},
		{[]string{":(icase)ab"}, []string{"AB", "aB/c"}, []string{"abc", "Ac"}},
		{[]string{":(literal)a*"}, []string{"a*", "a*/x"}, []string{"ab"}},
		{[]string{":!*.go"}, []string{"a.c", "d/e"}, []string{"a.go"}},
		{[]string{"d", ":!d/x"}, []string{"d/y"}, []string{"d/x", "d/x/z", "e"}},
		{[]string{"x*.c"}, []string{"x.c", "xy/z.c"}, []string{"y.c", "x.h"}},
		{[]string{"abc*"}, []string{"abcd"}, []string{"ab"}},
		{[]string{":(icase)d?R/*.txt"}, []string{"DIR/A.TXT", "dar/x/y.txt"}, []string{"dir/a.md"}},
		{[]string{"[ab]*"}, []string{"a1", "b/c"}, []string{"c"}},
	}
	for _, c := range cases {
		t.Run(strings.Join(c.specs, " "), func(t *testing.T) {
			set, err := Parse(c.specs)
			if err != nil {
				t.Fatalf("Parse returned error %v", err)
			}
			for _, path := range c.yes {
				if !set.Match(path) {
					t.Errorf("%q should match", path)
				}
			}
			for _, path := range c.no {
				if set.Match(path) {
					t.Errorf("%q should not match", path)
				}
			}
		})
	}
}

func TestSetSaysWhichDirectoriesCanHoldMatches(t *testing.T) {
	cases := []struct {
		specs []string
		yes   []string
		no    []string
	}{
		{nil, []string{"any"}, nil},
		{[]string{"dir/sub/f"}, []string{"dir", "dir/sub"}, []string{"di", "dir/other", "dirx"}},
		{[]string{"dir"}, []string{"dir", "dir/sub"}, []string{"di", "dirx"}},
		{[]string{"dir/"}, []string{"dir", "dir/sub"}, []string{"dirx"}},
		{[]string{"src/*.go"}, []string{"src", "src/deep"}, []string{"lib"}},
		{[]string{"s*"}, []string{"src", "s/t"}, []string{"lib"}},
		{[]string{"*.go"}, []string{"anything"}, nil},
		{[]string{":(icase)SRC/x"}, []string{"src"}, []string{"lib"}},
		{[]string{":!src"}, []string{"src", "lib"}, nil},
	}
	for _, c := range cases {
		t.Run(strings.Join(c.specs, " "), func(t *testing.T) {
			set, err := Parse(c.specs)
			if err != nil {
				t.Fatalf("Parse returned error %v", err)
			}
			for _, dir := range c.yes {
				if !set.MayMatchUnder(dir) {
					t.Errorf("%q should be walked", dir)
				}
			}
			for _, dir := range c.no {
				if set.MayMatchUnder(dir) {
					t.Errorf("%q should be skipped", dir)
				}
			}
		})
	}
}

func TestAnEmptySetMatchesEverything(t *testing.T) {
	set, err := Parse(nil)
	if err != nil || !set.Empty() {
		t.Fatalf("Parse(nil) returned %+v, %v", set, err)
	}
	withSpec, err := Parse([]string{"x"})
	if err != nil || withSpec.Empty() {
		t.Fatalf("Parse(x) returned %+v, %v", withSpec, err)
	}
}
