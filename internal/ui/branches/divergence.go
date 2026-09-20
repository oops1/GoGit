package branches

import (
	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/repo"
	"github.com/oops1/gogit/internal/ui/style"
)

func DivergencePairs(s Snapshot) []repo.BranchPair {
	tracking := make(map[refs.Name]hash.ObjectID, len(s.Remotes))
	for _, remote := range s.Remotes {
		for _, b := range remote.Branches {
			tracking[b.Name] = b.Target
		}
	}
	var pairs []repo.BranchPair
	for _, b := range s.Local {
		if b.Upstream == "" {
			continue
		}
		remoteTarget, ok := tracking[b.Upstream]
		if !ok {
			continue
		}
		pairs = append(pairs, repo.BranchPair{Name: b.Name, Local: b.Target, Remote: remoteTarget})
	}
	return pairs
}

func divergenceSuffix(d repo.Divergence) string {
	switch {
	case d.Ahead > 0 && d.Behind > 0:
		return " " + i18n.Tf("Status.Diverged", d.Ahead, d.Behind)
	case d.Ahead > 0:
		return " " + i18n.Tf("Status.Ahead", d.Ahead)
	case d.Behind > 0:
		return " " + i18n.Tf("Status.Behind", d.Behind)
	default:
		return ""
	}
}

func (v *View) SetDivergence(next map[refs.Name]repo.Divergence) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.divergence = next
	if v.tree != nil {
		v.render(v.last)
	}
}

func (v *View) Restyle(t *widget.Theme) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.secondary = style.Of(t).Secondary
	if v.tree != nil {
		v.render(v.last)
	}
}
