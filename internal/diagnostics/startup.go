package diagnostics

import (
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"
)

const EnvStartupTrace = "GOGIT_TRACE_STARTUP"

const (
	StepLoggerOpened     = "logger opened"
	StepApplicationBuilt = "application built"
	StepRepositoryOpened = "repository opened"
	StepWindowStarted    = "window started"
	StepJournalShown     = "journal first page shown"
	StepStatusShown      = "working tree status shown"
)

var (
	startupEnv = os.Getenv
	startupNow = time.Now
)

var startup struct {
	sync.Mutex
	log   *slog.Logger
	start time.Time
	seen  map[string]bool
}

func BeginStartupTrace(log *slog.Logger, start time.Time) bool {
	if strings.TrimSpace(startupEnv(EnvStartupTrace)) == "" {
		return false
	}
	startup.Lock()
	defer startup.Unlock()
	startup.log, startup.start, startup.seen = log, start, map[string]bool{}
	return true
}

func StartupStep(step string) {
	startup.Lock()
	defer startup.Unlock()
	if startup.log == nil || startup.seen[step] {
		return
	}
	startup.seen[step] = true
	startup.log.Info("startup step", "step", step, "elapsed_ms", startupNow().Sub(startup.start).Milliseconds())
}

func EndStartupTrace() {
	startup.Lock()
	defer startup.Unlock()
	startup.log, startup.seen = nil, nil
}
