package app

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/oops1/gogit/internal/gitcore/commitgraph"
	"github.com/oops1/gogit/internal/gitcore/ops"
	"github.com/oops1/gogit/internal/gitcore/revision"
)

const graphSettle = 2 * time.Second

var (
	openCommitGraph = ops.OpenCommitGraph
	statGraphFile   = os.Stat
	graphClock      = time.Now
)

func graphFilePaths(objectDirs []string) []string {
	paths := make([]string, 0, 2*len(objectDirs))
	for _, dir := range objectDirs {
		info := filepath.Join(dir, "info")
		paths = append(paths, filepath.Join(info, "commit-graph"), filepath.Join(info, "commit-graphs", "commit-graph-chain"))
	}
	return paths
}

func graphFilesKey(objectDirs []string) (string, bool) {
	var key strings.Builder
	settled := true
	now := graphClock()
	for _, path := range graphFilePaths(objectDirs) {
		info, err := statGraphFile(path)
		if err != nil {
			continue
		}
		key.WriteString(path + "|" + strconv.FormatInt(info.Size(), 10) + "|" + strconv.FormatInt(info.ModTime().UnixNano(), 10) + ";")
		if now.Sub(info.ModTime()) < graphSettle {
			settled = false
		}
	}
	return key.String(), settled
}

func (o *openedRepository) commitGraph() *commitgraph.Graph {
	key, settled := graphFilesKey(o.db.Dirs())
	o.graphMu.Lock()
	defer o.graphMu.Unlock()
	if o.graphReady && settled && key == o.graphKey {
		return o.graph
	}
	graph, _ := openCommitGraph(o.repo, o.db)
	o.graph, o.graphKey, o.graphReady = graph, key, settled
	return graph
}

func (o *openedRepository) revisionContext() revision.Context {
	return revision.Context{Objects: o.db, Refs: o.store, Shallow: o.shallow, Graph: o.commitGraph()}
}
