package ops

import (
	"github.com/oops1/gogit/internal/gitcore/merge"
	"github.com/oops1/gogit/internal/gitcore/repo"
)

func mergeRenameLimit(r *repo.Repository) int {
	for _, key := range []string{"merge.renameLimit", "diff.renameLimit"} {
		if limit, err := r.Config().GetInt(key); err == nil {
			return int(limit)
		}
	}
	return merge.DefaultRenameLimit
}
