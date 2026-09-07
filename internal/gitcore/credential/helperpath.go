package credential

import (
	"os/exec"
	"path/filepath"
	"sync"
)

var lookPath = exec.LookPath

var helperDirectories = sync.OnceValue(gitHelperDirectories)

func findHelperExecutable(exe string) (string, bool) {
	if path, err := lookPath(exe); err == nil {
		return path, true
	}
	for _, dir := range helperDirectories() {
		candidate := filepath.Join(dir, exe+helperExeSuffix)
		if isHelperFile(candidate) {
			return candidate, true
		}
	}
	return "", false
}

func gitHelperDirectories() []string {
	dirs := make([]string, 0, len(helperDirsRelativeToGit))
	if path, err := lookPath("git"); err == nil {
		root := gitInstallRoot(filepath.Dir(path))
		for _, rel := range helperDirsRelativeToGit {
			dirs = append(dirs, filepath.Join(root, filepath.FromSlash(rel)))
		}
	}
	return append(dirs, fixedHelperDirs()...)
}

func gitInstallRoot(binDir string) string {
	root := binDir
	for range 2 {
		switch filepath.Base(root) {
		case "cmd", "bin", "mingw64", "mingw32", "usr":
			root = filepath.Dir(root)
		default:
			return root
		}
	}
	return root
}
