package submodule

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/pathspec"
)

type ConfigReader interface {
	GetValue(key string) (string, bool)
	GetAll(key string) []string
	GetBool(key string) (bool, error)
	Has(key string) bool
}

func Active(cfg ConfigReader, module Module) (bool, error) {
	activeKey := "submodule." + module.Name + ".active"
	if cfg.Has(activeKey) {
		active, err := cfg.GetBool(activeKey)
		if err != nil {
			return false, fmt.Errorf("%w: %w", ErrInvalidActive, err)
		}
		return active, nil
	}
	if specs := cfg.GetAll("submodule.active"); len(specs) > 0 {
		set, err := pathspec.Parse(specs)
		if err != nil {
			return false, fmt.Errorf("%w: %w", ErrInvalidActive, err)
		}
		return set.Match(module.Path), nil
	}
	_, ok := cfg.GetValue("submodule." + module.Name + ".url")
	return ok, nil
}

func ConfiguredIgnore(cfg ConfigReader, module Module) (Ignore, error) {
	value, ok := cfg.GetValue("submodule." + module.Name + ".ignore")
	if !ok {
		return module.Ignore, nil
	}
	return ParseIgnore(value)
}

func URL(cfg ConfigReader, module Module) (string, bool) {
	value, ok := cfg.GetValue("submodule." + module.Name + ".url")
	if ok {
		return value, true
	}
	return module.URL, module.URL != ""
}

func Strategy(cfg ConfigReader, module Module) (UpdateStrategy, error) {
	value, ok := cfg.GetValue("submodule." + module.Name + ".update")
	if !ok {
		return module.Update, nil
	}
	return ParseUpdateStrategy(value)
}

func SkipsUpdate(cfg ConfigReader, module Module) bool {
	if value, ok := cfg.GetValue("submodule." + module.Name + ".update"); ok {
		return ParseUpdateType(value) == UpdateNone
	}
	return module.Update.Type == UpdateNone
}

func Branch(cfg ConfigReader, module Module) string {
	if value, ok := cfg.GetValue("submodule." + module.Name + ".branch"); ok {
		return value
	}
	return module.Branch
}

var lstatPath = os.Lstat

func ValidatePath(workTree, path string) error {
	parts := strings.FieldsFunc(path, func(c rune) bool { return c < 0x80 && nativeStyle.dirSep(byte(c)) })
	current := workTree
	for _, part := range parts {
		current = filepath.Join(current, part)
		if info, err := lstatPath(current); err == nil && info.Mode()&fs.ModeSymlink != 0 {
			return fmt.Errorf("%w: %s", ErrSymlinkInPath, path)
		}
	}
	return nil
}

func ValidateGitDir(modulesDir, name string, isGitDir func(string) bool) error {
	for at := range len(name) {
		if !nativeStyle.dirSep(name[at]) {
			continue
		}
		if isGitDir(filepath.Join(modulesDir, filepath.FromSlash(name[:at]))) {
			return fmt.Errorf("%w: %s", ErrGitDirInsideGitDir, name)
		}
	}
	return nil
}
