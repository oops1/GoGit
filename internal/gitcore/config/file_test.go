package config

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestSetRewritesTextMinimally(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		key   string
		value string
		want  string
	}{
		{
			"replacesValueInPlace",
			"[core]\n\tbare = false\n",
			"core.bare", "true",
			"[core]\n\tbare = true\n",
		},
		{
			"keepsOriginalKeyCaseAndSpacing",
			"[http]\n\tsslVerify=false\n",
			"http.sslverify", "true",
			"[http]\n\tsslVerify=true\n",
		},
		{
			"keepsTrailingComment",
			"[core]\n\tbare = false # why\n",
			"core.bare", "true",
			"[core]\n\tbare = true # why\n",
		},
		{
			"addsSpaceAfterBareEquals",
			"[core]\n\tbare =\n",
			"core.bare", "true",
			"[core]\n\tbare = true\n",
		},
		{
			"turnsImplicitTrueIntoAssignment",
			"[core]\n\tbare\n",
			"core.bare", "false",
			"[core]\n\tbare = false\n",
		},
		{
			"appendsKeyAtEndOfExistingSection",
			"[core]\n\tbare = true\n[user]\n\tname = x\n",
			"core.filemode", "true",
			"[core]\n\tbare = true\n\tfilemode = true\n[user]\n\tname = x\n",
		},
		{
			"appendsAfterLastEntryOfSectionNotAfterComments",
			"[core]\n\tbare = true\n# note\n\n[user]\n\tname = x\n",
			"core.filemode", "true",
			"[core]\n\tbare = true\n\tfilemode = true\n# note\n\n[user]\n\tname = x\n",
		},
		{
			"appendsToLastMatchingSection",
			"[core]\n\ta = 1\n[user]\n\tname = x\n[core]\n\tb = 2\n",
			"core.c", "3",
			"[core]\n\ta = 1\n[user]\n\tname = x\n[core]\n\tb = 2\n\tc = 3\n",
		},
		{
			"appendsRightAfterEmptySectionHeader",
			"[core]\n[user]\n\tname = x\n",
			"core.bare", "true",
			"[core]\n\tbare = true\n[user]\n\tname = x\n",
		},
		{
			"createsMissingSectionAtEndOfFile",
			"[core]\n\tbare = true\n",
			"user.name", "Ann",
			"[core]\n\tbare = true\n[user]\n\tname = Ann\n",
		},
		{
			"createsSubsectionWithQuotesEscaped",
			"",
			`remote.a"b\c.url`, "git://x",
			"[remote \"a\\\"b\\\\c\"]\n\turl = git://x\n",
		},
		{
			"addsNewlineBeforeAppendedSection",
			"[core]\n\tbare = true",
			"user.name", "Ann",
			"[core]\n\tbare = true\n[user]\n\tname = Ann\n",
		},
		{
			"addsNewlineBeforeAppendedKey",
			"[core]\n\tbare = true",
			"core.filemode", "false",
			"[core]\n\tbare = true\n\tfilemode = false\n",
		},
		{
			"replaceAllCollapsesRepeatedKeys",
			"[a]\n\tb = 1\n\tc = x\n\tb = 2\n\tb = 3\n",
			"a.b", "9",
			"[a]\n\tb = 9\n\tc = x\n",
		},
		{
			"quotesValueWithLeadingBlank",
			"[a]\n\tb = x\n",
			"a.b", " y",
			"[a]\n\tb = \" y\"\n",
		},
		{
			"escapesControlCharacters",
			"[a]\n\tb = x\n",
			"a.b", "1\t2\n3\b4\\5\"6",
			"[a]\n\tb = 1\\t2\\n3\\b4\\\\5\\\"6\n",
		},
		{
			"quotesValueWithCommentCharacter",
			"[a]\n\tb = x\n",
			"a.b", "y#z",
			"[a]\n\tb = \"y#z\"\n",
		},
		{
			"writesEmptyValue",
			"[a]\n\tb = x\n",
			"a.b", "",
			"[a]\n\tb = \n",
		},
		{
			"matchesSubsectionCaseSensitively",
			"[a \"X\"]\n\tb = 1\n",
			"a.x.b", "2",
			"[a \"X\"]\n\tb = 1\n[a \"x\"]\n\tb = 2\n",
		},
		{
			"keepsSectionHeaderSharedWithEntry",
			"[a] b = 1\n",
			"a.b", "2",
			"[a] b = 2\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := mustParse(t, tc.text)
			if err := f.Set(tc.key, tc.value); err != nil {
				t.Fatalf("Set(%q) returned error %v", tc.key, err)
			}
			if got := string(f.Encode()); got != tc.want {
				t.Fatalf("Encode = %q, want %q", got, tc.want)
			}
			again := mustParse(t, tc.want)
			if got, ok := again.Get(tc.key); !ok || got != tc.value {
				t.Fatalf("reparsed Get(%q) = %q, %v, want %q", tc.key, got, ok, tc.value)
			}
		})
	}
}

func TestAddAppendsWithoutReplacing(t *testing.T) {
	f := mustParse(t, "[a]\n\tb = 1\n[c]\n\td = 2\n")
	if err := f.Add("a.b", "2"); err != nil {
		t.Fatalf("Add returned error %v", err)
	}
	if err := f.Add("e.f", "3"); err != nil {
		t.Fatalf("Add returned error %v", err)
	}
	want := "[a]\n\tb = 1\n\tb = 2\n[c]\n\td = 2\n[e]\n\tf = 3\n"
	if got := string(f.Encode()); got != want {
		t.Fatalf("Encode = %q, want %q", got, want)
	}
	if got := f.GetAll("a.b"); !slices.Equal(got, []string{"1", "2"}) {
		t.Fatalf("GetAll = %q", got)
	}
}

func TestUnsetRemovesLines(t *testing.T) {
	tests := []struct {
		name string
		text string
		key  string
		want string
	}{
		{"removesWholeLine", "[a]\n\tb = 1\n\tc = 2\n", "a.b", "[a]\n\tc = 2\n"},
		{"removesTrailingComment", "[a]\n\tb = 1 # x\n\tc = 2\n", "a.b", "[a]\n\tc = 2\n"},
		{"removesLastOfRepeatedKeys", "[a]\n\tb = 1\n\tb = 2\n", "a.b", "[a]\n\tb = 1\n"},
		{"removesLineWithoutTrailingNewline", "[a]\n\tb = 1", "a.b", "[a]\n"},
		{"keepsSectionHeaderOnSharedLine", "[a] b = 1\n[c]\n", "a.b", "[a] \n[c]\n"},
		{"removesContinuedValue", "[a]\n\tb = 1\\\n2\n\tc = 3\n", "a.b", "[a]\n\tc = 3\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := mustParse(t, tc.text)
			if err := f.Unset(tc.key); err != nil {
				t.Fatalf("Unset returned error %v", err)
			}
			if got := string(f.Encode()); got != tc.want {
				t.Fatalf("Encode = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestUnsetAllRemovesEveryOccurrence(t *testing.T) {
	f := mustParse(t, "[a]\n\tb = 1\n\tc = x\n\tb = 2\n[d]\n\tb = 3\n")
	if err := f.UnsetAll("a.b"); err != nil {
		t.Fatalf("UnsetAll returned error %v", err)
	}
	want := "[a]\n\tc = x\n[d]\n\tb = 3\n"
	if got := string(f.Encode()); got != want {
		t.Fatalf("Encode = %q, want %q", got, want)
	}
}

func TestRemoveSectionDropsHeaderAndBody(t *testing.T) {
	tests := []struct {
		name    string
		text    string
		section string
		want    string
	}{
		{
			"removesSectionWithComments",
			"[a]\n\tb = 1\n# note\n[c]\n\td = 2\n",
			"a",
			"[c]\n\td = 2\n",
		},
		{
			"removesEverySectionWithTheSameName",
			"[a]\n\tb = 1\n[c]\n\td = 2\n[a]\n\te = 3\n",
			"a",
			"[c]\n\td = 2\n",
		},
		{
			"removesSubsectionOnly",
			"[a \"x\"]\n\tb = 1\n[a]\n\tc = 2\n",
			"a.x",
			"[a]\n\tc = 2\n",
		},
		{
			"keepsEarlierHeaderOnSharedLine",
			"[a][b]\n\tc = 1\n[d]\n",
			"b",
			"[a][d]\n",
		},
		{
			"removesTrailingSection",
			"[a]\n\tb = 1\n[c]\n\td = 2\n",
			"c",
			"[a]\n\tb = 1\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := mustParse(t, tc.text)
			if err := f.RemoveSection(tc.section); err != nil {
				t.Fatalf("RemoveSection returned error %v", err)
			}
			if got := string(f.Encode()); got != tc.want {
				t.Fatalf("Encode = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRenameSectionRewritesHeaders(t *testing.T) {
	f := mustParse(t, "[branch \"old\"]\n\tremote = o\n[x]\n\ty = 1\n[branch \"old\"]\n\tmerge = m\n")
	if err := f.RenameSection("branch.old", "branch.new"); err != nil {
		t.Fatalf("RenameSection returned error %v", err)
	}
	want := "[branch \"new\"]\n\tremote = o\n[x]\n\ty = 1\n[branch \"new\"]\n\tmerge = m\n"
	if got := string(f.Encode()); got != want {
		t.Fatalf("Encode = %q, want %q", got, want)
	}
	if v, ok := f.Get("branch.new.remote"); !ok || v != "o" {
		t.Fatalf("Get after rename = %q, %v", v, ok)
	}
}

func TestRenameSectionDropsSubsection(t *testing.T) {
	f := mustParse(t, "[a \"x\"]\n\tb = 1\n")
	if err := f.RenameSection("a.x", "z"); err != nil {
		t.Fatalf("RenameSection returned error %v", err)
	}
	if got := string(f.Encode()); got != "[z]\n\tb = 1\n" {
		t.Fatalf("Encode = %q", got)
	}
}

func TestMutatorsRejectBadNames(t *testing.T) {
	f := mustParse(t, "[a]\n\tb = 1\n")
	tests := []struct {
		name string
		call func() error
		want error
	}{
		{"setWithoutSection", func() error { return f.Set("b", "1") }, ErrInvalidName},
		{"addWithoutSection", func() error { return f.Add("b", "1") }, ErrInvalidName},
		{"unsetWithoutSection", func() error { return f.Unset("b") }, ErrInvalidName},
		{"unsetAllWithoutSection", func() error { return f.UnsetAll("b") }, ErrInvalidName},
		{"removeSectionEmpty", func() error { return f.RemoveSection("") }, ErrInvalidSection},
		{"renameSectionBadSource", func() error { return f.RenameSection("", "b") }, ErrInvalidSection},
		{"renameSectionBadTarget", func() error { return f.RenameSection("a", "") }, ErrInvalidSection},
		{"unsetMissingKey", func() error { return f.Unset("a.z") }, ErrNotFound},
		{"unsetAllMissingKey", func() error { return f.UnsetAll("a.z") }, ErrNotFound},
		{"removeMissingSection", func() error { return f.RemoveSection("zz") }, ErrSectionNotFound},
		{"renameMissingSection", func() error { return f.RenameSection("zz", "yy") }, ErrSectionNotFound},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestParseNameAcceptsAndRejects(t *testing.T) {
	tests := []struct {
		name string
		key  string
		want name
		ok   bool
	}{
		{"sectionAndKey", "Core.Bare", name{section: "core", key: "bare"}, true},
		{"withSubsection", "remote.Origin.URL", name{section: "remote", sub: "Origin", hasSub: true, key: "url"}, true},
		{"subsectionWithDots", "http.https://a.b/.proxy", name{section: "http", sub: "https://a.b/", hasSub: true, key: "proxy"}, true},
		{"emptySubsection", "a..b", name{section: "a", sub: "", hasSub: true, key: "b"}, true},
		{"noDot", "bare", name{}, false},
		{"leadingDot", ".bare", name{}, false},
		{"trailingDot", "core.", name{}, false},
		{"badSectionChar", "co re.bare", name{}, false},
		{"keyStartsWithDigit", "core.1bare", name{}, false},
		{"keyHasBadChar", "core.ba re", name{}, false},
		{"subsectionWithNewline", "a.x\ny.b", name{}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseName(tc.key)
			if tc.ok != (err == nil) {
				t.Fatalf("parseName(%q) error = %v, want ok=%v", tc.key, err, tc.ok)
			}
			if tc.ok && got != tc.want {
				t.Fatalf("parseName(%q) = %+v, want %+v", tc.key, got, tc.want)
			}
		})
	}
}

func TestParseSectionNameRejectsBadInput(t *testing.T) {
	for _, s := range []string{"", "a b", "a.x\ny", "a_b"} {
		if _, err := parseSectionName(s); !errors.Is(err, ErrInvalidSection) {
			t.Errorf("parseSectionName(%q) error = %v", s, err)
		}
	}
	got, err := parseSectionName("A.Sub.Part")
	if err != nil {
		t.Fatalf("parseSectionName returned error %v", err)
	}
	if want := (name{section: "a", sub: "Sub.Part", hasSub: true}); got != want {
		t.Fatalf("parseSectionName = %+v, want %+v", got, want)
	}
}

func TestNeedQuoteDecisions(t *testing.T) {
	tests := []struct {
		value string
		want  bool
	}{
		{"", false},
		{"plain", false},
		{" lead", true},
		{"trail ", true},
		{"a;b", true},
		{"a#b", true},
		{"a\rb", true},
		{"a\vb", true},
		{"a\fb", true},
		{"a\tb", false},
	}
	for _, tc := range tests {
		if got := needQuote(tc.value); got != tc.want {
			t.Errorf("needQuote(%q) = %v, want %v", tc.value, got, tc.want)
		}
	}
}

func TestSaveWritesAtomicallyAndReloads(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "config")
	f := mustParse(t, "[a]\n\tb = 1\n")
	if err := f.Save(path); err != nil {
		t.Fatalf("Save returned error %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile returned error %v", err)
	}
	if string(data) != "[a]\n\tb = 1\n" {
		t.Fatalf("saved %q", data)
	}
	if _, err := os.Stat(path + ".lock"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("lock file still present: %v", err)
	}
}

func TestSaveFailsWhenLockExists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	writeFile(t, path+".lock", "")
	f := mustParse(t, "")
	if err := f.Save(path); err == nil {
		t.Fatal("Save succeeded while the lock file existed")
	}
}

func TestSaveFailsWhenTargetIsDirectory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatalf("Mkdir returned error %v", err)
	}
	f := mustParse(t, "")
	if err := f.Save(path); err == nil {
		t.Fatal("Save succeeded while the target was a directory")
	}
	if _, err := os.Stat(path + ".lock"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("lock file was not cleaned up: %v", err)
	}
}

func TestSaveFailsWhenParentIsFile(t *testing.T) {
	dir := t.TempDir()
	parent := writeFile(t, filepath.Join(dir, "file"), "x")
	f := mustParse(t, "")
	if err := f.Save(filepath.Join(parent, "config")); err == nil {
		t.Fatal("Save succeeded below a regular file")
	}
}

func TestFileGettersReadTypedValues(t *testing.T) {
	f := mustParse(t, "[a]\n\tflag\n\tnum = 2k\n\tpath = ~/x\n\tstr = v\n\tstr = w\n")
	t.Setenv("HOME", filepath.ToSlash(t.TempDir()))
	if v, ok := f.Get("a.str"); !ok || v != "w" {
		t.Errorf("Get = %q, %v", v, ok)
	}
	if _, ok := f.Get("a.missing"); ok {
		t.Error("Get found a missing key")
	}
	if _, ok := f.Get("nodot"); ok {
		t.Error("Get accepted an invalid name")
	}
	if got := f.GetAll("a.str"); !slices.Equal(got, []string{"v", "w"}) {
		t.Errorf("GetAll = %q", got)
	}
	if !f.Has("a.flag") || f.Has("a.nope") {
		t.Error("Has returned the wrong answer")
	}
	if v, err := f.GetBool("a.flag"); err != nil || !v {
		t.Errorf("GetBool = %v, %v", v, err)
	}
	if v, err := f.GetInt("a.num"); err != nil || v != 2048 {
		t.Errorf("GetInt = %v, %v", v, err)
	}
	got, err := f.GetPath("a.path")
	if err != nil {
		t.Fatalf("GetPath returned error %v", err)
	}
	if want := filepath.Join(os.Getenv("HOME"), "x"); got != want {
		t.Errorf("GetPath = %q, want %q", got, want)
	}
	if f.Path() != "" {
		t.Errorf("Path = %q, want empty", f.Path())
	}
}

func TestEncodeOfEmptyFileIsEmpty(t *testing.T) {
	f := mustParse(t, "")
	if got := f.Encode(); len(got) != 0 {
		t.Fatalf("Encode = %q", got)
	}
}
