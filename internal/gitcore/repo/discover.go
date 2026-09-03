package repo

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/config"
)

const (
	envGitDir           = "GIT_DIR"
	envWorkTree         = "GIT_WORK_TREE"
	envCommonDir        = "GIT_COMMON_DIR"
	envCeiling          = "GIT_CEILING_DIRECTORIES"
	envAcrossFilesystem = "GIT_DISCOVERY_ACROSS_FILESYSTEM"
)

type DiscoverOptions struct {
	Env func(string) string
}

func (o DiscoverOptions) env(key string) string {
	if o.Env == nil {
		return os.Getenv(key)
	}
	return o.Env(key)
}

func (o DiscoverOptions) flag(key string) (bool, error) {
	raw := o.env(key)
	if raw == "" {
		return false, nil
	}
	on, err := config.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("%s: %w", key, err)
	}
	return on, nil
}

var filesystemIDOf = filesystemID

func Discover(start string, opts DiscoverOptions) (Layout, error) {
	if gitDir := opts.env(envGitDir); gitDir != "" {
		return explicitLayout(gitDir, opts)
	}
	return walkUp(absClean(start), opts)
}

func explicitLayout(gitDir string, opts DiscoverOptions) (Layout, error) {
	dir := absClean(gitDir)
	if resolved, err := readGitFile(dir); err == nil {
		dir = resolved
	}
	common, ok := gitDirectoryCommon(dir)
	if !ok {
		return Layout{}, fmt.Errorf("%w: %s=%s", ErrNotFound, envGitDir, dir)
	}
	return buildLayout(dir, common, "", opts)
}

func walkUp(start string, opts DiscoverOptions) (Layout, error) {
	info, err := os.Stat(start)
	if err != nil {
		return Layout{}, fmt.Errorf("%w: %s: %w", ErrInvalidPath, start, err)
	}
	if !info.IsDir() {
		return Layout{}, fmt.Errorf("%w: %s is not a directory", ErrInvalidPath, start)
	}
	across, err := opts.flag(envAcrossFilesystem)
	if err != nil {
		return Layout{}, err
	}
	startID := ""
	if !across {
		if startID, err = filesystemIDOf(start); err != nil {
			return Layout{}, err
		}
	}
	floor := ceilingFloor(start, parseCeilings(opts.env(envCeiling)))
	dir := start
	for {
		layout, found, err := probe(dir, opts)
		if err != nil || found {
			return layout, err
		}
		if floor != "" && samePath(dir, floor) {
			return Layout{}, fmt.Errorf("%w: %s", ErrCeilingReached, dir)
		}
		parent := filepath.Dir(dir)
		if samePath(parent, dir) {
			return Layout{}, fmt.Errorf("%w: %s", ErrNotFound, start)
		}
		if !across {
			id, err := filesystemIDOf(parent)
			if err != nil {
				return Layout{}, err
			}
			if id != startID {
				return Layout{}, fmt.Errorf("%w: %s: a filesystem boundary stops the search", ErrNotFound, start)
			}
		}
		dir = parent
	}
}

func probe(dir string, opts DiscoverOptions) (Layout, bool, error) {
	dot := filepath.Join(dir, dotGit)
	info, err := os.Stat(dot)
	switch {
	case err == nil && info.IsDir():
		if common, ok := gitDirectoryCommon(dot); ok {
			layout, err := buildLayout(dot, common, dir, opts)
			return layout, true, err
		}
	case err == nil:
		target, err := readGitFile(dot)
		if err != nil {
			return Layout{}, false, err
		}
		common, _ := gitDirectoryCommon(target)
		layout, err := buildLayout(target, common, dir, opts)
		return layout, true, err
	}
	if common, ok := gitDirectoryCommon(dir); ok {
		layout, err := buildLayout(dir, common, "", opts)
		return layout, true, err
	}
	return Layout{}, false, nil
}

func parseCeilings(raw string) []string {
	if raw == "" {
		return nil
	}
	var out []string
	resolveLinks := true
	for _, entry := range strings.Split(raw, string(os.PathListSeparator)) {
		if entry == "" {
			resolveLinks = false
			continue
		}
		if !filepath.IsAbs(entry) {
			continue
		}
		cleaned := filepath.Clean(entry)
		if resolveLinks {
			if real, err := filepath.EvalSymlinks(cleaned); err == nil {
				cleaned = real
			}
		}
		out = append(out, cleaned)
	}
	return out
}

func ceilingFloor(start string, ceilings []string) string {
	floor := ""
	for _, ceiling := range ceilings {
		if isStrictAncestor(ceiling, start) && len(ceiling) > len(floor) {
			floor = ceiling
		}
	}
	return floor
}

func isStrictAncestor(parent, child string) bool {
	if !strings.HasSuffix(parent, string(filepath.Separator)) {
		parent += string(filepath.Separator)
	}
	if len(child) <= len(parent) {
		return false
	}
	return samePath(child[:len(parent)], parent)
}

func buildLayout(gitDir, commonDir, hintWorkTree string, opts DiscoverOptions) (Layout, error) {
	if env := opts.env(envCommonDir); env != "" {
		commonDir = absClean(env)
	}
	layout := Layout{GitDir: gitDir, CommonDir: commonDir, IsWorktree: !samePath(commonDir, gitDir)}
	if env := opts.env(envWorkTree); env != "" {
		layout.WorkTree = absClean(env)
		return layout, nil
	}
	local, err := localConfig(commonDir)
	if err != nil {
		return Layout{}, err
	}
	bare, bareSet := localBool(local, "core.bare")
	if bareSet && bare {
		layout.Bare = true
		return layout, nil
	}
	if worktree := localPath(local, "core.worktree"); worktree != "" {
		layout.WorkTree = resolveFrom(gitDir, worktree)
		return layout, nil
	}
	if hintWorkTree != "" {
		layout.WorkTree = hintWorkTree
		return layout, nil
	}
	if worktree, ok := derivedWorkTree(gitDir, layout.IsWorktree); ok {
		layout.WorkTree = worktree
		return layout, nil
	}
	if bareSet {
		return Layout{}, fmt.Errorf("%w: %s", ErrNotBareNoWorkTree, gitDir)
	}
	layout.Bare = true
	return layout, nil
}

func derivedWorkTree(gitDir string, isWorktree bool) (string, bool) {
	if !isWorktree {
		if samePath(filepath.Base(gitDir), dotGit) {
			return filepath.Dir(gitDir), true
		}
		return "", false
	}
	data, err := os.ReadFile(filepath.Join(gitDir, gitDirFile))
	if err != nil {
		return "", false
	}
	line := firstLine(data)
	if line == "" {
		return "", false
	}
	return filepath.Dir(resolveFrom(gitDir, filepath.FromSlash(line))), true
}

func localConfig(commonDir string) (*config.File, error) {
	data, err := os.ReadFile(filepath.Join(commonDir, configFile))
	if err != nil {
		return nil, nil
	}
	file, err := config.Parse(data)
	if err != nil {
		return nil, err
	}
	return file, nil
}

func localBool(file *config.File, key string) (bool, bool) {
	if file == nil {
		return false, false
	}
	value, err := file.GetBool(key)
	if err != nil {
		return false, false
	}
	return value, true
}

func localPath(file *config.File, key string) string {
	if file == nil {
		return ""
	}
	value, err := file.GetPath(key)
	if err != nil {
		return ""
	}
	return value
}
