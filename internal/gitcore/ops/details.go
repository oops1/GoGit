package ops

import (
	"context"
	"slices"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/diff"
	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/gitcore/revision"
)

const defaultFileLimit = 2000

type DetailsOptions struct {
	Diff      diff.Options
	FileLimit int
}

type TreeFile struct {
	Path string
	Mode object.Mode
	Size int
}

type CommitDetails struct {
	Commit    hash.ObjectID
	Tree      hash.ObjectID
	Parents   []hash.ObjectID
	Author    object.Signature
	Committer object.Signature
	Message   string
	Branches  []string
	Tags      []string
	Changes   []diff.File
	Files     []TreeFile
	MoreFiles bool
}

func (d CommitDetails) Subject() string { return firstLine(d.Message) }

func Details(ctx context.Context, r *repo.Repository, rev string, opts DetailsOptions) (CommitDetails, error) {
	if err := ctx.Err(); err != nil {
		return CommitDetails{}, err
	}
	rc, err := openRepoContext(r)
	if err != nil {
		return CommitDetails{}, err
	}
	defer func() { _ = rc.close() }()

	id, err := resolveCommittish(rc, rev)
	if err != nil {
		return CommitDetails{}, err
	}
	commit, err := dbCommit(rc.db, id)
	if err != nil {
		return CommitDetails{}, err
	}
	details := CommitDetails{
		Commit:    id,
		Tree:      commit.Tree,
		Parents:   commit.Parents,
		Author:    commit.Author,
		Committer: commit.Committer,
		Message:   commit.Message,
	}
	if details.Branches, details.Tags, err = refsAround(ctx, rc, id); err != nil {
		return CommitDetails{}, err
	}
	if details.Changes, err = changesOfCommit(ctx, rc, commit, opts); err != nil {
		return CommitDetails{}, err
	}
	details.Files, details.MoreFiles, err = filesOfTree(ctx, rc, commit.Tree, fileLimitOf(opts))
	if err != nil {
		return CommitDetails{}, err
	}
	return details, nil
}

func fileLimitOf(opts DetailsOptions) int {
	if opts.FileLimit > 0 {
		return opts.FileLimit
	}
	return defaultFileLimit
}

func refsAround(ctx context.Context, rc *repoContext, commit hash.ObjectID) ([]string, []string, error) {
	var branches, tags []string
	for ref, err := range rc.refs.Prefix(refs.RefsPrefix) {
		if err != nil {
			return nil, nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		switch {
		case ref.Name.IsBranch():
			contains, err := revision.IsAncestor(revision.Context{Objects: mergeStore{db: rc.db}}, commit, ref.Target)
			if err != nil {
				return nil, nil, err
			}
			if contains {
				branches = append(branches, ref.Name.Short())
			}
		case ref.Name.IsTag() && pointsAt(ref, commit):
			tags = append(tags, ref.Name.Short())
		}
	}
	slices.SortFunc(branches, strings.Compare)
	slices.SortFunc(tags, strings.Compare)
	return branches, tags, nil
}

func pointsAt(ref refs.Ref, commit hash.ObjectID) bool {
	return ref.Target == commit || ref.Peeled == commit
}

func changesOfCommit(ctx context.Context, rc *repoContext, commit *object.Commit, opts DetailsOptions) ([]diff.File, error) {
	options := opts.Diff
	if options.RenameThreshold == 0 {
		options = diff.Defaults()
	}
	parentTree := hash.Zero
	if len(commit.Parents) > 0 {
		parent, err := dbCommit(rc.db, commit.Parents[0])
		if err != nil {
			return nil, err
		}
		parentTree = parent.Tree
	}
	return diff.Trees(ctx, mergeStore{db: rc.db}, parentTree, commit.Tree, options)
}

func filesOfTree(ctx context.Context, rc *repoContext, tree hash.ObjectID, limit int) ([]TreeFile, bool, error) {
	var files []TreeFile
	more, err := walkTree(ctx, rc, tree, "", limit, &files)
	return files, more, err
}

func walkTree(ctx context.Context, rc *repoContext, tree hash.ObjectID, prefix string, limit int, into *[]TreeFile) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	parsed, err := dbTree(rc.db, tree)
	if err != nil {
		return false, err
	}
	for _, entry := range parsed.Entries {
		path := prefix + entry.Name
		if entry.Mode == object.ModeTree {
			more, err := walkTree(ctx, rc, entry.ID, path+"/", limit, into)
			if err != nil || more {
				return more, err
			}
			continue
		}
		if len(*into) >= limit {
			return true, nil
		}
		size, err := blobSize(rc, entry)
		if err != nil {
			return false, err
		}
		*into = append(*into, TreeFile{Path: path, Mode: entry.Mode, Size: size})
	}
	return false, nil
}

func blobSize(rc *repoContext, entry object.TreeEntry) (int, error) {
	if entry.Mode == object.ModeSubmodule {
		return 0, nil
	}
	_, data, err := dbGet(rc.db, entry.ID)
	if err != nil {
		return 0, err
	}
	return len(data), nil
}
