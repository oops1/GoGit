package logx

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func newRedactLogger(buf *bytes.Buffer, keys ...string) *slog.Logger {
	th := slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	return slog.New(Redact(th, keys...))
}

func TestRedactDefaultKeysMaskedCaseInsensitive(t *testing.T) {
	var buf bytes.Buffer
	log := newRedactLogger(&buf)
	log.Info("login", "Password", "hunter2", "TOKEN", "abc", "user", "alice")
	out := buf.String()
	if strings.Contains(out, "hunter2") || strings.Contains(out, "abc") {
		t.Fatalf("secret leaked: %s", out)
	}
	if !strings.Contains(out, "Password=***") || !strings.Contains(out, "TOKEN=***") {
		t.Fatalf("secrets not masked: %s", out)
	}
	if !strings.Contains(out, "user=alice") {
		t.Fatalf("normal attr altered: %s", out)
	}
}

func TestRedactInsideGroup(t *testing.T) {
	var buf bytes.Buffer
	log := newRedactLogger(&buf)
	log.Info("auth", slog.Group("creds", "password", "s3cr3t", "id", 7))
	out := buf.String()
	if strings.Contains(out, "s3cr3t") {
		t.Fatalf("secret leaked in group: %s", out)
	}
	if !strings.Contains(out, "password=***") {
		t.Fatalf("group secret not masked: %s", out)
	}
	if !strings.Contains(out, "id=7") {
		t.Fatalf("group attr altered: %s", out)
	}
}

func TestRedactWithAttrs(t *testing.T) {
	var buf bytes.Buffer
	log := newRedactLogger(&buf).With("password", "leaked", "user", "bob")
	log.Info("session")
	out := buf.String()
	if strings.Contains(out, "leaked") {
		t.Fatalf("secret leaked: %s", out)
	}
	if !strings.Contains(out, "password=***") || !strings.Contains(out, "user=bob") {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestRedactWithGroup(t *testing.T) {
	var buf bytes.Buffer
	log := newRedactLogger(&buf).WithGroup("req").With("password", "leaked")
	log.Info("call")
	out := buf.String()
	if strings.Contains(out, "leaked") {
		t.Fatalf("secret leaked under group: %s", out)
	}
	if !strings.Contains(out, "password=***") {
		t.Fatalf("group-scoped secret not masked: %s", out)
	}
}

func TestRedactCustomKeys(t *testing.T) {
	var buf bytes.Buffer
	log := newRedactLogger(&buf, "ApiKey")
	log.Info("call", "apikey", "xyz")
	out := buf.String()
	if strings.Contains(out, "xyz") {
		t.Fatalf("custom secret leaked: %s", out)
	}
	if !strings.Contains(out, "apikey=***") {
		t.Fatalf("custom key not masked: %s", out)
	}
}

func TestRedactNonStringSecretValue(t *testing.T) {
	var buf bytes.Buffer
	log := newRedactLogger(&buf)
	log.Info("pin", "token", 123456)
	out := buf.String()
	if strings.Contains(out, "123456") {
		t.Fatalf("numeric secret leaked: %s", out)
	}
	if !strings.Contains(out, "token=***") {
		t.Fatalf("numeric secret not masked: %s", out)
	}
}

func TestRedactMasksAuthorizationHeaderInMessage(t *testing.T) {
	var buf bytes.Buffer
	log := newRedactLogger(&buf)
	log.Info("Authorization: Bearer abc.def.ghi")
	out := buf.String()
	if strings.Contains(out, "abc.def.ghi") {
		t.Fatalf("bearer token leaked: %s", out)
	}
	if !strings.Contains(out, "Authorization: Bearer ***") {
		t.Fatalf("header not masked: %s", out)
	}
}

func TestRedactMasksCredentialsInURLAttr(t *testing.T) {
	var buf bytes.Buffer
	log := newRedactLogger(&buf)
	log.Info("fetch", "url", "https://user:pass@example.com/repo.git")
	out := buf.String()
	if strings.Contains(out, "pass@") {
		t.Fatalf("url password leaked: %s", out)
	}
	if !strings.Contains(out, "user:***@example.com") {
		t.Fatalf("url not masked: %s", out)
	}
}

func TestRedactEnabledDelegatesToNext(t *testing.T) {
	var buf bytes.Buffer
	th := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})
	log := slog.New(Redact(th))
	log.Info("hidden")
	log.Warn("shown")
	out := buf.String()
	if strings.Contains(out, "hidden") {
		t.Fatalf("info should be filtered by underlying level: %s", out)
	}
	if !strings.Contains(out, "shown") {
		t.Fatalf("warn missing: %s", out)
	}
}
