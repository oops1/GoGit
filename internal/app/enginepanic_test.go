package app

import (
	"bytes"
	"image/color"
	"log/slog"
	"net/url"
	"strings"
	"testing"

	"github.com/oops1/headless-gui/v4/engine"
	"github.com/oops1/headless-gui/v4/widget"

	"github.com/oops1/gogit/internal/config"
)

func newPanicLoggingApp(t *testing.T) (*App, *bytes.Buffer) {
	t.Helper()
	widget.ClearStrings()
	t.Cleanup(widget.ClearStrings)
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	a, err := New(config.Default(), config.Paths{Dir: t.TempDir()}, logger)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)
	a.stopPostDriver()
	a.stopPostDriver = func() {}
	return a, &buf
}

func TestAPanicTheEngineIsolatesIsWrittenToTheLog(t *testing.T) {
	a, buf := newPanicLoggingApp(t)
	a.Engine().SetFailurePolicy(engine.FailureIsolate)

	a.Post(func() { panic("broken callback") })
	a.Engine().Flush()

	out := buf.String()
	for _, want := range []string{"engine caught a panic", "where=Post", "broken callback", "stack="} {
		if !strings.Contains(out, want) {
			t.Fatalf("log lacks %q: %s", want, out)
		}
	}
}

func TestTheSamePanicSiteIsLoggedOnce(t *testing.T) {
	a, buf := newPanicLoggingApp(t)
	a.Engine().SetFailurePolicy(engine.FailureIsolate)

	a.Post(func() { panic("first") })
	a.Post(func() { panic("second") })
	a.Engine().Flush()

	if got := strings.Count(buf.String(), "engine caught a panic"); got != 1 {
		t.Fatalf("logged %d times, want once: %s", got, buf.String())
	}
}

func TestPanicsOfDifferentWidgetsAreLoggedApart(t *testing.T) {
	a, buf := newPanicLoggingApp(t)

	a.reportEnginePanic(engine.PanicInfo{Where: "Draw", Widget: widget.NewLabel("a", color.RGBA{}), Path: "Window > Label", Value: "x"})
	a.reportEnginePanic(engine.PanicInfo{Where: "Draw", Widget: widget.NewButton("b"), Path: "Window > Button", Value: "y"})
	a.reportEnginePanic(engine.PanicInfo{Where: "Draw", Widget: widget.NewButton("c"), Path: "Window > Button", Value: "z"})

	out := buf.String()
	if got := strings.Count(out, "engine caught a panic"); got != 2 {
		t.Fatalf("logged %d times, want 2: %s", got, out)
	}
	for _, want := range []string{"*base.Label", "*base.Button", "path=\"Window > Button\""} {
		if !strings.Contains(out, want) {
			t.Fatalf("log lacks %q: %s", want, out)
		}
	}
}

func TestAPanicValueDoesNotLeakASecret(t *testing.T) {
	a, buf := newPanicLoggingApp(t)
	remote := url.URL{Scheme: "https", User: url.UserPassword("user", "hunter2"), Host: "host", Path: "/r.git"}

	a.reportEnginePanic(engine.PanicInfo{
		Where: "Post",
		Value: "fetch " + remote.String() + " failed",
		Stack: []byte("Authorization: Bearer abc"),
	})

	out := buf.String()
	for _, secret := range []string{"hunter2", "Bearer abc"} {
		if strings.Contains(out, secret) {
			t.Fatalf("log leaks %q: %s", secret, out)
		}
	}
}

func TestAWidgetlessPanicHasNoTypeName(t *testing.T) {
	if got := widgetTypeName(nil); got != "" {
		t.Fatalf("widgetTypeName(nil) = %q", got)
	}
}
