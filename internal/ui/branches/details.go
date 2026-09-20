package branches

import (
	"time"

	gitconfig "github.com/oops1/gogit/internal/gitcore/config"
	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/refs"
)

type ObjectSource interface {
	Get(id hash.ObjectID) (object.Type, []byte, error)
}

type UpstreamSource interface {
	Branch(name string) (gitconfig.Branch, bool)
}

func LoadTimes(src ObjectSource, snap *Snapshot) {
	known := map[hash.ObjectID]time.Time{}
	for i := range snap.Local {
		snap.Local[i].When = commitTime(src, known, snap.Local[i].Target)
	}
	for r := range snap.Remotes {
		list := snap.Remotes[r].Branches
		for i := range list {
			list[i].When = commitTime(src, known, list[i].Target)
		}
	}
	for i := range snap.Tags {
		snap.Tags[i].When = commitTime(src, known, tagCommit(snap.Tags[i]))
	}
}

func tagCommit(t Tag) hash.ObjectID {
	if t.Peeled.IsZero() {
		return t.Target
	}
	return t.Peeled
}

func commitTime(src ObjectSource, known map[hash.ObjectID]time.Time, id hash.ObjectID) time.Time {
	if when, seen := known[id]; seen {
		return when
	}
	var when time.Time
	kind, data, err := src.Get(id)
	if err == nil && kind == object.TypeCommit {
		if commit, err := object.ParseCommit(data); err == nil {
			when = commit.Committer.When
		}
	}
	known[id] = when
	return when
}

func LoadUpstreams(src UpstreamSource, snap *Snapshot) {
	for i := range snap.Local {
		branch, ok := src.Branch(snap.Local[i].Name.Short())
		if !ok || branch.Remote == "" || len(branch.Merge) == 0 {
			continue
		}
		snap.Local[i].Upstream = refs.RemoteBranchName(branch.Remote, refs.Name(branch.Merge[0]).Short())
	}
}
