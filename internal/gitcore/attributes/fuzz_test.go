package attributes

import (
	"strings"
	"testing"
)

var fuzzPaths = []Path{
	{Name: "a.txt"},
	{Name: "sub", IsDir: true},
	{Name: "sub/b.log"},
	{Name: "sub/deep/c.tmp"},
	{Name: "space file.txt"},
	{Name: "#hash"},
	{Name: "build", IsDir: true},
	{Name: ""},
}

func FuzzIgnorePattern(f *testing.F) {
	seeds := []string{
		"",
		"#comment\n",
		"*.log\n!important.log\n",
		"build/\n",
		"/rootonly\n",
		"doc/**/draft.txt\n",
		"**/tmp\n",
		"space\\ file.txt\n",
		"\\#hash\n\\!bang\n",
		"a**b\n",
		"[Cc]ache\n",
		"trailing   \n",
		"\r\n",
		utf8BOM + "foo\n",
		"[\n",
		"a[b-\n",
		"!\n",
		"/\n",
		"**\n",
		"a/**/**/b\n",
	}
	for _, seed := range seeds {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		rules := parseIgnoreFile(".gitignore", "sub", data)
		for _, rule := range rules {
			if !rule.Valid() {
				t.Fatalf("rule %+v parsed from %q is not valid", rule, data)
			}
			again := parsePattern(rule.String(), rule.pat.base)
			if again != rule.pat {
				t.Fatalf("pattern %q does not survive a round trip: %+v vs %+v", rule.String(), again, rule.pat)
			}
		}
		matcher := testMatcher(map[string]string{
			".gitignore":          string(data),
			"sub/.gitignore":      string(data),
			"sub/deep/.gitignore": string(data),
		}, IgnoreOptions{})
		for match, err := range matcher.Check(fuzzPaths) {
			if err != nil {
				t.Fatalf("Check(%q) returned error %v", match.Path, err)
			}
			if match.Ignored && !match.Rule.Valid() {
				t.Fatalf("%q is ignored without a rule", match.Path)
			}
			if match.Rule.Valid() && match.Rule.Negative && match.Ignored {
				t.Fatalf("%q is ignored by the negative rule %q", match.Path, match.Rule.String())
			}
		}
	})
}

func FuzzAttributesLine(f *testing.F) {
	seeds := []string{
		"",
		"# comment",
		"* text=auto",
		"*.bin binary",
		`"sp ace.txt" diff=spaced`,
		"[attr]binary -diff -merge -text",
		"[attr]bad*name -diff",
		"a.txt !text",
		"a.txt -text=ignored",
		"a.txt text eol=crlf",
		"!negative x",
		"a.txt\tx\t-y",
		`"broken x`,
		`"\101" x`,
		"a.txt =",
		"a.txt " + strings.Repeat("x", maxAttributesLine),
		"sub/** merge=z",
		"onlydir/ diff=d",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, line string) {
		parsed, ok := parseAttributesLine(line, ".gitattributes", "", 1, true)
		if ok {
			if parsed.macro != "" && !attributeNameValid(parsed.macro) {
				t.Fatalf("macro %q from %q has an invalid name", parsed.macro, line)
			}
			if parsed.macro == "" && parsed.pat.negative {
				t.Fatalf("line %q produced a negative pattern", line)
			}
			for _, state := range parsed.states {
				if !attributeNameValid(state.name) {
					t.Fatalf("attribute %q from %q has an invalid name", state.name, line)
				}
			}
		}
		attrs := testAttributes(map[string]string{
			".gitattributes":     line + "\n",
			"sub/.gitattributes": line + "\n",
		}, AttributeOptions{})
		for _, path := range fuzzPaths {
			name := path.Name
			if path.IsDir {
				name += "/"
			}
			for attribute, value := range attrs.Get(name) {
				if !attributeNameValid(attribute) {
					t.Fatalf("resolved attribute %q of %q is not a valid name", attribute, name)
				}
				if value.IsUnspecified() {
					t.Fatalf("attribute %q of %q is listed while unspecified", attribute, name)
				}
			}
			attrs.Text(name)
			attrs.Binary(name)
			attrs.Diff(name)
			attrs.Merge(name)
			attrs.Filter(name)
		}
	})
}
