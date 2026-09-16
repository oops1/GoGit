package app

import (
	"errors"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/hooks"
	"github.com/oops1/gogit/internal/i18n"
)

const hookTailLines = 3

func hookEvents(reporter OperationReporter) hooks.Sink {
	return func(e hooks.Event) {
		switch e.Kind {
		case hooks.EventStarted:
			reporter.Log(i18n.Tf("Operation.Log.HookRunning", e.Hook))
		case hooks.EventOutput:
			reporter.Log(e.Line)
		case hooks.EventTruncated:
			reporter.Log(i18n.Tf("Operation.Log.HookTruncated", e.Hook))
		case hooks.EventIgnored:
			reporter.Log(i18n.Tf("Operation.Log.HookIgnored", e.Hook))
		case hooks.EventNoInterpreter:
			reporter.Log(i18n.Tf("Operation.Log.HookNoInterpreter", e.Hook, e.Interpreter))
		}
	}
}

func reportHookRejection(reporter OperationReporter, err error) {
	var hookErr *hooks.Error
	if errors.As(err, &hookErr) {
		reporter.Log(i18n.Tf("Operation.Log.HookRejected", hookErr.Hook))
	}
}

func hookFailureText(err error) (string, bool) {
	var hookErr *hooks.Error
	if !errors.As(err, &hookErr) {
		return "", false
	}
	detail := i18n.Tf("Status.HookExitStatus", hookErr.ExitCode)
	if n := len(hookErr.Output); n > 0 {
		detail = strings.Join(hookErr.Output[max(0, n-hookTailLines):], "; ")
	}
	return i18n.Tf("Status.HookRejected", hookErr.Hook, detail), true
}
