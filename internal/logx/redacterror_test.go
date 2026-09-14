package logx

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestRedactMasksSecretsInsideErrorsAndTokenURLs(t *testing.T) {
	var buf bytes.Buffer
	log := newRedactLogger(&buf)

	log.Warn("fetch failed", "error", errors.New(`parse "https://u:hunter2@host:bad": invalid port`), "remote", "https://ghp_TOKEN@github.com/o/r.git", "ssh", "ssh://git@github.com/o/r.git", "count", 3)

	out := buf.String()
	if strings.Contains(out, "hunter2") || strings.Contains(out, "ghp_TOKEN") {
		t.Fatalf("a secret leaked: %s", out)
	}
	if !strings.Contains(out, "ssh://git@github.com") || !strings.Contains(out, "count=3") {
		t.Fatalf("harmless values were altered: %s", out)
	}
}
