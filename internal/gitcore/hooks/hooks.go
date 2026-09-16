package hooks

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/oops1/gogit/internal/gitcore/repo"
)

const adviceIgnoredHookKey = "advice.ignoredHook"

var ErrRejected = errors.New("hooks: the hook rejected the operation")

var (
	environ     = os.Environ
	waitDelay   = 2 * time.Second
	waitCommand = (*exec.Cmd).Wait
)

type EventKind int

const (
	EventStarted EventKind = iota
	EventOutput
	EventTruncated
	EventIgnored
	EventNoInterpreter
)

type Event struct {
	Kind        EventKind
	Hook        string
	Line        string
	Interpreter string
}

type Sink func(Event)

func (s Sink) emit(e Event) {
	if s != nil {
		s(e)
	}
}

type Invocation struct {
	Name  string
	Args  []string
	Stdin []byte
	Env   []string
}

type Result struct {
	Ran       bool
	ExitCode  int
	Output    []string
	Truncated bool
}

type Error struct {
	Hook     string
	ExitCode int
	Output   []string
	Err      error
}

func (e *Error) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("hooks: the %s hook could not run: %v", e.Hook, e.Err)
	}
	return fmt.Sprintf("hooks: the %s hook rejected the operation with exit status %d", e.Hook, e.ExitCode)
}

func (e *Error) Unwrap() []error {
	if e.Err != nil {
		return []error{ErrRejected, e.Err}
	}
	return []error{ErrRejected}
}

type Runner struct {
	dir     string
	workDir string
	gitDir  string
	advise  bool
	events  Sink
}

func New(r *repo.Repository, events Sink) *Runner {
	workDir := r.WorkTree()
	if r.IsBare() || workDir == "" {
		workDir = r.GitDir()
	}
	return &Runner{
		dir:     r.HooksDir(),
		workDir: workDir,
		gitDir:  r.GitDir(),
		advise:  adviseIgnoredHooks(r),
		events:  events,
	}
}

func adviseIgnoredHooks(r *repo.Repository) bool {
	if !r.Config().Has(adviceIgnoredHookKey) {
		return true
	}
	advise, err := r.Config().GetBool(adviceIgnoredHookKey)
	return advise || err != nil
}

func (r *Runner) Exists(name string) bool {
	_, runnable := r.locate(name)
	return runnable
}

func (r *Runner) Present(name string) bool {
	path, _ := r.locate(name)
	return path != ""
}

func (r *Runner) PathArg(path string) string {
	rel, err := filepath.Rel(r.workDir, path)
	if err != nil || !filepath.IsLocal(rel) {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}

func (r *Runner) locate(name string) (string, bool) {
	for _, candidate := range candidateNames(name) {
		path := filepath.Join(r.dir, candidate)
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			continue
		}
		return path, executable(path)
	}
	return "", false
}

func (r *Runner) Verify(ctx context.Context, inv Invocation) error {
	result, err := r.Run(ctx, inv)
	switch {
	case ctx.Err() != nil:
		return ctx.Err()
	case err != nil:
		return &Error{Hook: inv.Name, ExitCode: -1, Output: result.Output, Err: err}
	case result.ExitCode != 0:
		return &Error{Hook: inv.Name, ExitCode: result.ExitCode, Output: result.Output}
	}
	return nil
}

func (r *Runner) Run(ctx context.Context, inv Invocation) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	path, runnable := r.locate(inv.Name)
	switch {
	case path == "":
		return Result{}, nil
	case !runnable:
		if r.advise {
			r.events.emit(Event{Kind: EventIgnored, Hook: inv.Name})
		}
		return Result{}, nil
	}
	plan, ok := planLaunch(path, inv.Args)
	if !ok {
		r.events.emit(Event{Kind: EventNoInterpreter, Hook: inv.Name, Interpreter: plan.interpreter})
		return Result{}, nil
	}
	r.events.emit(Event{Kind: EventStarted, Hook: inv.Name})
	out := newOutput(inv.Name, r.events)
	build := func(program string, args []string) *exec.Cmd {
		cmd := exec.CommandContext(ctx, program, args...)
		cmd.Dir = r.workDir
		cmd.Env = r.environment(plan.env, inv.Env)
		if inv.Stdin != nil {
			cmd.Stdin = bytes.NewReader(inv.Stdin)
		}
		cmd.Stdout = out
		cmd.Stderr = out
		cmd.WaitDelay = waitDelay
		return cmd
	}
	result, err := runProcess(ctx, build, plan)
	result.Output, result.Truncated = out.finish()
	return result, err
}

type process struct {
	cmd     *exec.Cmd
	release func()
}

func runProcess(ctx context.Context, build func(string, []string) *exec.Cmd, plan launch) (Result, error) {
	proc, err := startProcess(build, plan)
	if err != nil {
		return Result{Ran: true, ExitCode: -1}, err
	}
	waitErr := waitCommand(proc.cmd)
	proc.release()
	if err := ctx.Err(); err != nil {
		return Result{Ran: true, ExitCode: -1}, err
	}
	var exitErr *exec.ExitError
	if waitErr != nil && !errors.As(waitErr, &exitErr) && !errors.Is(waitErr, exec.ErrWaitDelay) {
		return Result{Ran: true, ExitCode: -1}, waitErr
	}
	return Result{Ran: true, ExitCode: proc.cmd.ProcessState.ExitCode()}, nil
}

func (r *Runner) environment(platform, extra []string) []string {
	env := environ()
	env = append(env, "GIT_DIR="+r.gitDir)
	env = append(env, platform...)
	return append(env, extra...)
}

type launch struct {
	program     string
	args        []string
	env         []string
	interpreter string
}
