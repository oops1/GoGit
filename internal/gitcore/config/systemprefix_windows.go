//go:build windows

package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/sys/windows/registry"
)

const (
	gitForWindowsKey   = `SOFTWARE\GitForWindows`
	installPathValue   = "InstallPath"
	registryQueryFlags = registry.QUERY_VALUE | registry.WOW64_64KEY
)

var (
	gitInstallPrefix    = sync.OnceValue(findGitInstallPrefix)
	registryInstallPath = func(root registry.Key) string { return registryString(root, gitForWindowsKey, installPathValue) }
	lookPath            = exec.LookPath
)

func findGitInstallPrefix() string {
	for _, root := range gitPrefixCandidates() {
		if hasSystemConfig(root) {
			return root
		}
	}
	return ""
}

func gitPrefixCandidates() []string {
	var roots []string
	for _, key := range []registry.Key{registry.LOCAL_MACHINE, registry.CURRENT_USER} {
		if path := registryInstallPath(key); path != "" {
			roots = append(roots, path)
		}
	}
	if git, err := lookPath("git"); err == nil {
		roots = append(roots, prefixesOfGit(git)...)
	}
	return roots
}

func registryString(root registry.Key, path, name string) string {
	key, err := registry.OpenKey(root, path, registryQueryFlags)
	if err != nil {
		return ""
	}
	defer func() { _ = key.Close() }()
	value, _, err := key.GetStringValue(name)
	if err != nil {
		return ""
	}
	return value
}

func prefixesOfGit(git string) []string {
	dir := filepath.Dir(git)
	parent := filepath.Dir(dir)
	roots := []string{parent}
	if strings.EqualFold(filepath.Base(dir), "bin") || strings.EqualFold(filepath.Base(dir), "cmd") {
		roots = append(roots, filepath.Dir(parent))
	}
	return roots
}

func hasSystemConfig(root string) bool {
	info, err := os.Stat(filepath.Join(root, "etc", "gitconfig"))
	return err == nil && !info.IsDir()
}
