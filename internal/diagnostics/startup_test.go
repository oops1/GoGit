package diagnostics

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func traceInto(t *testing.T, env string) (*bytes.Buffer, bool) {
	t.Helper()
	previousEnv, previousNow := startupEnv, startupNow
	startupEnv = func(name string) string {
		if name == EnvStartupTrace {
			return env
		}
		return ""
	}
	start := time.Unix(1700000000, 0)
	startupNow = func() time.Time { return start.Add(250 * time.Millisecond) }
	t.Cleanup(func() {
		startupEnv, startupNow = previousEnv, previousNow
		EndStartupTrace()
	})
	var buf bytes.Buffer
	began := BeginStartupTrace(slog.New(slog.NewTextHandler(&buf, nil)), start)
	return &buf, began
}

func TestStartupStepsAreLoggedOnceWithTheTimeSinceStart(t *testing.T) {
	buf, began := traceInto(t, "1")

	StartupStep(StepWindowStarted)
	StartupStep(StepWindowStarted)
	StartupStep(StepJournalShown)

	if !began {
		t.Fatal("the trace did not begin although the variable is set")
	}
	text := buf.String()
	if strings.Count(text, "step=\"window started\"") != 1 || !strings.Contains(text, "step=\"journal first page shown\"") {
		t.Fatalf("the log is %q, want each step once", text)
	}
	if !strings.Contains(text, "elapsed_ms=250") {
		t.Fatalf("the log is %q, want the time since the process started", text)
	}
	if strings.Index(text, "window started") > strings.Index(text, "journal first page shown") {
		t.Fatalf("the steps are out of order in %q", text)
	}
}

func TestStartupStepsAreSilentWithoutTheVariable(t *testing.T) {
	buf, began := traceInto(t, "  ")

	StartupStep(StepLoggerOpened)

	if began || buf.Len() != 0 {
		t.Fatalf("began = %v, log = %q, want nothing without the variable", began, buf.String())
	}
}

func TestStartupStepsStopAfterTheTraceEnds(t *testing.T) {
	buf, _ := traceInto(t, "1")
	EndStartupTrace()

	StartupStep(StepStatusShown)

	if buf.Len() != 0 {
		t.Fatalf("the log is %q after the trace ended", buf.String())
	}
}
