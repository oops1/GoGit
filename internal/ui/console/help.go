package console

import (
	"slices"
	"strings"

	"github.com/oops1/gogit/internal/i18n"
)

const helpCommand = "help"

const (
	helpPrefix    = "Console.Help."
	helpSeparator = " — "
	helpIndent    = "  "
)

var helpExamplesOf = map[string]string{
	"stage": "add",
}

func helpNames() []string {
	names := append(Names(), helpCommand)
	slices.Sort(names)
	return names
}

func runHelp(args []string) (string, error) {
	switch len(args) {
	case 0:
		return helpOverview(), nil
	case 1:
		return helpTopic(args[0])
	}
	return "", detail(ErrUsage, helpCommand)
}

func helpOverview() string {
	lines := []string{i18n.T(helpPrefix + "Intro"), ""}
	for _, name := range helpNames() {
		lines = append(lines, helpIndent+name+helpSeparator+helpSummary(name))
	}
	lines = append(lines, "", i18n.Tf(helpPrefix+"Unsupported", strings.Join(unsupportedCommands, ", ")))
	return strings.Join(lines, "\n")
}

func helpTopic(name string) (string, error) {
	if !slices.Contains(helpNames(), name) {
		if slices.Contains(unsupportedCommands, name) {
			return "", detail(ErrUnsupported, name)
		}
		return "", detail(ErrUnknownCommand, name)
	}
	lines := []string{name + helpSeparator + helpSummary(name), ""}
	examples := name
	if shared, ok := helpExamplesOf[name]; ok {
		examples = shared
	}
	for line := range strings.SplitSeq(i18n.T(helpPrefix+examples+".Examples"), "\n") {
		lines = append(lines, helpIndent+line)
	}
	return strings.Join(lines, "\n"), nil
}

func helpSummary(name string) string {
	return i18n.T(helpPrefix + name + ".Summary")
}
