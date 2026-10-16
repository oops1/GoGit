package app

import (
	"bytes"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/oops1/gogit/internal/diagnostics"
)

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestStartupTraceMarksTheRepositoryTheJournalAndTheStatus(t *testing.T) {
	t.Setenv(diagnostics.EnvStartupTrace, "1")
	var trace lockedBuffer
	if !diagnostics.BeginStartupTrace(slog.New(slog.NewTextHandler(&trace, nil)), time.Now()) {
		t.Fatal("the startup trace did not begin")
	}
	t.Cleanup(diagnostics.EndStartupTrace)

	a, _, _ := cloneForDivergence(t)
	waitForJournalRows(t, a, 1)
	waitForWorkingRows(t, a, 0)
	drainPostQueue(t, a)

	text := trace.String()
	for _, step := range []string{diagnostics.StepRepositoryOpened, diagnostics.StepJournalShown, diagnostics.StepStatusShown} {
		if !strings.Contains(text, "step=\""+step+"\"") {
			t.Fatalf("the trace %q lacks %q", text, step)
		}
	}
}
