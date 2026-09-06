package ops

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/oops1/gogit/internal/gitcore/config"
	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/progress"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/refspec"
	"github.com/oops1/gogit/internal/gitcore/remote"
	"github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/gitcore/transport"
)

var (
	ErrCloneTargetNotEmpty  = errors.New("ops: clone target directory exists and is not empty")
	ErrRemoteBranchNotFound = errors.New("ops: remote repository has no such branch")
)

const (
	defaultCloneRemoteName = "origin"
	cloneCommitterName     = "gogit"
	cloneCommitterEmail    = "gogit@localhost"
	directoryMode          = 0o777
)

var (
	cloneRepoInit       = repo.Init
	cloneRepoOpen       = repo.Open
	cloneRepoOpenLayout = repo.OpenLayout
	cloneReadDir        = os.ReadDir
	cloneMkdirAll       = os.MkdirAll
)

type CloneOptions struct {
	Bare         bool
	Branch       string
	SingleBranch bool
	Depth        int
	RemoteName   string
	NoCheckout   bool
	Progress     progress.Func
	Transport    transport.Options
}

type cloneTarget struct {
	branch   string
	commit   hash.ObjectID
	detached bool
}

func Clone(ctx context.Context, url, dir string, opts CloneOptions) (*repo.Repository, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	created, err := prepareCloneDirectory(dir)
	if err != nil {
		return nil, err
	}
	r, err := cloneInto(ctx, url, dir, opts)
	if err != nil {
		cleanupCloneDirectory(dir, created)
		return nil, err
	}
	return r, nil
}

func cloneRemoteName(opts CloneOptions) string {
	if opts.RemoteName != "" {
		return opts.RemoteName
	}
	return defaultCloneRemoteName
}

func prepareCloneDirectory(dir string) (bool, error) {
	entries, err := cloneReadDir(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		if err := cloneMkdirAll(dir, directoryMode); err != nil {
			return false, err
		}
		return true, nil
	case err != nil:
		return false, err
	case len(entries) > 0:
		return false, fmt.Errorf("%w: %s", ErrCloneTargetNotEmpty, dir)
	default:
		return false, nil
	}
}

func cleanupCloneDirectory(dir string, created bool) {
	if created {
		_ = os.RemoveAll(dir)
		return
	}
	entries, err := cloneReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		_ = os.RemoveAll(filepath.Join(dir, entry.Name()))
	}
}

func failClone(r *repo.Repository, err error) (*repo.Repository, error) {
	_ = r.Close()
	return nil, err
}

func cloneInto(ctx context.Context, url, dir string, opts CloneOptions) (*repo.Repository, error) {
	remoteName := cloneRemoteName(opts)
	prog := opts.Progress
	prog.Phase("init")

	r, err := cloneRepoInit(dir, repo.InitOptions{Bare: opts.Bare})
	if err != nil {
		return nil, err
	}

	spec, explicitBranch, err := planCloneFetch(ctx, url, remoteName, opts)
	if err != nil {
		return failClone(r, err)
	}

	file, err := localConfigFile(r)
	if err != nil {
		return failClone(r, err)
	}
	if err := writeCloneRemoteConfig(file, remoteName, url, spec); err != nil {
		return failClone(r, err)
	}

	rem := remote.Remote{Name: remoteName, URLs: []string{url}, Fetch: []refspec.RefSpec{spec}}
	result, err := fetchCloneObjects(ctx, r, rem, remote.FetchOptions{
		Depth:     opts.Depth,
		Progress:  prog,
		Transport: opts.Transport,
	})
	if err != nil {
		return failClone(r, err)
	}

	target, err := resolveCloneTarget(opts, remoteName, explicitBranch, result)
	if err != nil {
		return failClone(r, err)
	}

	if err := finishCloneRefs(r, opts.Bare, target, url); err != nil {
		return failClone(r, err)
	}
	if !target.detached && !opts.Bare && !target.commit.IsZero() {
		if err := writeCloneBranchConfig(file, remoteName, target.branch); err != nil {
			return failClone(r, err)
		}
	}

	reopened, err := cloneRepoOpen(dir, repo.OpenOptions{})
	if err != nil {
		return failClone(r, err)
	}
	_ = r.Close()
	r = reopened

	if !opts.Bare && !opts.NoCheckout && !target.commit.IsZero() {
		if err := CheckoutTree(ctx, r, target.commit, CheckoutOptions{Progress: prog}); err != nil {
			return failClone(r, err)
		}
	}
	return r, nil
}

func fetchCloneObjects(ctx context.Context, r *repo.Repository, rem remote.Remote, opts remote.FetchOptions) (remote.FetchResult, error) {
	fetchRepo, err := cloneRepoOpenLayout(repo.Layout{GitDir: r.GitDir(), CommonDir: r.CommonDir(), Bare: true}, repo.OpenOptions{})
	if err != nil {
		return remote.FetchResult{}, err
	}
	result, fetchErr := remote.Fetch(ctx, fetchRepo, rem, opts)
	closeErr := fetchRepo.Close()
	if fetchErr != nil {
		return remote.FetchResult{}, fetchErr
	}
	if closeErr != nil {
		return remote.FetchResult{}, closeErr
	}
	return result, nil
}

func planCloneFetch(ctx context.Context, url, remoteName string, opts CloneOptions) (refspec.RefSpec, string, error) {
	if !opts.SingleBranch {
		return wildcardCloneRefspec(remoteName, opts.Bare), "", nil
	}
	branch := opts.Branch
	if branch == "" {
		refsList, err := remote.LsRemote(ctx, url, opts.Transport)
		if err != nil {
			return refspec.RefSpec{}, "", err
		}
		branch = defaultBranchFrom(refsList)
	}
	if branch == "" {
		return wildcardCloneRefspec(remoteName, opts.Bare), "", nil
	}
	return singleCloneRefspec(remoteName, branch, opts.Bare), branch, nil
}

func defaultBranchFrom(refsList []transport.Ref) string {
	for _, ref := range refsList {
		if ref.Name == string(refs.HEAD) && refs.Name(ref.Symref).IsBranch() {
			return refs.Name(ref.Symref).Short()
		}
	}
	return ""
}

func wildcardCloneRefspec(remoteName string, bare bool) refspec.RefSpec {
	if bare {
		return refspec.RefSpec{Force: true, Src: refs.HeadsPrefix + "*", Dst: refs.HeadsPrefix + "*"}
	}
	return refspec.DefaultFetch(remoteName)
}

func singleCloneRefspec(remoteName, branch string, bare bool) refspec.RefSpec {
	src := refs.BranchName(branch).String()
	dst := src
	if !bare {
		dst = refs.RemoteBranchName(remoteName, branch).String()
	}
	return refspec.RefSpec{Force: true, Src: src, Dst: dst}
}

func remoteTrackingName(remoteName, branch string, bare bool) refs.Name {
	if bare {
		return refs.BranchName(branch)
	}
	return refs.RemoteBranchName(remoteName, branch)
}

func changeTarget(changes []remote.Change, name refs.Name) (hash.ObjectID, bool) {
	for _, change := range changes {
		if change.Name == name && !change.Deleted {
			return change.New, true
		}
	}
	return hash.Zero, false
}

func findAdvertisedRef(refsList []transport.Ref, name string) (transport.Ref, bool) {
	for _, ref := range refsList {
		if ref.Name == name {
			return ref, true
		}
	}
	return transport.Ref{}, false
}

func resolveCloneTarget(opts CloneOptions, remoteName, explicitBranch string, result remote.FetchResult) (cloneTarget, error) {
	branch := opts.Branch
	explicit := branch != ""
	if branch == "" {
		branch = explicitBranch
		explicit = branch != ""
	}
	if branch == "" && refs.Name(result.Head).IsBranch() {
		branch = refs.Name(result.Head).Short()
	}
	if branch != "" {
		dst := remoteTrackingName(remoteName, branch, opts.Bare)
		if commit, ok := changeTarget(result.Changes, dst); ok {
			return cloneTarget{branch: branch, commit: commit}, nil
		}
		if explicit {
			return cloneTarget{}, fmt.Errorf("%w: %s", ErrRemoteBranchNotFound, branch)
		}
	}
	if headRef, ok := findAdvertisedRef(result.Refs, string(refs.HEAD)); ok && !headRef.ID.IsZero() {
		return cloneTarget{commit: headRef.ID, detached: true}, nil
	}
	return cloneTarget{}, nil
}

func writeCloneRemoteConfig(file *config.File, remoteName, url string, spec refspec.RefSpec) error {
	err := errors.Join(
		file.Set("remote."+remoteName+".url", url),
		file.Set("remote."+remoteName+".fetch", spec.String()),
	)
	if err != nil {
		return err
	}
	return file.Save(file.Path())
}

func writeCloneBranchConfig(file *config.File, remoteName, branch string) error {
	err := errors.Join(
		file.Set("branch."+branch+".remote", remoteName),
		file.Set("branch."+branch+".merge", refs.BranchName(branch).String()),
	)
	if err != nil {
		return err
	}
	return file.Save(file.Path())
}

func cloneCommitter(r *repo.Repository) func() object.Signature {
	return func() object.Signature {
		user := r.Config().User()
		name, email := user.Name, user.Email
		if name == "" {
			name = cloneCommitterName
		}
		if email == "" {
			email = cloneCommitterEmail
		}
		return object.Signature{Name: name, Email: email, When: time.Now()}
	}
}

func finishCloneRefs(r *repo.Repository, bare bool, target cloneTarget, url string) error {
	if target.commit.IsZero() {
		return nil
	}
	store, err := refsOpen(refs.Options{
		GitDir:    r.GitDir(),
		CommonDir: r.CommonDir(),
		Bare:      r.IsBare(),
		Committer: cloneCommitter(r),
	})
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()

	tx := store.Begin()
	tx.SetMessage("clone: from " + url)
	if target.detached {
		if err := txDetach(tx, refs.HEAD, target.commit); err != nil {
			tx.Rollback()
			return err
		}
	} else {
		branchRef := refs.BranchName(target.branch)
		if !bare {
			if err := txUpdate(tx, branchRef, target.commit, hash.Zero); err != nil {
				tx.Rollback()
				return err
			}
		}
		if err := txSetSymbolic(tx, refs.HEAD, branchRef); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}
