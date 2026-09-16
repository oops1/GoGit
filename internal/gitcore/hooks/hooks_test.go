package hooks

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/oops1/gogit/internal/gitcore/repo"
)

type script struct {
	print      []string
	stderr     string
	record     string
	exit       int
	sleep      bool
	background bool
}

type recorder struct {
	mu     sync.Mutex
	events []Event
}

func (r *recorder) sink(e Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
}

func (r *recorder) snapshot() []Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.events)
}

func (r *recorder) lines() []string {
	var out []string
	for _, e := range r.snapshot() {
		if e.Kind == EventOutput {
			out = append(out, e.Line)
		}
	}
	return out
}

func newRepository(t *testing.T, bare bool, config string) *repo.Repository {
	t.Helper()
	dir := t.TempDir()
	global := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(global, nil, 0o666); err != nil {
		t.Fatalf("WriteFile returned error %v", err)
	}
	r, err := repo.Init(dir, repo.InitOptions{InitialBranch: "main", Bare: bare, NoSystem: true, GlobalFile: global})
	if err != nil {
		t.Fatalf("repo.Init returned error %v", err)
	}
	if config == "" {
		t.Cleanup(func() { _ = r.Close() })
		return r
	}
	data, err := r.CommonRoot().ReadFile("config")
	if err != nil {
		t.Fatalf("ReadFile returned error %v", err)
	}
	if err := r.CommonRoot().WriteFile("config", append(data, config...), 0o666); err != nil {
		t.Fatalf("WriteFile returned error %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("Close returned error %v", err)
	}
	reopened, err := repo.Open(dir, repo.OpenOptions{NoSystem: true, GlobalFile: global})
	if err != nil {
		t.Fatalf("repo.Open returned error %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	return reopened
}

func readLog(t *testing.T, path string) map[string]string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile returned error %v", err)
	}
	fields := map[string]string{}
	var stdin []string
	inStdin := false
	for line := range strings.SplitSeq(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		if inStdin {
			stdin = append(stdin, line)
			continue
		}
		key, value, _ := strings.Cut(line, ":")
		fields[key] = strings.TrimRight(value, " ")
		inStdin = key == "stdin"
	}
	fields["stdin"] = strings.Join(stdin, "\n")
	return fields
}

func TestRunWithoutAHookDoesNothing(t *testing.T) {
	r := newRepository(t, false, "")
	events := &recorder{}
	runner := New(r, events.sink)

	result, err := runner.Run(t.Context(), Invocation{Name: "pre-commit"})

	if err != nil || result.Ran || len(events.snapshot()) != 0 {
		t.Fatalf("result = %+v, err = %v, events = %v", result, err, events.snapshot())
	}
	if runner.Exists("pre-commit") || runner.Present("pre-commit") {
		t.Fatal("a missing hook must be neither present nor runnable")
	}
	if err := runner.Verify(t.Context(), Invocation{Name: "pre-commit"}); err != nil {
		t.Fatalf("Verify returned error %v", err)
	}
}

func TestADirectoryNamedLikeAHookIsNotAHook(t *testing.T) {
	r := newRepository(t, false, "")
	if err := os.MkdirAll(filepath.Join(r.HooksDir(), "pre-commit"), 0o777); err != nil {
		t.Fatalf("MkdirAll returned error %v", err)
	}
	if New(r, nil).Present("pre-commit") {
		t.Fatal("a directory must not count as a hook")
	}
}

func TestRunPassesArgumentsStdinEnvironmentAndWorkingDirectory(t *testing.T) {
	r := newRepository(t, false, "")
	log := filepath.Join(t.TempDir(), "log")
	writeHook(t, r.HooksDir(), "pre-push", script{record: log})
	runner := New(r, nil)

	result, err := runner.Run(t.Context(), Invocation{
		Name:  "pre-push",
		Args:  []string{"origin", "url"},
		Stdin: []byte("refs/heads/main 1 refs/heads/main 0\n"),
		Env:   []string{"HOOK_EXTRA=value"},
	})

	if err != nil || !result.Ran || result.ExitCode != 0 {
		t.Fatalf("result = %+v, err = %v", result, err)
	}
	fields := readLog(t, log)
	if fields["args"] != "origin url" {
		t.Fatalf("args = %q", fields["args"])
	}
	if !strings.EqualFold(filepath.Clean(fields["dir"]), filepath.Clean(r.WorkTree())) {
		t.Fatalf("dir = %q, want %q", fields["dir"], r.WorkTree())
	}
	if fields["gitdir"] != r.GitDir() {
		t.Fatalf("GIT_DIR = %q, want %q", fields["gitdir"], r.GitDir())
	}
	if fields["extra"] != "value" {
		t.Fatalf("extra = %q", fields["extra"])
	}
	if !strings.Contains(fields["stdin"], "refs/heads/main 1 refs/heads/main 0") {
		t.Fatalf("stdin = %q", fields["stdin"])
	}
}

func TestBareRepositoryHooksRunInTheGitDirectory(t *testing.T) {
	r := newRepository(t, true, "")
	log := filepath.Join(t.TempDir(), "log")
	writeHook(t, r.HooksDir(), "pre-receive", script{record: log})

	if err := New(r, nil).Verify(t.Context(), Invocation{Name: "pre-receive"}); err != nil {
		t.Fatalf("Verify returned error %v", err)
	}
	if dir := readLog(t, log)["dir"]; !strings.EqualFold(filepath.Clean(dir), filepath.Clean(r.GitDir())) {
		t.Fatalf("dir = %q, want %q", dir, r.GitDir())
	}
}

func TestHooksPathSelectsTheHookDirectory(t *testing.T) {
	absolute := t.TempDir()
	cases := []struct {
		name   string
		config string
		dir    func(r *repo.Repository) string
	}{
		{"default", "", func(r *repo.Repository) string { return filepath.Join(r.CommonDir(), "hooks") }},
		{"absolute", "[core]\n\thooksPath = " + filepath.ToSlash(absolute) + "\n", func(*repo.Repository) string { return absolute }},
		{"relative", "[core]\n\thooksPath = .githooks\n", func(r *repo.Repository) string { return filepath.Join(r.WorkTree(), ".githooks") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newRepository(t, false, tc.config)
			writeHook(t, tc.dir(r), "post-commit", script{})
			if !New(r, nil).Exists("post-commit") {
				t.Fatalf("hook in %s was not found", tc.dir(r))
			}
		})
	}
}

func TestOutputOfBothStreamsReachesTheSinkAndTheResult(t *testing.T) {
	r := newRepository(t, false, "")
	writeHook(t, r.HooksDir(), "pre-commit", script{print: []string{"first", "second"}, stderr: "problem", exit: 3})
	events := &recorder{}
	runner := New(r, events.sink)

	err := runner.Verify(t.Context(), Invocation{Name: "pre-commit"})

	var hookErr *Error
	if !errors.As(err, &hookErr) || !errors.Is(err, ErrRejected) {
		t.Fatalf("err = %v, want a rejection", err)
	}
	if hookErr.Hook != "pre-commit" || hookErr.ExitCode != 3 {
		t.Fatalf("error = %+v", hookErr)
	}
	want := []string{"first", "second", "problem"}
	if got := events.lines(); !slices.Equal(got, want) {
		t.Fatalf("streamed lines = %q, want %q", got, want)
	}
	if !slices.Equal(hookErr.Output, want) {
		t.Fatalf("error output = %q, want %q", hookErr.Output, want)
	}
	if kinds := events.snapshot(); kinds[0].Kind != EventStarted || kinds[0].Hook != "pre-commit" {
		t.Fatalf("first event = %+v, want the start", kinds[0])
	}
}

func TestErrorTextNamesTheHook(t *testing.T) {
	rejected := &Error{Hook: "commit-msg", ExitCode: 1}
	failed := &Error{Hook: "pre-push", ExitCode: -1, Err: exec.ErrNotFound}

	if !strings.Contains(rejected.Error(), "commit-msg") || !strings.Contains(rejected.Error(), "1") {
		t.Fatalf("rejected text = %q", rejected.Error())
	}
	if !strings.Contains(failed.Error(), "pre-push") || !errors.Is(failed, exec.ErrNotFound) || !errors.Is(failed, ErrRejected) {
		t.Fatalf("failed = %v", failed)
	}
	if errors.Is(rejected, exec.ErrNotFound) {
		t.Fatal("a plain rejection must not wrap another error")
	}
}

func TestACanceledContextStopsBeforeLookingForTheHook(t *testing.T) {
	r := newRepository(t, false, "")
	writeHook(t, r.HooksDir(), "pre-commit", script{})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, err := New(r, nil).Run(ctx, Invocation{Name: "pre-commit"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run returned %v, want context.Canceled", err)
	}
	if err := New(r, nil).Verify(ctx, Invocation{Name: "pre-commit"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Verify returned %v, want context.Canceled", err)
	}
}

func TestCancellationKillsTheHookAndItsChildren(t *testing.T) {
	r := newRepository(t, false, "")
	writeHook(t, r.HooksDir(), "pre-commit", script{sleep: true})
	previous := waitDelay
	waitDelay = time.Minute
	t.Cleanup(func() { waitDelay = previous })
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	sink := func(e Event) {
		if e.Kind == EventOutput && e.Line == "started" {
			cancel()
		}
	}

	began := time.Now()
	err := New(r, sink).Verify(ctx, Invocation{Name: "pre-commit"})

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Verify returned %v, want context.Canceled", err)
	}
	if elapsed := time.Since(began); elapsed > 30*time.Second {
		t.Fatalf("the hook tree survived the cancellation for %v", elapsed)
	}
}

func TestABackgroundChildHoldingTheOutputDoesNotBlockTheHook(t *testing.T) {
	r := newRepository(t, false, "")
	writeHook(t, r.HooksDir(), "post-commit", script{background: true})
	previous := waitDelay
	waitDelay = 100 * time.Millisecond
	t.Cleanup(func() { waitDelay = previous })

	result, err := New(r, nil).Run(t.Context(), Invocation{Name: "post-commit"})

	if err != nil || result.ExitCode != 0 {
		t.Fatalf("result = %+v, err = %v", result, err)
	}
	time.Sleep(3 * time.Second)
}

func TestAWaitFailureIsReportedAsAHookThatCouldNotRun(t *testing.T) {
	r := newRepository(t, false, "")
	writeHook(t, r.HooksDir(), "pre-commit", script{})
	boom := errors.New("wait failed")
	previous := waitCommand
	waitCommand = func(cmd *exec.Cmd) error {
		_ = previous(cmd)
		return boom
	}
	t.Cleanup(func() { waitCommand = previous })

	err := New(r, nil).Verify(t.Context(), Invocation{Name: "pre-commit"})

	var hookErr *Error
	if !errors.As(err, &hookErr) || !errors.Is(err, boom) || hookErr.ExitCode != -1 {
		t.Fatalf("Verify returned %v, want the wait failure", err)
	}
}

func TestAdviceAboutIgnoredHooksFollowsTheConfiguration(t *testing.T) {
	cases := []struct {
		config string
		want   bool
	}{
		{"", true},
		{"[advice]\n\tignoredHook = true\n", true},
		{"[advice]\n\tignoredHook = false\n", false},
		{"[advice]\n\tignoredHook = maybe\n", true},
	}
	for _, tc := range cases {
		if got := New(newRepository(t, false, tc.config), nil).advise; got != tc.want {
			t.Fatalf("config %q: advise = %v, want %v", tc.config, got, tc.want)
		}
	}
}

func TestPathArgIsRelativeInsideTheWorkTreeAndAbsoluteOutside(t *testing.T) {
	r := newRepository(t, false, "")
	runner := New(r, nil)
	outside := filepath.Join(t.TempDir(), "file")

	if got := runner.PathArg(filepath.Join(r.GitDir(), "COMMIT_EDITMSG")); got != ".git/COMMIT_EDITMSG" {
		t.Fatalf("inside = %q", got)
	}
	if got := runner.PathArg(outside); got != filepath.ToSlash(outside) {
		t.Fatalf("outside = %q", got)
	}
}
