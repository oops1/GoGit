package ops

import (
	"context"
	"fmt"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/config"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/refspec"
	"github.com/oops1/gogit/internal/gitcore/remote"
	"github.com/oops1/gogit/internal/gitcore/repo"
)

func networkView(r *repo.Repository) (*repo.Repository, error) {
	return cloneRepoOpenLayout(repo.Layout{GitDir: r.GitDir(), CommonDir: r.CommonDir(), Bare: true}, repo.OpenOptions{})
}

func fetchRemote(ctx context.Context, r *repo.Repository, rem remote.Remote, opts remote.FetchOptions) (remote.FetchResult, error) {
	view, err := networkView(r)
	if err != nil {
		return remote.FetchResult{}, err
	}
	result, fetchErr := remote.Fetch(ctx, view, rem, opts)
	closeErr := view.Close()
	if fetchErr != nil {
		return remote.FetchResult{}, fetchErr
	}
	if closeErr != nil {
		return remote.FetchResult{}, closeErr
	}
	return result, nil
}

func pushRemote(ctx context.Context, r *repo.Repository, rem remote.Remote, opts remote.PushOptions) (remote.PushResult, error) {
	view, err := networkView(r)
	if err != nil {
		return remote.PushResult{}, err
	}
	result, pushErr := remote.Push(ctx, view, rem, opts)
	closeErr := view.Close()
	if pushErr != nil {
		return result, pushErr
	}
	if closeErr != nil {
		return result, closeErr
	}
	return result, nil
}

func localConfigFile(r *repo.Repository) (*config.File, error) {
	file, ok := r.Config().File(config.LevelLocal)
	if !ok {
		return nil, ErrNoLocalConfig
	}
	return file, nil
}

func hasConfigSubsection(file *config.File, section, sub string) bool {
	for v := range file.Variables() {
		if v.Section == section && v.HasSubsection && v.Subsection == sub {
			return true
		}
	}
	return false
}

func validateRemoteName(name string) error {
	if name == "" || strings.ContainsAny(name, "\n\t ") {
		return fmt.Errorf("%w: %q", ErrInvalidRemoteName, name)
	}
	return nil
}

func currentBranchShort(r *repo.Repository) (string, bool, error) {
	store, err := refsOpen(refs.Options{GitDir: r.GitDir(), CommonDir: r.CommonDir(), Bare: r.IsBare()})
	if err != nil {
		return "", false, err
	}
	defer func() { _ = store.Close() }()
	branchRef, err := currentBranchRef(store)
	if err != nil {
		return "", false, err
	}
	if branchRef == "" {
		return "", false, nil
	}
	return branchRef.Short(), true, nil
}

func resolveRemoteName(r *repo.Repository, cfg *config.Config, explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	branch, ok, err := currentBranchShort(r)
	if err != nil {
		return "", err
	}
	if ok {
		if b, bok := cfg.Branch(branch); bok && b.Remote != "" {
			return b.Remote, nil
		}
	}
	return defaultCloneRemoteName, nil
}

func setBranchUpstream(r *repo.Repository, branch, remoteName, mergeRef string) error {
	file, err := localConfigFile(r)
	if err != nil {
		return err
	}
	if err := file.Set("branch."+branch+".remote", remoteName); err != nil {
		return err
	}
	if err := file.Set("branch."+branch+".merge", mergeRef); err != nil {
		return err
	}
	return file.Save(file.Path())
}

func setPushedUpstreams(r *repo.Repository, cfg *config.Config, remoteName string, changes []remote.Change) error {
	prefix := remoteName + "/"
	for _, change := range changes {
		if change.Deleted || !change.Created {
			continue
		}
		branch := strings.TrimPrefix(change.Name.Short(), prefix)
		if existing, has := cfg.Branch(branch); has && existing.Remote != "" {
			continue
		}
		if err := setBranchUpstream(r, branch, remoteName, refs.BranchName(branch).String()); err != nil {
			return err
		}
	}
	return nil
}

func AddRemote(r *repo.Repository, name, url string) error {
	if err := validateRemoteName(name); err != nil {
		return err
	}
	file, err := localConfigFile(r)
	if err != nil {
		return err
	}
	if hasConfigSubsection(file, "remote", name) {
		return fmt.Errorf("%w: %s", ErrRemoteExists, name)
	}
	if err := file.Set("remote."+name+".url", url); err != nil {
		return err
	}
	if err := file.Set("remote."+name+".fetch", refspec.DefaultFetch(name).String()); err != nil {
		return err
	}
	return file.Save(file.Path())
}

func RemoveRemote(r *repo.Repository, name string) error {
	file, err := localConfigFile(r)
	if err != nil {
		return err
	}
	if !hasConfigSubsection(file, "remote", name) {
		return fmt.Errorf("%w: %s", remote.ErrNoRemote, name)
	}
	if err := file.RemoveSection("remote." + name); err != nil {
		return err
	}
	return file.Save(file.Path())
}

func SetRemoteURL(r *repo.Repository, name, url string, push bool) error {
	file, err := localConfigFile(r)
	if err != nil {
		return err
	}
	if !hasConfigSubsection(file, "remote", name) {
		return fmt.Errorf("%w: %s", remote.ErrNoRemote, name)
	}
	key := "remote." + name + ".url"
	if push {
		key = "remote." + name + ".pushurl"
	}
	if err := file.Set(key, url); err != nil {
		return err
	}
	return file.Save(file.Path())
}
