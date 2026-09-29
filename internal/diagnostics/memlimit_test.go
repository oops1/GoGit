package diagnostics

import (
	"log/slog"
	"math"
	"testing"
)

func TestLimitMemoryAsksTheCollectorToStayUnderTheLimit(t *testing.T) {
	t.Setenv(EnvMemoryLimit, "")
	var asked int64
	setMemoryLimit = func(limit int64) int64 {
		asked = limit
		return math.MaxInt64
	}
	t.Cleanup(func() { setMemoryLimit = nil })
	LimitMemory(slog.New(slog.DiscardHandler))
	if asked != DefaultMemoryLimit {
		t.Fatalf("the collector was asked for %d bytes instead of %d", asked, int64(DefaultMemoryLimit))
	}
}

func TestLimitMemoryLeavesTheLimitTheUserChose(t *testing.T) {
	t.Setenv(EnvMemoryLimit, "2GiB")
	called := false
	setMemoryLimit = func(int64) int64 {
		called = true
		return 0
	}
	t.Cleanup(func() { setMemoryLimit = nil })
	LimitMemory(slog.New(slog.DiscardHandler))
	if called {
		t.Fatal("the limit from the environment was overwritten")
	}
}
