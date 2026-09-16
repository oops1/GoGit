package hooks

import (
	"errors"
	"os/exec"
	"syscall"

	"golang.org/x/sys/unix"
)

const fallbackShell = "/bin/sh"

var (
	access    = unix.Access
	killGroup = func(pid int) error { return unix.Kill(-pid, unix.SIGKILL) }
)

func CanRunScripts() bool { return true }

func candidateNames(name string) []string { return []string{name} }

func executable(path string) bool { return access(path, unix.X_OK) == nil }

func planLaunch(path string, args []string) (launch, bool) {
	return launch{program: path, args: args}, true
}

func startProcess(build func(string, []string) *exec.Cmd, plan launch) (*process, error) {
	cmd, err := startGroup(build(plan.program, plan.args))
	if errors.Is(err, syscall.ENOEXEC) {
		cmd, err = startGroup(build(fallbackShell, append([]string{plan.program}, plan.args...)))
	}
	if err != nil {
		return nil, err
	}
	return &process{cmd: cmd, release: func() {}}, nil
}

func startGroup(cmd *exec.Cmd) (*exec.Cmd, error) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return killGroup(cmd.Process.Pid) }
	return cmd, cmd.Start()
}
