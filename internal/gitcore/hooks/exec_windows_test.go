package hooks

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func swap[T any](t *testing.T, target *T, value T) {
	t.Helper()
	previous := *target
	*target = value
	t.Cleanup(func() { *target = previous })
}

func writeFile(t *testing.T, path, content string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o777); err != nil {
		t.Fatalf("MkdirAll returned error %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o666); err != nil {
		t.Fatalf("WriteFile returned error %v", err)
	}
	return path
}

func fakeGitRoot(t *testing.T, tools ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, tool := range tools {
		writeFile(t, filepath.Join(root, tool), "")
	}
	return root
}

func TestScriptsRunOnlyWithGitForWindows(t *testing.T) {
	swap(t, &gitForWindows, func() string { return "" })
	if CanRunScripts() {
		t.Fatal("scripts must not run without Git for Windows")
	}
	swap(t, &gitForWindows, func() string { return `C:\Git` })
	if !CanRunScripts() {
		t.Fatal("scripts must run with Git for Windows")
	}
}

func TestCandidateNamesAreTheHookNameAndItsExeLikeGitForWindows(t *testing.T) {
	want := []string{"pre-commit", "pre-commit.exe"}
	if got := candidateNames("pre-commit"); !slices.Equal(got, want) {
		t.Fatalf("candidates = %q", got)
	}
}

func TestBatchFilesAreNotHooksLikeGitForWindows(t *testing.T) {
	r := newRepository(t, false, "")
	for _, name := range []string{"pre-commit.cmd", "pre-commit.bat", "pre-commit.com"} {
		writeFile(t, filepath.Join(r.HooksDir(), name), "@exit /b 1\r\n")
	}

	if New(r, nil).Present("pre-commit") {
		t.Fatal("a .cmd, .bat or .com file must not be taken for a hook")
	}
}

func TestTheExactHookNameWinsOverAnExtension(t *testing.T) {
	r := newRepository(t, false, "")
	exact := writeFile(t, filepath.Join(r.HooksDir(), "pre-commit"), "#!/bin/sh\n")
	writeFile(t, filepath.Join(r.HooksDir(), "pre-commit.exe"), "MZ")

	if path, runnable := New(r, nil).locate("pre-commit"); path != exact || !runnable {
		t.Fatalf("locate = %q, %v", path, runnable)
	}
}

func TestParseInterpreterReadsTheShebangLikeGitForWindows(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		content string
		want    string
	}{
		{"#!/bin/sh\necho\n", "sh"},
		{"#!/usr/bin/env python3 -u\r\n", "env"},
		{"#!C:\\Git\\bin\\bash.exe\n", "bash.exe"},
		{"#!sh\n", ""},
		{"echo hi\n", ""},
		{"#!/b", ""},
		{"#!/bin/" + strings.Repeat("x", 120) + "\n", ""},
	}
	for i, tc := range cases {
		path := writeFile(t, filepath.Join(dir, string(rune('a'+i))), tc.content)
		if got := parseInterpreter(path); got != tc.want {
			t.Fatalf("%q: interpreter = %q, want %q", tc.content, got, tc.want)
		}
	}
	if got := parseInterpreter(filepath.Join(dir, "missing")); got != "" {
		t.Fatalf("missing file: interpreter = %q", got)
	}
}

func TestFindInterpreterSearchesTheGitForWindowsToolDirectories(t *testing.T) {
	root := fakeGitRoot(t, `mingw64\bin\python.exe`, `usr\bin\sh.exe`, `bin\bash.exe`)
	if err := os.MkdirAll(filepath.Join(root, "usr", "bin", "env.exe"), 0o777); err != nil {
		t.Fatalf("MkdirAll returned error %v", err)
	}
	cases := map[string]string{
		"python":   filepath.Join(root, "mingw64", "bin", "python.exe"),
		"sh":       filepath.Join(root, "usr", "bin", "sh.exe"),
		"bash.exe": filepath.Join(root, "bin", "bash.exe"),
		"env":      "",
		"perl":     "",
	}
	for name, want := range cases {
		if got := findInterpreter(root, name); got != want {
			t.Fatalf("%s: found %q, want %q", name, got, want)
		}
	}
	if got := findInterpreter("", "sh"); got != "" {
		t.Fatalf("without a root found %q", got)
	}
}

func TestGitEnvironmentPutsTheGitToolsFirstAndFillsTheMsysDefaults(t *testing.T) {
	root := `C:\Git`
	values := map[string]string{"PATH": `C:\Windows`, "USERPROFILE": `C:\Users\ann`}
	swap(t, &getenv, func(key string) string { return values[key] })

	got := gitEnvironment(root)
	want := []string{`PATH=C:\Git\mingw64\bin;C:\Git\usr\bin;C:\Windows`, "MSYSTEM=MINGW64", `HOME=C:\Users\ann`}
	if !slices.Equal(got, want) {
		t.Fatalf("environment = %q, want %q", got, want)
	}

	values["MSYSTEM"], values["HOME"] = "MSYS", `D:\home`
	if got := gitEnvironment(root); len(got) != 1 {
		t.Fatalf("environment with msys defaults set = %q", got)
	}
	if got := gitEnvironment(""); got != nil {
		t.Fatalf("environment without git = %q", got)
	}
}

func TestPlanLaunchRunsExecutablesDirectlyAndScriptsThroughTheirInterpreter(t *testing.T) {
	root := fakeGitRoot(t, `usr\bin\sh.exe`)
	swap(t, &gitForWindows, func() string { return root })
	dir := t.TempDir()
	exe := writeFile(t, filepath.Join(dir, "pre-commit.exe"), "#!/bin/sh\n")
	binary := writeFile(t, filepath.Join(dir, "post-commit"), "MZ")
	shell := writeFile(t, filepath.Join(dir, "commit-msg"), "#!/bin/sh\n")
	python := writeFile(t, filepath.Join(dir, "pre-push"), "#!/usr/bin/python\n")

	if plan, ok := planLaunch(exe, []string{"a"}); !ok || plan.program != exe || !slices.Equal(plan.args, []string{"a"}) {
		t.Fatalf("exe plan = %+v, %v", plan, ok)
	}
	if plan, ok := planLaunch(binary, nil); !ok || plan.program != binary || len(plan.env) == 0 {
		t.Fatalf("binary plan = %+v, %v", plan, ok)
	}
	plan, ok := planLaunch(shell, []string{"msg"})
	if !ok || plan.program != filepath.Join(root, "usr", "bin", "sh.exe") || !slices.Equal(plan.args, []string{filepath.ToSlash(shell), "msg"}) {
		t.Fatalf("shell plan = %+v, %v", plan, ok)
	}
	if plan, ok := planLaunch(python, nil); ok || plan.interpreter != "python" {
		t.Fatalf("python plan = %+v, %v", plan, ok)
	}
}

func TestAScriptHookWithoutAnInterpreterIsSkippedWithAWarning(t *testing.T) {
	swap(t, &gitForWindows, func() string { return "" })
	r := newRepository(t, false, "")
	writeFile(t, filepath.Join(r.HooksDir(), "pre-commit"), "#!/bin/sh\nexit 1\n")
	events := &recorder{}

	err := New(r, events.sink).Verify(t.Context(), Invocation{Name: "pre-commit"})

	got := events.snapshot()
	if err != nil || len(got) != 1 || got[0].Kind != EventNoInterpreter || got[0].Interpreter != "sh" || got[0].Hook != "pre-commit" {
		t.Fatalf("err = %v, events = %+v", err, got)
	}
}

func TestAHookThatIsNotAProgramFailsToStart(t *testing.T) {
	r := newRepository(t, false, "")
	writeFile(t, filepath.Join(r.HooksDir(), "pre-commit"), "not a program\n")

	err := New(r, nil).Verify(t.Context(), Invocation{Name: "pre-commit"})

	var hookErr *Error
	if !errors.As(err, &hookErr) || hookErr.Err == nil || hookErr.ExitCode != -1 {
		t.Fatalf("Verify returned %v, want a start failure", err)
	}
}

func TestJobObjectFailuresStopTheHook(t *testing.T) {
	boom := errors.New("job failure")
	cases := map[string]func(t *testing.T){
		"create": func(t *testing.T) {
			swap(t, &createJob, func() (windows.Handle, error) { return 0, boom })
		},
		"open": func(t *testing.T) {
			swap(t, &openProcess, func(int) (windows.Handle, error) { return 0, boom })
		},
		"assign": func(t *testing.T) {
			swap(t, &assignProcess, func(windows.Handle, windows.Handle) error { return boom })
		},
	}
	for name, inject := range cases {
		t.Run(name, func(t *testing.T) {
			r := newRepository(t, false, "")
			log := filepath.Join(t.TempDir(), "log")
			writeHook(t, r.HooksDir(), "pre-commit", script{record: log})
			inject(t)

			err := New(r, nil).Verify(t.Context(), Invocation{Name: "pre-commit"})

			if !errors.Is(err, boom) {
				t.Fatalf("Verify returned %v, want %v", err, boom)
			}
		})
	}
}

func TestShellScriptHooksRunThroughGitForWindows(t *testing.T) {
	if gitForWindows() == "" {
		t.Skip("Git for Windows is not installed")
	}
	r := newRepository(t, false, "")
	writeFile(t, filepath.Join(r.HooksDir(), "commit-msg"), "#!/bin/sh\necho \"file=$1\"\nread line\necho \"stdin=$line\"\necho \"$MSYSTEM\" | tr A-Z a-z\nexit 4\n")
	events := &recorder{}

	err := New(r, events.sink).Verify(t.Context(), Invocation{Name: "commit-msg", Args: []string{".git/COMMIT_EDITMSG"}, Stdin: []byte("payload\n")})

	var hookErr *Error
	if !errors.As(err, &hookErr) || hookErr.ExitCode != 4 {
		t.Fatalf("Verify returned %v, want exit status 4", err)
	}
	want := []string{"file=.git/COMMIT_EDITMSG", "stdin=payload"}
	if got := events.lines(); len(got) != 3 || !slices.Equal(got[:2], want) || got[2] == "" {
		t.Fatalf("lines = %q, want %q and the msys system", got, want)
	}
}
