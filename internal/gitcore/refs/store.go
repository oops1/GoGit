package refs

import (
	"path/filepath"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/config"
	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
)

const (
	packedRefsFile   = "packed-refs"
	logsDir          = "logs"
	maxSymbolicDepth = 5
	keepRefDirs      = 2
)

type ObjectPeeler interface {
	PeelTag(id hash.ObjectID) (target hash.ObjectID, isTag bool, err error)
}

type ReflogPolicy uint8

const (
	ReflogDefault ReflogPolicy = iota
	ReflogEnabled
	ReflogDisabled
	ReflogAlways
)

func ReflogPolicyFromConfig(cfg *config.Config) (ReflogPolicy, error) {
	raw, ok := cfg.Get("core.logAllRefUpdates")
	if !ok {
		return ReflogDefault, nil
	}
	if strings.EqualFold(raw, "always") {
		return ReflogAlways, nil
	}
	enabled, err := config.ParseBool(raw)
	if err != nil {
		return ReflogDefault, err
	}
	if enabled {
		return ReflogEnabled, nil
	}
	return ReflogDisabled, nil
}

type Options struct {
	GitDir    string
	CommonDir string
	Bare      bool
	Reflog    ReflogPolicy
	Peeler    ObjectPeeler
	Committer func() object.Signature
}

type Store struct {
	git    tree
	common tree
	opts   Options
}

type Ref struct {
	Name           Name
	Target         hash.ObjectID
	SymbolicTarget Name
	Peeled         hash.ObjectID
}

func (r Ref) IsSymbolic() bool { return r.SymbolicTarget != "" }

func Open(opts Options) (*Store, error) {
	git, err := openTree(opts.GitDir)
	if err != nil {
		return nil, err
	}
	store := &Store{git: git, common: git, opts: opts}
	if opts.CommonDir == "" || filepath.Clean(opts.CommonDir) == filepath.Clean(opts.GitDir) {
		return store, nil
	}
	common, err := openTree(opts.CommonDir)
	if err != nil {
		_ = git.close()
		return nil, err
	}
	store.common = common
	return store, nil
}

func (s *Store) Close() error {
	err := s.git.close()
	if s.split() {
		if closeErr := s.common.close(); err == nil {
			err = closeErr
		}
	}
	return err
}

func (s *Store) split() bool { return s.common.root != s.git.root }

func (s *Store) treeFor(name Name) tree {
	if name.IsPerWorktree() {
		return s.git
	}
	return s.common
}
