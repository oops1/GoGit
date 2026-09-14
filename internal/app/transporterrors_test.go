package app

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/progress"
	"github.com/oops1/gogit/internal/gitcore/transport"
	"github.com/oops1/gogit/internal/i18n"
)

func TestLocalizeOperationErrorExplainsTransportFailures(t *testing.T) {
	for _, known := range transportErrorKeys {
		wrapped := fmt.Errorf("%w: detail", known.err)
		got := localizeOperationError(wrapped)
		if !errors.Is(got, known.err) || !strings.HasPrefix(got.Error(), i18n.T(known.key)+": ") {
			t.Errorf("localizeOperationError(%v) = %v, want the %s text in front", wrapped, got, known.key)
		}
	}
}

func TestLocalizeOperationErrorKeepsOtherErrors(t *testing.T) {
	if got := localizeOperationError(nil); got != nil {
		t.Fatalf("localizeOperationError(nil) = %v, want nil", got)
	}
	if got := localizeOperationError(errors.New("boom")); got.Error() != "boom" {
		t.Fatalf("localizeOperationError(boom) = %v, want boom unchanged", got)
	}
}

func TestRemoteUserAgentLooksLikeGit(t *testing.T) {
	if !strings.HasPrefix(remoteUserAgent, "git/") || !strings.Contains(remoteUserAgent, "(Go.Git ") {
		t.Fatalf("remoteUserAgent = %q, want git/<version> (Go.Git <version>)", remoteUserAgent)
	}
}

func TestOperationProgressLogsTheTLSVerificationWarningOnce(t *testing.T) {
	a := newTestApp(t)
	views := captureOperationViews(t)
	a.RunOperation("Fetch", func(_ context.Context, reporter OperationReporter) error {
		prog := newOperationProgress(reporter)
		prog.Phase(progress.PhaseTLSVerifyDisabled)
		prog.Phase(progress.PhaseTLSVerifyDisabled)
		return nil
	})
	view := lastOperationView(t, views)
	waitForFinishedOperation(t, a, view)
	lines := readOnDispatcher(t, a, view.Lines)
	if want := []string{i18n.T("Operation.Log.TLSVerifyDisabled")}; !slices.Equal(lines, want) {
		t.Fatalf("lines = %v, want %v", lines, want)
	}
}

func TestRunOperationExplainsATransportFailure(t *testing.T) {
	a := newTestApp(t)
	views := captureOperationViews(t)
	a.RunOperation("Fetch", func(context.Context, OperationReporter) error {
		return fmt.Errorf("%w: 407", transport.ErrProxyAuthRequired)
	})
	view := lastOperationView(t, views)
	waitForFinishedOperation(t, a, view)
	lines := readOnDispatcher(t, a, view.Lines)
	prefix := i18n.T("Operation.Error.ProxyAuthRequired")
	if !slices.ContainsFunc(lines, func(line string) bool { return strings.HasPrefix(line, prefix) }) {
		t.Fatalf("lines = %v, want a line starting with %q", lines, prefix)
	}
}
