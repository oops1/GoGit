package console

import (
	"errors"

	"github.com/oops1/gogit/internal/i18n"
)

var (
	ErrUnknownCommand = errors.New("console: unknown command")
	ErrUnsupported    = errors.New("console: the command is not supported yet")
	ErrUsage          = errors.New("console: the arguments do not fit the command")
	ErrNoRepository   = errors.New("console: no repository is open")
	ErrBusy           = errors.New("console: another operation is already running")
)

type Detail struct {
	Kind error
	Text string
}

func (d *Detail) Error() string {
	if d.Text == "" {
		return d.Kind.Error()
	}
	return d.Kind.Error() + ": " + d.Text
}

func (d *Detail) Unwrap() error { return d.Kind }

func detail(kind error, text string) error { return &Detail{Kind: kind, Text: text} }

var detailedKeys = map[error]string{
	ErrUnknownCommand:     "Console.Error.UnknownCommand",
	ErrUnsupported:        "Console.Error.Unsupported",
	ErrUsage:              "Console.Error.Usage",
	ErrUnknownOption:      "Console.Error.UnknownOption",
	ErrOptionNeedsValue:   "Console.Error.OptionNeedsValue",
	ErrOptionTakesNoValue: "Console.Error.OptionTakesNoValue",
}

var plainKeys = map[error]string{
	ErrBadQuoting:   "Console.Error.BadQuoting",
	ErrBadEscape:    "Console.Error.BadEscape",
	ErrNoRepository: "Console.Error.NoRepository",
	ErrBusy:         "Console.Error.Busy",
}

func Message(err error) string {
	if err == nil {
		return ""
	}
	var d *Detail
	if errors.As(err, &d) {
		if key, ok := detailedKeys[d.Kind]; ok {
			return i18n.Tf(key, d.Text)
		}
	}
	for sentinel, key := range plainKeys {
		if errors.Is(err, sentinel) {
			return i18n.T(key)
		}
	}
	return i18n.Tf("Console.Error.Failed", err)
}
