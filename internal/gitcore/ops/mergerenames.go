package ops

import (
	"github.com/oops1/gogit/internal/gitcore/merge"
	"github.com/oops1/gogit/internal/gitcore/repo"
)

const directoryRenamesKey = "merge.directoryRenames"

func mergeRenameLimit(r *repo.Repository) int {
	for _, key := range []string{"merge.renameLimit", "diff.renameLimit"} {
		if limit, err := r.Config().GetInt(key); err == nil {
			return int(limit)
		}
	}
	return merge.DefaultRenameLimit
}

func directoryRenamesMode(r *repo.Repository) merge.DirectoryRenames {
	value, set := r.Config().Get(directoryRenamesKey)
	if !set || value == "conflict" {
		return merge.DirectoryRenamesConflict
	}
	enabled, err := r.Config().GetBool(directoryRenamesKey)
	switch {
	case err != nil:
		return merge.DirectoryRenamesConflict
	case enabled:
		return merge.DirectoryRenamesApply
	}
	return merge.DirectoryRenamesOff
}
