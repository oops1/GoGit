package console

import (
	"errors"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/i18n"
)

var exampleOptionPattern = regexp.MustCompile(`^-{1,2}[A-Za-z][A-Za-z-]*$`)

func optionTablesOf(name string) []option {
	tables := map[string][][]option{
		"add":       {addOptions},
		"branch":    {branchOptions},
		"checkout":  {checkoutOptions},
		"commit":    {commitOptions},
		"config":    {configOptions},
		"diff":      {diffOptions},
		"fetch":     {fetchOptions},
		"log":       {logOptions},
		"merge":     {mergeOptions},
		"pull":      {pullOptions},
		"push":      {pushOptions},
		"rebase":    {rebaseOptions},
		"remote":    {remoteListOptions, remoteURLOptions},
		"reset":     {resetOptions},
		"stash":     {stashPushOptions, stashApplyOptions},
		"status":    {statusOptions},
		"submodule": {submoduleOptions},
		"switch":    {switchOptions},
		"tag":       {tagOptions},
	}
	var all []option
	for _, table := range tables[name] {
		all = append(all, table...)
	}
	return all
}

func knowsOption(specs []option, word string) bool {
	name := strings.TrimLeft(word, "-")
	long := strings.HasPrefix(word, "--")
	for _, spec := range specs {
		if long && spec.long == name {
			return true
		}
		if !long && spec.short == name {
			return true
		}
	}
	return false
}

func TestHelpWithoutArgumentsExplainsTheConsoleAndListsEveryCommand(t *testing.T) {
	setupI18N(t)

	out, err := Run(t.Context(), Env{}, "help")
	if err != nil {
		t.Fatal(err)
	}

	for _, name := range helpNames() {
		want := helpIndent + name + helpSeparator + i18n.T("Console.Help."+name+".Summary")
		if !slices.Contains(lines(out), want) {
			t.Errorf("the overview lacks %q", want)
		}
	}
	if !strings.HasPrefix(out, i18n.T("Console.Help.Intro")) {
		t.Errorf("the overview must open with the introduction, got %q", out)
	}
	if !strings.HasSuffix(out, i18n.Tf("Console.Help.Unsupported", strings.Join(unsupportedCommands, ", "))) {
		t.Errorf("the overview must end with the unsupported commands, got %q", out)
	}
}

func TestHelpNamesEveryCommandTheConsoleRuns(t *testing.T) {
	names := helpNames()

	for _, name := range Names() {
		if !slices.Contains(names, name) {
			t.Errorf("%s has no help", name)
		}
	}
	if !slices.Contains(names, "help") || !slices.IsSorted(names) {
		t.Fatalf("names = %v", names)
	}
}

func TestHelpOfACommandGivesItsSummaryAndExamples(t *testing.T) {
	setupI18N(t)

	out, err := Run(t.Context(), Env{}, "help log")
	if err != nil {
		t.Fatal(err)
	}

	got := lines(out)
	if got[0] != "log"+helpSeparator+i18n.T("Console.Help.log.Summary") {
		t.Fatalf("first line = %q", got[0])
	}
	if got[1] != "" {
		t.Fatalf("second line = %q, want a blank one", got[1])
	}
	if len(got) < 4 || !strings.HasPrefix(got[2], helpIndent+"log ") {
		t.Fatalf("examples = %q", got[2:])
	}
}

func TestHelpOfAnAliasShowsTheExamplesOfItsTarget(t *testing.T) {
	setupI18N(t)

	alias, err := Run(t.Context(), Env{}, "help stage")
	if err != nil {
		t.Fatal(err)
	}

	target := strings.Join(lines(alias)[2:], "\n")
	if target != helpIndent+strings.ReplaceAll(i18n.T("Console.Help.add.Examples"), "\n", "\n"+helpIndent) {
		t.Fatalf("stage examples = %q", target)
	}
}

func TestHelpAcceptsAGitPrefix(t *testing.T) {
	setupI18N(t)

	plain, err := Run(t.Context(), Env{}, "help tag")
	if err != nil {
		t.Fatal(err)
	}
	prefixed, err := Run(t.Context(), Env{}, "git help tag")
	if err != nil {
		t.Fatal(err)
	}

	if plain != prefixed {
		t.Fatalf("plain = %q, prefixed = %q", plain, prefixed)
	}
}

func TestHelpRefusesWhatItDoesNotKnow(t *testing.T) {
	tests := []struct {
		name string
		line string
		want error
	}{
		{"unknown command", "help frobnicate", ErrUnknownCommand},
		{"unsupported command", "help bisect", ErrUnsupported},
		{"too many arguments", "help log tag", ErrUsage},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Run(t.Context(), Env{}, tc.line); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestHelpWorksWithoutAnOpenRepository(t *testing.T) {
	setupI18N(t)

	for _, line := range []string{"help", "help commit"} {
		if _, err := Run(t.Context(), Env{}, line); err != nil {
			t.Fatalf("%s: %v", line, err)
		}
	}
}

func TestEveryHelpTextExistsInEveryLanguage(t *testing.T) {
	catalog, err := i18n.Builtin()
	if err != nil {
		t.Fatal(err)
	}
	keys := []string{"Console.Help.Intro", "Console.Help.Unsupported", "Dialog.Console.Help"}
	for _, name := range helpNames() {
		keys = append(keys, "Console.Help."+name+".Summary")
		if _, alias := helpExamplesOf[name]; !alias {
			keys = append(keys, "Console.Help."+name+".Examples")
		}
	}
	for code, table := range catalog {
		for _, key := range keys {
			if strings.TrimSpace(table[key]) == "" {
				t.Errorf("%s lacks %s", code, key)
			}
		}
	}
}

func TestEveryHelpExampleIsACommandTheConsoleAccepts(t *testing.T) {
	catalog, err := i18n.Builtin()
	if err != nil {
		t.Fatal(err)
	}
	for code, table := range catalog {
		for _, name := range helpNames() {
			if _, alias := helpExamplesOf[name]; alias {
				continue
			}
			for _, example := range strings.Split(table["Console.Help."+name+".Examples"], "\n") {
				command, description, found := strings.Cut(example, helpSeparator)
				if !found || strings.TrimSpace(description) == "" {
					t.Errorf("%s %s: %q has no description", code, name, example)
					continue
				}
				parsed, err := Parse(command)
				if err != nil || parsed.Name != name {
					t.Errorf("%s %s: %q does not start with the command: %v", code, name, command, err)
					continue
				}
				for _, word := range parsed.Args {
					if exampleOptionPattern.MatchString(word) && !knowsOption(optionTablesOf(name), word) {
						t.Errorf("%s %s: %q uses %s, which the command does not take", code, name, command, word)
					}
				}
			}
		}
	}
}

func TestHelpExamplesFitTheOutputWidth(t *testing.T) {
	const widest = 98
	catalog, err := i18n.Builtin()
	if err != nil {
		t.Fatal(err)
	}
	for code, table := range catalog {
		for key, text := range table {
			if !strings.HasPrefix(key, "Console.Help.") {
				continue
			}
			for line := range strings.SplitSeq(text, "\n") {
				if len([]rune(line)) > widest && !strings.HasSuffix(key, ".Intro") {
					t.Errorf("%s %s: %q is longer than %d characters", code, key, line, widest)
				}
			}
		}
	}
}
