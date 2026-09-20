package console

import (
	"errors"
	"testing"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/i18n"
)

func setupI18N(t *testing.T) {
	t.Helper()
	widget.ClearStrings()
	t.Cleanup(widget.ClearStrings)
	if _, err := i18n.Install(""); err != nil {
		t.Fatal(err)
	}
	i18n.Apply("en")
}

func TestMessageSpellsOutTheDetailedFailures(t *testing.T) {
	setupI18N(t)
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"unknown command", detail(ErrUnknownCommand, "frob"), i18n.Tf("Console.Error.UnknownCommand", "frob")},
		{"unsupported", detail(ErrUnsupported, "bisect"), i18n.Tf("Console.Error.Unsupported", "bisect")},
		{"usage", detail(ErrUsage, "add"), i18n.Tf("Console.Error.Usage", "add")},
		{"unknown option", detail(ErrUnknownOption, "--x"), i18n.Tf("Console.Error.UnknownOption", "--x")},
		{"option needs a value", detail(ErrOptionNeedsValue, "-m"), i18n.Tf("Console.Error.OptionNeedsValue", "-m")},
		{"option takes no value", detail(ErrOptionTakesNoValue, "--a=1"), i18n.Tf("Console.Error.OptionTakesNoValue", "--a=1")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Message(tt.err); got != tt.want {
				t.Fatalf("Message = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMessageNamesThePlainFailures(t *testing.T) {
	setupI18N(t)
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"bad quoting", ErrBadQuoting, i18n.T("Console.Error.BadQuoting")},
		{"bad escape", ErrBadEscape, i18n.T("Console.Error.BadEscape")},
		{"no repository", ErrNoRepository, i18n.T("Console.Error.NoRepository")},
		{"busy", ErrBusy, i18n.T("Console.Error.Busy")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Message(tt.err); got != tt.want {
				t.Fatalf("Message = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMessageFallsBackToTheErrorItself(t *testing.T) {
	setupI18N(t)
	err := errors.New("boom")

	if got := Message(err); got != i18n.Tf("Console.Error.Failed", err) {
		t.Fatalf("Message = %q", got)
	}
	if got := Message(detail(errors.New("other"), "x")); got == "" {
		t.Fatalf("Message = %q", got)
	}
}

func TestMessageOfNothingIsEmpty(t *testing.T) {
	setupI18N(t)
	if got := Message(nil); got != "" {
		t.Fatalf("Message = %q", got)
	}
}
