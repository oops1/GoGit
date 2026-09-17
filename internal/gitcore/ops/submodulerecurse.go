package ops

import (
	"context"
	"errors"

	"github.com/oops1/gogit/internal/gitcore/config"
	"github.com/oops1/gogit/internal/gitcore/repo"
)

func recurseSubmodulesConfigured(r *repo.Repository, explicit bool) (bool, error) {
	if explicit {
		return true, nil
	}
	recurse, err := r.Config().GetBool(submoduleRecurseKey)
	if errors.Is(err, config.ErrNotFound) {
		return false, nil
	}
	return recurse, err
}

func changedGitlinks(headTree, targetTree map[string]treeEntry) map[string]bool {
	changed := map[string]bool{}
	for path, target := range targetTree {
		if !target.mode.IsSubmodule() {
			continue
		}
		if head, ok := headTree[path]; !ok || head != target {
			changed[path] = true
		}
	}
	return changed
}

func updateSwitchedSubmodules(ctx context.Context, r *repo.Repository, headTree, targetTree map[string]treeEntry, opts SwitchOptions) error {
	recurse, err := recurseSubmodulesConfigured(r, opts.RecurseSubmodules)
	if err != nil || !recurse {
		return err
	}
	changed := changedGitlinks(headTree, targetTree)
	if len(changed) == 0 {
		return nil
	}
	return SubmoduleUpdate(ctx, r, nil, SubmoduleUpdateOptions{
		Mode:          SubmoduleUpdateCheckout,
		NoFetch:       true,
		Force:         opts.Force,
		Events:        opts.SubmoduleEvents,
		populatedOnly: true,
		onlyPaths:     changed,
	})
}

func updatePulledSubmodules(ctx context.Context, r *repo.Repository, branch string, opts PullOptions) error {
	recurse, err := recurseSubmodulesConfigured(r, opts.RecurseSubmodules)
	if err != nil || !recurse {
		return err
	}
	mode := SubmoduleUpdateCheckout
	if pullRebasesSubmodules(r.Config(), branch) {
		mode = SubmoduleUpdateRebase
	}
	return SubmoduleUpdate(ctx, r, nil, SubmoduleUpdateOptions{
		Mode:      mode,
		Recursive: true,
		Progress:  opts.Progress,
		Transport: opts.Fetch.Transport,
		Events:    opts.SubmoduleEvents,
	})
}
