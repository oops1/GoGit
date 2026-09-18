package ops

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/config"
	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/refspec"
	"github.com/oops1/gogit/internal/gitcore/remote"
	"github.com/oops1/gogit/internal/gitcore/repo"
)

func networkView(r *repo.Repository) (*repo.Repository, error) {
	return cloneRepoOpenLayout(repo.Layout{GitDir: r.GitDir(), CommonDir: r.CommonDir(), Bare: true}, r.Options())
}

func fetchRemote(ctx context.Context, r *repo.Repository, rem remote.Remote, opts remote.FetchOptions) (remote.FetchResult, error) {
	opts, err := withPromisorFilter(r, rem, opts)
	if err != nil {
		return remote.FetchResult{}, err
	}
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
	for _, change := range changes {
		if !change.Created || !change.Source.IsBranch() || !change.Pushed.IsBranch() {
			continue
		}
		branch := change.Source.Short()
		if existing, has := cfg.Branch(branch); has && existing.Remote != "" {
			continue
		}
		if err := setBranchUpstream(r, branch, remoteName, change.Pushed.String()); err != nil {
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
	fetch := file.GetAll("remote." + name + ".fetch")
	for _, key := range keysNamingRemote(file, name) {
		_ = file.UnsetAll(key)
	}
	_ = file.RemoveSection("remote." + name)
	err = file.Save(file.Path())
	if err == nil {
		err = removeTrackingRefs(r, fetch)
	}
	return err
}

func keysNamingRemote(file *config.File, name string) []string {
	var keys []string
	for v := range file.Variables() {
		if v.Value != name {
			continue
		}
		switch {
		case v.Section == "branch" && v.HasSubsection && strings.EqualFold(v.Key, "remote"):
			keys = append(keys, "branch."+v.Subsection+".remote", "branch."+v.Subsection+".merge")
		case v.Section == "branch" && v.HasSubsection && strings.EqualFold(v.Key, "pushRemote"):
			keys = append(keys, "branch."+v.Subsection+".pushRemote")
		case v.Section == "remote" && !v.HasSubsection && strings.EqualFold(v.Key, "pushDefault"):
			keys = append(keys, "remote.pushDefault")
		}
	}
	return keys
}

func removeTrackingRefs(r *repo.Repository, fetch []string) error {
	specs, err := refspec.ParseAll(fetch)
	if err != nil {
		return nil
	}
	rc, err := openRepoContext(r)
	if err != nil {
		return err
	}
	defer func() { _ = rc.close() }()
	tx := rc.refs.Begin()
	for ref, err := range rc.refs.All() {
		if err != nil {
			tx.Rollback()
			return err
		}
		tracked := slices.ContainsFunc(specs, func(spec refspec.RefSpec) bool {
			_, ok := spec.MatchDst(ref.Name.String())
			return ok
		})
		if tracked {
			_ = tx.Delete(ref.Name, hash.Zero)
		}
	}
	return tx.Commit()
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
