package hooks

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

const (
	interpreterProbe = 99
	msysSystem       = "MINGW64"
)

var directExtensions = []string{".exe", ".com", ".bat", ".cmd"}

var (
	createJob     = func() (windows.Handle, error) { return windows.CreateJobObject(nil, nil) }
	openProcess   = openProcessForJob
	assignProcess = windows.AssignProcessToJobObject
	terminateJob  = windows.TerminateJobObject
	closeHandle   = windows.CloseHandle
	getenv        = os.Getenv
)

func CanRunScripts() bool { return gitForWindows() != "" }

func candidateNames(name string) []string {
	names := []string{name}
	for _, ext := range directExtensions {
		names = append(names, name+ext)
	}
	return names
}

func executable(string) bool { return true }

func planLaunch(path string, args []string) (launch, bool) {
	root := gitForWindows()
	env := gitEnvironment(root)
	direct := launch{program: path, args: args, env: env}
	if slices.Contains(directExtensions, strings.ToLower(filepath.Ext(path))) {
		return direct, true
	}
	interpreter := parseInterpreter(path)
	if interpreter == "" {
		return direct, true
	}
	program := findInterpreter(root, interpreter)
	if program == "" {
		return launch{interpreter: interpreter}, false
	}
	return launch{program: program, args: append([]string{filepath.ToSlash(path)}, args...), env: env, interpreter: interpreter}, true
}

func parseInterpreter(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	buf := make([]byte, interpreterProbe)
	n, _ := io.ReadFull(f, buf)
	head := buf[:n]
	if n < 4 || head[0] != '#' || head[1] != '!' {
		return ""
	}
	end := bytes.IndexAny(head, "\r\n")
	if end < 0 {
		return ""
	}
	line := string(head[2:end])
	slash := strings.LastIndexByte(line, '/')
	if slash < 0 {
		slash = strings.LastIndexByte(line, '\\')
	}
	if slash < 0 {
		return ""
	}
	name, _, _ := strings.Cut(line[slash+1:], " ")
	return name
}

func findInterpreter(root, name string) string {
	if root == "" {
		return ""
	}
	file := strings.TrimSuffix(name, ".exe") + ".exe"
	for _, dir := range []string{filepath.Join("mingw64", "bin"), filepath.Join("usr", "bin"), "bin"} {
		candidate := filepath.Join(root, dir, file)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return ""
}

func gitEnvironment(root string) []string {
	if root == "" {
		return nil
	}
	path := strings.Join([]string{filepath.Join(root, "mingw64", "bin"), filepath.Join(root, "usr", "bin"), getenv("PATH")}, string(os.PathListSeparator))
	env := []string{"PATH=" + path}
	if getenv("MSYSTEM") == "" {
		env = append(env, "MSYSTEM="+msysSystem)
	}
	if getenv("HOME") == "" {
		env = append(env, "HOME="+getenv("USERPROFILE"))
	}
	return env
}

func openProcessForJob(pid int) (windows.Handle, error) {
	return windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(pid))
}

func startProcess(build func(string, []string) *exec.Cmd, plan launch) (*process, error) {
	job, err := createJob()
	if err != nil {
		return nil, err
	}
	cmd := build(plan.program, plan.args)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW}
	cmd.Cancel = func() error { return terminateJob(job, 1) }
	if err := cmd.Start(); err != nil {
		_ = closeHandle(job)
		return nil, err
	}
	if err := joinJob(job, cmd.Process.Pid); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		_ = closeHandle(job)
		return nil, err
	}
	return &process{cmd: cmd, release: func() { _ = closeHandle(job) }}, nil
}

func joinJob(job windows.Handle, pid int) error {
	handle, err := openProcess(pid)
	if err != nil {
		return err
	}
	defer func() { _ = closeHandle(handle) }()
	return assignProcess(job, handle)
}
