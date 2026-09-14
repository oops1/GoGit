package ops

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/config"
	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/index"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/repo"
)

var ErrUnsafePath = index.ErrUnsafePath

var writeGuardRules = index.PathRules{
	ProtectNTFS: runtime.GOOS == "windows",
	ProtectHFS:  runtime.GOOS == "darwin",
	Windows:     runtime.GOOS == "windows",
}

func pathRulesOf(r *repo.Repository) (index.PathRules, error) {
	rules := index.DefaultPathRules()
	var err error
	if rules.ProtectNTFS, err = configFlag(r.Config(), "core.protectntfs", rules.ProtectNTFS); err != nil {
		return index.PathRules{}, err
	}
	if rules.ProtectHFS, err = configFlag(r.Config(), "core.protecthfs", rules.ProtectHFS); err != nil {
		return index.PathRules{}, err
	}
	return rules, nil
}

func configFlag(cfg *config.Config, key string, fallback bool) (bool, error) {
	if !cfg.Has(key) {
		return fallback, nil
	}
	return cfg.GetBool(key)
}

func verifiedTreeEntries(db *odb.DB, commitID hash.ObjectID, rules index.PathRules) (map[string]treeEntry, error) {
	entries, err := commitTreeEntries(db, commitID)
	if err != nil {
		return nil, err
	}
	return entries, verifyPaths(entries, func(entry treeEntry) object.Mode { return entry.mode }, rules)
}

func verifyPaths[E any](entries map[string]E, modeOf func(E) object.Mode, rules index.PathRules) error {
	for _, name := range slices.Sorted(maps.Keys(entries)) {
		if err := index.VerifyPath(name, modeOf(entries[name]), rules); err != nil {
			return err
		}
		for dir := parentOf(name); dir != ""; dir = parentOf(dir) {
			if _, file := entries[dir]; file {
				return fmt.Errorf("%w: %q is both a file and a directory", ErrUnsafePath, dir)
			}
		}
	}
	return nil
}

func ensureDirectories(root *os.Root, dir string) error {
	built := ""
	for part := range strings.SplitSeq(dir, "/") {
		built = joinRel(built, part)
		name := filepath.FromSlash(built)
		info, err := fsRootLstat(root, name)
		switch {
		case err == nil && info.Mode().Type() == os.ModeDir:
			continue
		case err == nil:
			if err := fsRootRemove(root, name); err != nil {
				return err
			}
		case !missingPath(err):
			return err
		}
		if err := fsRootMkdir(root, name, 0o777); err != nil {
			return err
		}
	}
	return nil
}
