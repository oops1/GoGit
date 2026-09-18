package submodule

import (
	"errors"
	"slices"
	"testing"
)

func mustParse(t *testing.T, text string) *Modules {
	t.Helper()
	modules, err := Parse([]byte(text))
	if err != nil {
		t.Fatalf("Parse returned error %v", err)
	}
	return modules
}

func TestParseReadsEverySettingOfAModule(t *testing.T) {
	modules := mustParse(t, "[submodule \"lib\"]\n"+
		"\tpath = libs/lib\n"+
		"\turl = ../lib.git\n"+
		"\tbranch = stable\n"+
		"\tupdate = rebase\n"+
		"\tignore = dirty\n"+
		"\tshallow = true\n"+
		"\tfetchRecurseSubmodules = false\n")

	got, ok := modules.ByPath("libs/lib")
	want := Module{
		Name:         "lib",
		Path:         "libs/lib",
		URL:          "../lib.git",
		Branch:       "stable",
		Update:       UpdateStrategy{Type: UpdateRebase},
		Ignore:       IgnoreDirty,
		Shallow:      true,
		ShallowSet:   true,
		FetchRecurse: FetchRecurseOff,
	}
	if !ok || got != want {
		t.Fatalf("ByPath = %+v, %v; want %+v", got, ok, want)
	}
	if byName, ok := modules.ByName("lib"); !ok || byName != want {
		t.Fatalf("ByName = %+v, %v", byName, ok)
	}
	if modules.Len() != 1 || !slices.Equal(modules.All(), []Module{want}) {
		t.Fatalf("All = %+v", modules.All())
	}
}

func TestParseKeepsTheOrderOfFirstAppearanceAndLetsLaterValuesWin(t *testing.T) {
	modules := mustParse(t, "[submodule \"b\"]\n\tpath = one\n"+
		"[submodule \"a\"]\n\tpath = two\n\tURL = first\n"+
		"[submodule \"b\"]\n\tpath = three\n"+
		"[submodule \"a\"]\n\turl = second\n\tshallow\n")

	names := []string{}
	for _, m := range modules.All() {
		names = append(names, m.Name)
	}
	if !slices.Equal(names, []string{"b", "a"}) {
		t.Fatalf("order = %v", names)
	}
	if _, ok := modules.ByPath("one"); ok {
		t.Fatal("a moved path still maps to its module")
	}
	if b, ok := modules.ByPath("three"); !ok || b.Name != "b" {
		t.Fatalf("ByPath(three) = %+v, %v", b, ok)
	}
	if a, _ := modules.ByName("a"); a.URL != "second" || !a.Shallow || !a.ShallowSet {
		t.Fatalf("a = %+v", a)
	}
	if _, ok := modules.ByName("missing"); ok {
		t.Fatal("ByName found a module that does not exist")
	}
}

func TestParseSkipsSuspiciousNamesOptionLikeValuesAndOtherSections(t *testing.T) {
	modules := mustParse(t, "[core]\n\tbare = false\n"+
		"[submodule]\n\tpath = top\n"+
		"[submodule \"../escape\"]\n\tpath = escape\n"+
		"[submodule \"ok\"]\n\tpath = -rf\n\turl = --upload-pack=evil\n\tignore = sometimes\n")

	if modules.Len() != 1 {
		t.Fatalf("Len = %d, want only the allowed name", modules.Len())
	}
	ok, _ := modules.ByName("ok")
	if ok.Path != "" || ok.URL != "" || ok.Ignore != IgnoreUnset {
		t.Fatalf("ok = %+v, want option-like and invalid values dropped", ok)
	}
	if _, found := modules.ByPath("-rf"); found {
		t.Fatal("an option-like path was mapped")
	}
}

func TestParseRejectsWhatGitRefusesToRead(t *testing.T) {
	for _, text := range []string{
		"[submodule \"a\"]\n\tpath\n",
		"[submodule \"a\"]\n\turl\n",
		"[submodule \"a\"]\n\tignore\n",
		"[submodule \"a\"]\n\tupdate\n",
		"[submodule \"a\"]\n\tbranch\n",
		"[submodule \"a\"]\n\tupdate = sideways\n",
		"[submodule \"a\"]\n\tupdate = !rm -rf .\n",
		"[submodule \"a\"]\n\tshallow = maybe\n",
		"[submodule \"a\"\n",
	} {
		if _, err := Parse([]byte(text)); !errors.Is(err, ErrInvalidGitmodules) {
			t.Fatalf("Parse(%q) returned %v, want ErrInvalidGitmodules", text, err)
		}
	}
}

func TestParseUpdateStrategyFollowsGit(t *testing.T) {
	tests := []struct {
		value string
		want  UpdateStrategy
		err   bool
	}{
		{"checkout", UpdateStrategy{Type: UpdateCheckout}, false},
		{"rebase", UpdateStrategy{Type: UpdateRebase}, false},
		{"merge", UpdateStrategy{Type: UpdateMerge}, false},
		{"none", UpdateStrategy{Type: UpdateNone}, false},
		{"!make", UpdateStrategy{Type: UpdateCommand, Command: "make"}, false},
		{"Checkout", UpdateStrategy{}, true},
	}
	for _, tt := range tests {
		got, err := ParseUpdateStrategy(tt.value)
		if got != tt.want || (err != nil) != tt.err || tt.err && !errors.Is(err, ErrInvalidUpdate) {
			t.Fatalf("ParseUpdateStrategy(%q) = %+v, %v", tt.value, got, err)
		}
	}
}

func TestUpdateTypeNamesMatchConfigValues(t *testing.T) {
	names := map[UpdateType]string{
		UpdateUnspecified: "",
		UpdateCheckout:    "checkout",
		UpdateRebase:      "rebase",
		UpdateMerge:       "merge",
		UpdateNone:        "none",
		UpdateCommand:     "command",
	}
	for kind, want := range names {
		if got := kind.String(); got != want {
			t.Fatalf("%d.String() = %q, want %q", kind, got, want)
		}
	}
}

func TestParseIgnoreAcceptsOnlyGitModes(t *testing.T) {
	for _, value := range []string{"none", "untracked", "dirty", "all"} {
		if got, err := ParseIgnore(value); err != nil || string(got) != value {
			t.Fatalf("ParseIgnore(%q) = %q, %v", value, got, err)
		}
	}
	if _, err := ParseIgnore("All"); !errors.Is(err, ErrInvalidIgnore) {
		t.Fatalf("ParseIgnore(All) returned %v", err)
	}
}

func TestNameAllowedRejectsDotDotComponents(t *testing.T) {
	tests := map[string]bool{
		"":          false,
		"..":        false,
		"a/../b":    false,
		`a\..\b`:    false,
		"..a":       true,
		"a..":       true,
		"libs/core": true,
	}
	for name, want := range tests {
		if got := NameAllowed(name); got != want {
			t.Fatalf("NameAllowed(%q) = %v, want %v", name, got, want)
		}
	}
}
