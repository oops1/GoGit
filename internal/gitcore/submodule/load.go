package submodule

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/index"
	"github.com/oops1/gogit/internal/gitcore/object"
)

const GitmodulesFile = ".gitmodules"

type BlobReader interface {
	Blob(id hash.ObjectID) (*object.Blob, error)
}

type Source struct {
	WorkTree string
	Index    *index.Index
	Objects  BlobReader
	HeadBlob hash.ObjectID
}

var (
	statGitmodulesFile = os.Stat
	readGitmodulesFile = os.ReadFile
)

func Load(src Source) (*Modules, error) {
	if src.WorkTree == "" || src.Index != nil && len(src.Index.Conflicts(GitmodulesFile)) > 0 {
		return newModules(), nil
	}
	data, found, err := gitmodulesData(src)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrReadGitmodules, err)
	}
	if !found {
		return newModules(), nil
	}
	return Parse(data)
}

func gitmodulesData(src Source) ([]byte, bool, error) {
	path := filepath.Join(src.WorkTree, GitmodulesFile)
	info, err := statGitmodulesFile(path)
	switch {
	case err == nil && info.IsDir():
		return nil, false, nil
	case err == nil:
		data, err := readGitmodulesFile(path)
		return data, err == nil, err
	case !errors.Is(err, fs.ErrNotExist):
		return nil, false, err
	}
	blob := src.HeadBlob
	if src.Index != nil {
		if entry, ok := src.Index.Get(GitmodulesFile, index.StageMerged); ok {
			blob = entry.ID
		}
	}
	if blob.IsZero() || src.Objects == nil {
		return nil, false, nil
	}
	content, err := src.Objects.Blob(blob)
	if err != nil {
		return nil, false, err
	}
	return content.Data, true, nil
}
