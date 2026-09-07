//go:build windows

package credential

import (
	"os"
	"path/filepath"
)

const helperExeSuffix = ".exe"

var helperDirsRelativeToGit = []string{
	"mingw64/bin",
	"mingw64/libexec/git-core",
	"mingw32/bin",
	"mingw32/libexec/git-core",
	"usr/bin",
	"cmd",
}

func fixedHelperDirs() []string {
	roots := []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)")}
	if local := os.Getenv("LOCALAPPDATA"); local != "" {
		roots = append(roots, filepath.Join(local, "Programs"))
	}
	dirs := make([]string, 0, len(roots)*len(helperDirsRelativeToGit))
	for _, root := range roots {
		if root == "" {
			continue
		}
		for _, rel := range helperDirsRelativeToGit {
			dirs = append(dirs, filepath.Join(root, "Git", filepath.FromSlash(rel)))
		}
	}
	return dirs
}
