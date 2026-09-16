package hooks

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestScriptsAlwaysRunOnLinux(t *testing.T) {
	if !CanRunScripts() {
		t.Fatal("scripts must run on Linux")
	}
}

func TestAHookWithoutTheExecutableBitIsIgnoredWithAdvice(t *testing.T) {
	cases := []struct {
		config string
		events int
	}{
		{"", 1},
		{"[advice]\n\tignoredHook = false\n", 0},
	}
	for _, tc := range cases {
		r := newRepository(t, false, tc.config)
		path := writeHook(t, r.HooksDir(), "pre-commit", script{exit: 1})
		if err := os.Chmod(path, 0o644); err != nil {
			t.Fatalf("Chmod returned error %v", err)
		}
		events := &recorder{}
		runner := New(r, events.sink)

		err := runner.Verify(t.Context(), Invocation{Name: "pre-commit"})

		got := events.snapshot()
		if err != nil || len(got) != tc.events || !runner.Present("pre-commit") || runner.Exists("pre-commit") {
			t.Fatalf("config %q: err = %v, events = %+v", tc.config, err, got)
		}
		if tc.events == 1 && (got[0].Kind != EventIgnored || got[0].Hook != "pre-commit") {
			t.Fatalf("event = %+v, want the ignored hook advice", got[0])
		}
	}
}

func TestAScriptWithoutAShebangRunsThroughTheShell(t *testing.T) {
	r := newRepository(t, false, "")
	path := filepath.Join(r.HooksDir(), "post-commit")
	if err := os.WriteFile(path, []byte("echo \"through $0\"\n"), 0o755); err != nil {
		t.Fatalf("WriteFile returned error %v", err)
	}
	events := &recorder{}

	if err := New(r, events.sink).Verify(t.Context(), Invocation{Name: "post-commit"}); err != nil {
		t.Fatalf("Verify returned error %v", err)
	}
	if got := events.lines(); !slices.Equal(got, []string{"through " + path}) {
		t.Fatalf("lines = %q", got)
	}
}

func TestAHookWithAMissingInterpreterFailsToStart(t *testing.T) {
	r := newRepository(t, false, "")
	path := filepath.Join(r.HooksDir(), "pre-commit")
	if err := os.WriteFile(path, []byte("#!/nonexistent/interpreter\n"), 0o755); err != nil {
		t.Fatalf("WriteFile returned error %v", err)
	}

	err := New(r, nil).Verify(t.Context(), Invocation{Name: "pre-commit"})

	var hookErr *Error
	if !errors.As(err, &hookErr) || hookErr.Err == nil || hookErr.ExitCode != -1 {
		t.Fatalf("Verify returned %v, want a start failure", err)
	}
}
