package repo

import (
	"bufio"
	"bytes"
	"errors"
	"io/fs"
	"os"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/commitgraph"
	"github.com/oops1/gogit/internal/gitcore/config"
)

const (
	coreCommitGraphKey       = "core.commitGraph"
	generationVersionKey     = "commitGraph.generationVersion"
	readChangedPathsKey      = "commitGraph.readChangedPaths"
	useReplaceRefsKey        = "core.useReplaceRefs"
	noReplaceObjectsEnv      = "GIT_NO_REPLACE_OBJECTS"
	graftFileEnv             = "GIT_GRAFT_FILE"
	graftsFileName           = "info/grafts"
	replaceRefsDir           = "refs/replace"
	packedRefsFileName       = "packed-refs"
	packedReplacePrefix      = " refs/replace/"
	defaultGenerationVersion = 2
	graftComment             = '#'
)

type CommitGraphSettings struct {
	Enabled           bool
	GenerationVersion int
	Open              commitgraph.OpenOptions
}

func (r *Repository) CommitGraphSettings() (CommitGraphSettings, error) {
	enabled, err := boolSetting(r.cfg, coreCommitGraphKey, true)
	if err != nil {
		return CommitGraphSettings{}, err
	}
	readPaths, err := boolSetting(r.cfg, readChangedPathsKey, true)
	if err != nil {
		return CommitGraphSettings{}, err
	}
	version := int64(defaultGenerationVersion)
	if r.cfg.Has(generationVersionKey) {
		if version, err = r.cfg.GetInt(generationVersionKey); err != nil {
			return CommitGraphSettings{}, err
		}
	}
	compatible, err := r.commitGraphCompatible()
	if err != nil {
		return CommitGraphSettings{}, err
	}
	return CommitGraphSettings{
		Enabled:           enabled && compatible,
		GenerationVersion: int(version),
		Open: commitgraph.OpenOptions{
			SkipGenerationData: version < defaultGenerationVersion,
			SkipChangedPaths:   !readPaths,
		},
	}, nil
}

func boolSetting(cfg *config.Config, key string, fallback bool) (bool, error) {
	if !cfg.Has(key) {
		return fallback, nil
	}
	return cfg.GetBool(key)
}

func (r *Repository) env(key string) string {
	if r.opts.Env != nil {
		return r.opts.Env(key)
	}
	return os.Getenv(key)
}

func (r *Repository) commitGraphCompatible() (bool, error) {
	replaced, err := r.usesReplaceRefs()
	if err != nil || replaced {
		return false, err
	}
	grafted, err := r.hasGrafts()
	if err != nil || grafted {
		return false, err
	}
	shallow, err := r.IsShallow()
	return !shallow, err
}

func (r *Repository) usesReplaceRefs() (bool, error) {
	use, err := boolSetting(r.cfg, useReplaceRefsKey, true)
	if err != nil || !use || r.env(noReplaceObjectsEnv) != "" {
		return false, err
	}
	found := false
	_ = fs.WalkDir(r.commonRoot.FS(), replaceRefsDir, func(_ string, entry fs.DirEntry, err error) error {
		if err == nil && !entry.IsDir() {
			found = true
			return fs.SkipAll
		}
		return err
	})
	if found {
		return true, nil
	}
	packed, err := r.commonRoot.ReadFile(packedRefsFileName)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return bytes.Contains(packed, []byte(packedReplacePrefix)), err
}

func (r *Repository) hasGrafts() (bool, error) {
	var data []byte
	var err error
	if custom := r.env(graftFileEnv); custom != "" {
		data, err = os.ReadFile(custom)
	} else {
		data, err = r.commonRoot.ReadFile(graftsFileName)
	}
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" && line[0] != graftComment {
			return true, nil
		}
	}
	return false, nil
}
