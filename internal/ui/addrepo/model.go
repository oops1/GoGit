package addrepo

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/repo"
)

type Mode int

const (
	ModeOpen Mode = iota
	ModeCreate
)

type Request struct {
	Path string
	Name string
	Bare bool
	Mode Mode
}

type Hint struct {
	Key  string
	Args []any
	OK   bool
}

type Result struct {
	Layout repo.Layout
	Name   string
	Path   string
}

const (
	hintPathRequired      = "Dialog.AddRepo.Hint.PathRequired"
	hintPathNotFound      = "Dialog.AddRepo.Hint.PathNotFound"
	hintNotARepository    = "Dialog.AddRepo.Hint.NotARepository"
	hintOpenFound         = "Dialog.AddRepo.Hint.OpenFound"
	hintPathNotDirectory  = "Dialog.AddRepo.Hint.PathNotDirectory"
	hintAlreadyRepository = "Dialog.AddRepo.Hint.AlreadyRepository"
	hintNameRequired      = "Dialog.AddRepo.Hint.NameRequired"
	hintWillCreate        = "Dialog.AddRepo.Hint.WillCreate"
)

func noEnv(string) string { return "" }

func discover(path string) (repo.Layout, error) {
	return repo.Discover(path, repo.DiscoverOptions{Env: noEnv})
}

func repoRoot(layout repo.Layout) string {
	if layout.Bare || layout.WorkTree == "" {
		return layout.GitDir
	}
	return layout.WorkTree
}

func branchOf(layout repo.Layout) string {
	r, err := repo.OpenLayout(layout, repo.OpenOptions{Env: noEnv})
	if err != nil {
		return ""
	}
	defer func() { _ = r.Close() }()
	data, err := os.ReadFile(r.GitPath("HEAD"))
	if err != nil {
		return ""
	}
	line := strings.TrimSpace(string(data))
	if branch, ok := strings.CutPrefix(line, "ref: refs/heads/"); ok {
		return branch
	}
	return line
}

func effectiveName(rawName, path string) string {
	name := strings.TrimSpace(rawName)
	if name != "" {
		return name
	}
	return filepath.Base(filepath.Clean(path))
}

func Validate(req Request) Hint {
	path := strings.TrimSpace(req.Path)
	if path == "" {
		return Hint{Key: hintPathRequired}
	}
	info, statErr := os.Stat(path)
	if req.Mode == ModeCreate {
		return validateCreate(path, req.Name, info, statErr)
	}
	return validateOpen(path, info, statErr)
}

func validateOpen(path string, info os.FileInfo, statErr error) Hint {
	if statErr != nil {
		return Hint{Key: hintPathNotFound, Args: []any{path}}
	}
	if !info.IsDir() {
		return Hint{Key: hintPathNotDirectory, Args: []any{path}}
	}
	layout, err := discover(path)
	if err != nil {
		return Hint{Key: hintNotARepository, Args: []any{path}}
	}
	branch := branchOf(layout)
	return Hint{Key: hintOpenFound, Args: []any{repoRoot(layout), branch}, OK: true}
}

func validateCreate(path, rawName string, info os.FileInfo, statErr error) Hint {
	if statErr == nil {
		if !info.IsDir() {
			return Hint{Key: hintPathNotDirectory, Args: []any{path}}
		}
		if repo.IsRepository(path) {
			return Hint{Key: hintAlreadyRepository, Args: []any{path}}
		}
	}
	if strings.TrimSpace(rawName) == "" {
		return Hint{Key: hintNameRequired}
	}
	return Hint{Key: hintWillCreate, Args: []any{path}, OK: true}
}

func Apply(req Request) (Result, error) {
	path := strings.TrimSpace(req.Path)
	if req.Mode == ModeCreate {
		return applyCreate(path, req)
	}
	return applyOpen(path)
}

func applyOpen(path string) (Result, error) {
	layout, err := discover(path)
	if err != nil {
		return Result{}, err
	}
	root := repoRoot(layout)
	return Result{Layout: layout, Name: filepath.Base(root), Path: root}, nil
}

func applyCreate(path string, req Request) (Result, error) {
	r, err := repo.Init(path, repo.InitOptions{Bare: req.Bare, Env: noEnv})
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = r.Close() }()
	layout := r.Layout()
	root := repoRoot(layout)
	return Result{Layout: layout, Name: effectiveName(req.Name, root), Path: root}, nil
}
