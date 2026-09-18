package ops

import (
	"github.com/oops1/gogit/internal/gitcore/commitgraph"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/repo"
)

func OpenCommitGraph(r *repo.Repository, db *odb.DB) (*commitgraph.Graph, error) {
	settings, err := r.CommitGraphSettings()
	if err != nil || !settings.Enabled {
		return nil, err
	}
	graph, _ := commitGraphOpen(db.Dirs(), settings.Open)
	return graph, nil
}
