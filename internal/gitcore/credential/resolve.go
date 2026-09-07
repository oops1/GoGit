package credential

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/config"
)

var statHelperCandidate = os.Stat

func resolveHelper(raw string) (Helper, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || strings.HasPrefix(trimmed, "!") {
		return nil, ErrUnsupportedHelper
	}
	fields := strings.Fields(trimmed)
	switch fields[0] {
	case "cache":
		return nil, ErrUnsupportedHelper
	case "store":
		return newStoreHelper(trimmed, fields[1:])
	}
	if len(fields) > 1 {
		if isHelperFile(trimmed) {
			return &execHelper{name: trimmed, exe: trimmed}, nil
		}
		return nil, ErrUnsupportedHelper
	}
	return &execHelper{name: trimmed, exe: execName(fields[0])}, nil
}

func isHelperFile(path string) bool {
	info, err := statHelperCandidate(path)
	return err == nil && !info.IsDir()
}

func execName(name string) string {
	if filepath.IsAbs(name) {
		return name
	}
	exe := "git-credential-" + name
	if path, ok := findHelperExecutable(exe); ok {
		return path
	}
	return exe
}

func newStoreHelper(raw string, args []string) (Helper, error) {
	path := ""
	for _, a := range args {
		if v, ok := strings.CutPrefix(a, "--file="); ok {
			path = v
		}
	}
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		path = filepath.Join(home, ".git-credentials")
	} else {
		expanded, err := config.ExpandPath(path)
		if err != nil {
			return nil, err
		}
		path = expanded
	}
	return &storeHelper{name: raw, path: path}, nil
}
