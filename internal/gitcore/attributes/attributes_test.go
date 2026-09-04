package attributes

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestValueDescribesItsKind(t *testing.T) {
	tests := []struct {
		name          string
		value         Value
		kind          Kind
		text          string
		printed       string
		set           bool
		unset         bool
		unspecified   bool
		printedAsText bool
	}{
		{name: "set", value: SetValue(), kind: Set, printed: "set", set: true},
		{name: "unset", value: UnsetValue(), kind: Unset, printed: "unset", unset: true},
		{name: "unspecified", value: UnspecifiedValue(), kind: Unspecified, printed: "unspecified", unspecified: true},
		{name: "text", value: TextValue("auto"), kind: Valued, text: "auto", printed: "auto", printedAsText: true},
		{name: "zero", value: Value{}, kind: Unspecified, printed: "unspecified", unspecified: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			v := tc.value
			if v.Kind() != tc.kind || v.Text() != tc.text || v.String() != tc.printed {
				t.Fatalf("value %+v reports kind %v text %q string %q", v, v.Kind(), v.Text(), v.String())
			}
			if v.IsSet() != tc.set || v.IsUnset() != tc.unset || v.IsUnspecified() != tc.unspecified {
				t.Fatalf("value %+v reports set %v unset %v unspecified %v", v, v.IsSet(), v.IsUnset(), v.IsUnspecified())
			}
		})
	}
}

func lineSummary(lines []attrLine) []string {
	out := make([]string, len(lines))
	for i, line := range lines {
		var states []string
		for _, s := range line.states {
			states = append(states, s.name+"="+s.value.String())
		}
		name := line.pat.text
		if line.macro != "" {
			name = "[attr]" + line.macro
		}
		out[i] = name + " " + strings.Join(states, ",")
	}
	return out
}

func TestParseAttributesFileFollowsGitSyntax(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		macroOK bool
		want    []string
	}{
		{"empty", "", true, nil},
		{"comment", "# c\n", true, nil},
		{"leadingBlanksBeforeComment", "   # c\n", true, nil},
		{"blankLine", "   \n", true, nil},
		{"setAttribute", "a.txt attr\n", true, []string{"a.txt attr=set"}},
		{"unsetAttribute", "a.txt -attr\n", true, []string{"a.txt attr=unset"}},
		{"unspecifiedAttribute", "a.txt !attr\n", true, []string{"a.txt attr=unspecified"}},
		{"valuedAttribute", "a.txt attr=value\n", true, []string{"a.txt attr=value"}},
		{"emptyValue", "a.txt attr=\n", true, []string{"a.txt attr="}},
		{"unsetIgnoresValue", "a.txt -attr=value\n", true, []string{"a.txt attr=unset"}},
		{"severalAttributes", "a.txt x -y z=1\n", true, []string{"a.txt x=set,y=unset,z=1"}},
		{"tabsSeparate", "a.txt\tx\t-y\n", true, []string{"a.txt x=set,y=unset"}},
		{"carriageReturnIsBlank", "a.txt x\r\n", true, []string{"a.txt x=set"}},
		{"patternWithoutAttributes", "a.txt\n", true, []string{"a.txt "}},
		{"quotedPattern", `"sp ace.txt" x` + "\n", true, []string{"sp ace.txt x=set"}},
		{"unbalancedQuoteIsLiteral", `"broken x` + "\n", true, []string{`"broken x=set`}},
		{"macro", "[attr]bin -diff -text\n", true, []string{"[attr]bin diff=unset,text=unset"}},
		{"macroRejectedWhenNotAllowed", "[attr]bin -diff\n", false, nil},
		{"macroWithoutNameIsPattern", "[attr] x\n", true, []string{"[attr] x=set"}},
		{"quotedMacroNameStopsAtBlank", `"[attr]mac ro" -diff` + "\n", true, []string{"[attr]mac diff=unset"}},
		{"quotedMacroNameSkipsLeadingBlank", `"[attr] mac" -diff` + "\n", true, []string{"[attr]mac diff=unset"}},
		{"macroWithInvalidName", "[attr]b*d -diff\n", true, nil},
		{"negativePatternDropped", "!a.txt x\n", true, nil},
		{"invalidAttributeDropsLine", "a.txt bad*name\n", true, nil},
		{"invalidLeadingDashDropsLine", "a.txt --x\n", true, nil},
		{"lineTooLong", "a.txt " + strings.Repeat("x", maxAttributesLine) + "\n", true, nil},
		{"missingFinalNewline", "a.txt x", true, []string{"a.txt x=set"}},
		{"dottedAttributeName", "a.txt my.attr_1-2\n", true, []string{"a.txt my.attr_1-2=set"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := lineSummary(parseAttributesFile(".gitattributes", "", []byte(tc.input), tc.macroOK))
			if !slices.Equal(got, tc.want) {
				t.Fatalf("parseAttributesFile(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestAttributeNameValidAcceptsGitCharacterSet(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"empty", "", false},
		{"leadingDash", "-a", false},
		{"letters", "abcZ", true},
		{"digits", "a9", true},
		{"punctuation", "a.b_c-d", true},
		{"star", "a*", false},
		{"slash", "a/b", false},
		{"equals", "a=b", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := attributeNameValid(tc.input); got != tc.want {
				t.Fatalf("attributeNameValid(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

func testAttributes(files map[string]string, opts AttributeOptions) *Attributes {
	opts.Work = MemoryLoader(files)
	opts.Global = MemoryLoader(files)
	return New(opts)
}

var attributeFiles = map[string]string{
	".gitattributes": "* text=auto\n*.bin binary\n\"sp ace.txt\" diff=spaced\n" +
		"[attr]mymacro -text diff merge=custom\nsub/** merge=fromroot\n" +
		"onlydir/ diff=dironly\n*.c filter=indent ident\n",
	"sub/.gitattributes":      "a.bin -merge\nb.bin mymacro\n[attr]nested -diff\n",
	"sub/deep/.gitattributes": "x.txt diff=subdeep\n",
	"info/attributes":         "*.md text eol=crlf\ninfo.txt diff=frominfo\n",
	"global/attributes":       "*.glob diff=fromglobal\n* merge=fromglobal\n",
	"system/attributes":       "*.sys diff=fromsystem\n",
}

func attributesUnderTest() *Attributes {
	return testAttributes(attributeFiles, AttributeOptions{
		InfoFile:       "info/attributes",
		AttributesFile: "global/attributes",
		SystemFile:     "system/attributes",
	})
}

func TestGetResolvesAttributesInGitOrder(t *testing.T) {
	tests := []struct {
		name string
		path string
		want map[string]string
	}{
		{"rootPattern", "x.txt", map[string]string{"text": "auto", "merge": "fromglobal"}},
		{"builtinBinaryMacro", "a.bin", map[string]string{
			"binary": "set", "diff": "unset", "merge": "unset", "text": "unset",
		}},
		{"nearerFileWins", "sub/a.bin", map[string]string{
			"binary": "set", "diff": "unset", "merge": "unset", "text": "unset",
		}},
		{"userMacroExpands", "sub/b.bin", map[string]string{
			"binary": "set", "mymacro": "set", "diff": "set", "merge": "custom", "text": "unset",
		}},
		{"quotedPattern", "sp ace.txt", map[string]string{
			"diff": "spaced", "text": "auto", "merge": "fromglobal",
		}},
		{"infoFileWins", "r.md", map[string]string{
			"text": "set", "eol": "crlf", "merge": "fromglobal",
		}},
		{"infoFileWinsOverSubdirectory", "sub/r.md", map[string]string{
			"text": "set", "eol": "crlf", "merge": "fromroot",
		}},
		{"directoryOnlyPattern", "onlydir/", map[string]string{
			"diff": "dironly", "text": "auto", "merge": "fromglobal",
		}},
		{"directoryOnlyPatternSkipsFiles", "onlydir", map[string]string{
			"text": "auto", "merge": "fromglobal",
		}},
		{"severalAttributesOnOneLine", "a.c", map[string]string{
			"filter": "indent", "ident": "set", "text": "auto", "merge": "fromglobal",
		}},
		{"deepestFileWins", "sub/deep/x.txt", map[string]string{
			"diff": "subdeep", "text": "auto", "merge": "fromroot",
		}},
		{"globalFile", "a.glob", map[string]string{
			"diff": "fromglobal", "text": "auto", "merge": "fromglobal",
		}},
		{"systemFile", "a.sys", map[string]string{
			"diff": "fromsystem", "text": "auto", "merge": "fromglobal",
		}},
	}
	attrs := attributesUnderTest()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := map[string]string{}
			for name, value := range attrs.Get(tc.path) {
				got[name] = value.String()
			}
			if len(got) != len(tc.want) {
				t.Fatalf("Get(%q) = %v, want %v", tc.path, got, tc.want)
			}
			for name, want := range tc.want {
				if got[name] != want {
					t.Fatalf("Get(%q)[%q] = %q, want %q", tc.path, name, got[name], want)
				}
			}
		})
	}
}

func TestGetReportsUnspecifiedForNamedAttributes(t *testing.T) {
	attrs := attributesUnderTest()
	got := attrs.Get("x.txt", "text", "diff", "absent")
	if len(got) != 3 {
		t.Fatalf("Get returned %d attributes, want 3", len(got))
	}
	if got["text"].String() != "auto" || !got["diff"].IsUnspecified() || !got["absent"].IsUnspecified() {
		t.Fatalf("Get returned %v", got)
	}
}

func TestGetHidesExplicitlyUnspecifiedAttributes(t *testing.T) {
	attrs := testAttributes(map[string]string{
		".gitattributes": "* diff=fromall\na.txt !diff\n",
	}, AttributeOptions{})
	if _, ok := attrs.Get("a.txt")["diff"]; ok {
		t.Fatal("an attribute cancelled with ! is still listed")
	}
	if got := attrs.Get("b.txt")["diff"].String(); got != "fromall" {
		t.Fatalf("diff of b.txt is %q, want %q", got, "fromall")
	}
}

func TestGetCachesResolvedPathsSeparatelyForDirectories(t *testing.T) {
	attrs := testAttributes(map[string]string{
		".gitattributes": "d/ diff=dir\n",
	}, AttributeOptions{})
	if got := attrs.Get("d")["diff"]; got.Kind() != Unspecified {
		t.Fatalf("diff of the file d is %q, want unspecified", got)
	}
	if got := attrs.Get("d/")["diff"].String(); got != "dir" {
		t.Fatalf("diff of the directory d is %q, want %q", got, "dir")
	}
	if got := attrs.Get("d/")["diff"].String(); got != "dir" {
		t.Fatalf("cached diff of the directory d is %q, want %q", got, "dir")
	}
}

func TestMacrosDoNotRecurseForever(t *testing.T) {
	attrs := testAttributes(map[string]string{
		".gitattributes": "[attr]a a b\n[attr]b a\nx.txt a\n",
	}, AttributeOptions{})
	got := attrs.Get("x.txt")
	if got["a"].String() != "set" || got["b"].String() != "set" {
		t.Fatalf("Get returned %v, want a and b set", got)
	}
}

func TestMacrosAreTakenFromTheNearestDefinition(t *testing.T) {
	attrs := testAttributes(map[string]string{
		".gitattributes":  "[attr]m diff=root\nx.txt m\n",
		"info/attributes": "[attr]m diff=info\n",
	}, AttributeOptions{InfoFile: "info/attributes"})
	if got := attrs.Get("x.txt")["diff"].String(); got != "info" {
		t.Fatalf("diff of x.txt is %q, want %q", got, "info")
	}
}

func TestMacrosInSubdirectoriesAreRejected(t *testing.T) {
	attrs := testAttributes(map[string]string{
		".gitattributes":     "sub/x.txt m\n",
		"sub/.gitattributes": "[attr]m diff=sub\n",
	}, AttributeOptions{})
	got := attrs.Get("sub/x.txt")
	if got["m"].String() != "set" {
		t.Fatalf("m of sub/x.txt is %q, want set", got["m"])
	}
	if _, ok := got["diff"]; ok {
		t.Fatal("a macro declared in a subdirectory was expanded")
	}
}

func TestAttributesUseConfiguredPerDirectoryFile(t *testing.T) {
	attrs := testAttributes(map[string]string{
		"sub/.attrs": "x.txt diff=custom\n",
	}, AttributeOptions{PerDirectory: ".attrs"})
	if got := attrs.Get("sub/x.txt")["diff"].String(); got != "custom" {
		t.Fatalf("diff of sub/x.txt is %q, want %q", got, "custom")
	}
}

func TestAttributesFoldCaseWhenRequested(t *testing.T) {
	files := map[string]string{".gitattributes": "*.TXT diff=upper\n"}
	if got := testAttributes(files, AttributeOptions{}).Get("a.txt")["diff"]; !got.IsUnspecified() {
		t.Fatalf("case sensitive lookup returned %q", got)
	}
	folded := testAttributes(files, AttributeOptions{IgnoreCase: true})
	if got := folded.Get("a.txt")["diff"].String(); got != "upper" {
		t.Fatalf("case folding lookup returned %q, want %q", got, "upper")
	}
}

func TestLookupReportsAttributeLoaderErrors(t *testing.T) {
	attrs := New(AttributeOptions{
		Work:     failingLoader("sub/.gitattributes"),
		Global:   failingLoader("info/attributes"),
		InfoFile: "info/attributes",
	})
	if _, err := attrs.Lookup("sub/x.txt"); !errors.Is(err, errLoader) {
		t.Fatalf("Lookup returned %v, want %v", err, errLoader)
	}
	if _, err := attrs.Lookup("sub/deep/x.txt"); !errors.Is(err, errLoader) {
		t.Fatalf("Lookup below a failing directory returned %v, want %v", err, errLoader)
	}
}

func TestBinaryAndDriverHelpersReadAttributes(t *testing.T) {
	attrs := testAttributes(map[string]string{
		".gitattributes": "*.bin binary\n*.c diff=cpp merge=union filter=indent\n*.txt diff\n",
	}, AttributeOptions{})
	tests := []struct {
		name   string
		path   string
		binary bool
		diff   string
		merge  string
		filter string
	}{
		{"binaryMacro", "a.bin", true, "", "", ""},
		{"drivers", "a.c", false, "cpp", "union", "indent"},
		{"setWithoutDriverName", "a.txt", false, "", "", ""},
		{"unspecified", "a.md", false, "", "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := attrs.Binary(tc.path); got != tc.binary {
				t.Fatalf("Binary(%q) = %v, want %v", tc.path, got, tc.binary)
			}
			if got := attrs.Diff(tc.path); got != tc.diff {
				t.Fatalf("Diff(%q) = %q, want %q", tc.path, got, tc.diff)
			}
			if got := attrs.Merge(tc.path); got != tc.merge {
				t.Fatalf("Merge(%q) = %q, want %q", tc.path, got, tc.merge)
			}
			if got := attrs.Filter(tc.path); got != tc.filter {
				t.Fatalf("Filter(%q) = %q, want %q", tc.path, got, tc.filter)
			}
		})
	}
}
