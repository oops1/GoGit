package ops

import (
	"context"

	"github.com/oops1/gogit/internal/gitcore/diff"
	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/gitcore/revision"
)

type HistoryOptions struct {
	Follow   bool
	MaxCount int
	Diff     diff.Options
}

type HistoryEntry struct {
	Commit hash.ObjectID
	Author object.Signature
	When   object.Signature
	Path   string
	Old    string
}

func (e HistoryEntry) Renamed() bool { return e.Old != "" && e.Old != e.Path }

func FileHistory(ctx context.Context, r *repo.Repository, rev, path string, opts HistoryOptions) ([]HistoryEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	rc, err := openRepoContext(r)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.close() }()

	clean, err := cleanRepoPath(path)
	if err != nil {
		return nil, err
	}
	start, err := resolveCommittish(rc, rev)
	if err != nil {
		return nil, err
	}
	h := &historian{ctx: ctx, rc: rc, opts: opts}
	return h.collect(start, clean)
}

type historian struct {
	ctx  context.Context
	rc   *repoContext
	opts HistoryOptions
	out  []HistoryEntry
}

func (h *historian) collect(start hash.ObjectID, path string) ([]HistoryEntry, error) {
	for {
		next, older, err := h.walkPath(start, path)
		if err != nil {
			return nil, err
		}
		if next.IsZero() {
			return h.out, nil
		}
		start, path = next, older
	}
}

func (h *historian) walkPath(start hash.ObjectID, path string) (hash.ObjectID, string, error) {
	walk := revision.Walk(h.ctx, revision.Options{
		Context: revision.Context{Objects: mergeStore{db: h.rc.db}},
		Include: []hash.ObjectID{start},
		Paths:   []string{path},
	})
	for commit, err := range walk {
		if err != nil {
			return hash.Zero, "", err
		}
		if h.full() {
			return hash.Zero, "", nil
		}
		entry := HistoryEntry{Commit: commit.ID, Author: commit.Author, When: commit.Committer, Path: path, Old: path}
		older, renamed, err := h.renamedIn(commit, path)
		if err != nil {
			return hash.Zero, "", err
		}
		if renamed {
			entry.Old = older
		}
		h.out = append(h.out, entry)
		if renamed {
			return commit.Parents[0], older, nil
		}
	}
	return hash.Zero, "", nil
}

func (h *historian) full() bool {
	return h.opts.MaxCount > 0 && len(h.out) >= h.opts.MaxCount
}

func (h *historian) renamedIn(commit *revision.Commit, path string) (string, bool, error) {
	if !h.opts.Follow || len(commit.Parents) != 1 {
		return "", false, nil
	}
	parent, err := dbCommit(h.rc.db, commit.Parents[0])
	if err != nil {
		return "", false, err
	}
	opts := h.opts.Diff
	if opts.RenameThreshold == 0 {
		opts = diff.Defaults()
	}
	opts.DetectRenames, opts.Paths = true, nil
	files, err := diff.Trees(h.ctx, mergeStore{db: h.rc.db}, parent.Tree, commit.Tree, opts)
	if err != nil {
		return "", false, err
	}
	for _, file := range files {
		if file.NewPath == path && file.Status == diff.StatusRenamed {
			return file.OldPath, true, nil
		}
	}
	return "", false, nil
}
