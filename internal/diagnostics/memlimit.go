package diagnostics

import (
	"log/slog"
	"os"
	"runtime/debug"
)

const (
	EnvMemoryLimit     = "GOMEMLIMIT"
	DefaultMemoryLimit = 2 << 30
)

var setMemoryLimit = debug.SetMemoryLimit

func LimitMemory(log *slog.Logger) {
	if os.Getenv(EnvMemoryLimit) != "" {
		return
	}
	previous := setMemoryLimit(DefaultMemoryLimit)
	log.Info("the garbage collector was given a memory limit",
		"limit_mib", DefaultMemoryLimit>>20, "previous_mib", previous>>20)
}
