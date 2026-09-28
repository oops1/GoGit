package branches

import (
	"strings"

	"github.com/oops1/gogit/internal/gitcore/refs"
)

type remoteBranchRef struct {
	ref    refs.Name
	remote string
}

func remotePairs(local []Branch, remotes []Remote) (map[refs.Name]string, map[refs.Name]bool) {
	byRef := make(map[refs.Name]remoteBranchRef)
	byShortName := make(map[string][]remoteBranchRef)
	for _, remote := range remotes {
		for _, b := range remote.Branches {
			if isRemoteHead(b.Name) {
				continue
			}
			info := remoteBranchRef{ref: b.Name, remote: remote.Name}
			byRef[b.Name] = info
			short := strings.TrimPrefix(b.Name.Short(), remote.Name+"/")
			byShortName[short] = append(byShortName[short], info)
		}
	}

	pairedRemote := make(map[refs.Name]string)
	consumedRemote := make(map[refs.Name]bool)
	for _, b := range local {
		match, matched := remoteMatchFor(b, byRef, byShortName)
		if !matched {
			continue
		}
		pairedRemote[b.Name] = match.remote
		consumedRemote[match.ref] = true
	}
	return pairedRemote, consumedRemote
}

func remoteMatchFor(b Branch, byRef map[refs.Name]remoteBranchRef, byShortName map[string][]remoteBranchRef) (remoteBranchRef, bool) {
	if b.Upstream != "" {
		if info, ok := byRef[b.Upstream]; ok {
			return info, true
		}
	}
	candidates, ok := byShortName[b.Name.Short()]
	if !ok || len(candidates) == 0 {
		return remoteBranchRef{}, false
	}
	return candidates[0], true
}
