package config

import (
	"os"
	"path/filepath"
	"runtime"
)

func defaultSystemPaths() []string {
	return systemPathsFor(runtime.GOOS, os.Getenv("ProgramData"), gitInstallPrefix())
}

func systemPathsFor(goos, programData, gitPrefix string) []string {
	if goos != "windows" {
		return []string{"/etc/gitconfig"}
	}
	var paths []string
	if programData != "" {
		paths = append(paths, filepath.Join(programData, "Git", "config"))
	}
	if gitPrefix != "" {
		paths = append(paths, filepath.Join(gitPrefix, "etc", "gitconfig"))
	}
	return paths
}

func loadWithSystemPaths(paths []string) (*Config, error) {
	l := &loader{cfg: &Config{files: map[Level]*File{}}}
	if err := l.addSystemFiles(paths); err != nil {
		return nil, err
	}
	return l.cfg, nil
}
