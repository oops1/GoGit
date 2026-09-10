package ops

import (
	"context"
	"errors"
	"fmt"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/gitcore/revision"
	"github.com/oops1/gogit/internal/gitcore/trailer"
)

var ErrUnsignedCommit = errors.New("ops: commit has no author")

const attributionReflogMessage = "attribution: drop trailers that name other people"

type AttributionResult struct {
	Branch    refs.Name
	Old       hash.ObjectID
	New       hash.ObjectID
	Rewritten int
}

func StripAttribution(ctx context.Context, r *repo.Repository, branch refs.Name) (AttributionResult, error) {
	rc, err := openRepoContext(r)
	if err != nil {
		return AttributionResult{}, err
	}
	defer func() { _ = rc.close() }()

	tip, err := branchTip(rc, branch)
	if err != nil || tip.IsZero() {
		return AttributionResult{Branch: branch}, err
	}
	published, err := publishedTips(rc)
	if err != nil {
		return AttributionResult{Branch: branch}, err
	}
	shallow, err := r.Shallow()
	if err != nil {
		return AttributionResult{Branch: branch}, err
	}

	rewritten := map[hash.ObjectID]hash.ObjectID{}
	changed := 0
	walk := revision.Options{
		Context: revision.Context{Objects: rc.db, Shallow: shallow},
		Include: []hash.ObjectID{tip},
		Exclude: published,
		Order:   revision.Topo,
		Reverse: true,
	}
	for commit, err := range revision.Walk(ctx, walk) {
		if err != nil {
			return AttributionResult{Branch: branch}, err
		}
		if commit.Author.Name == "" || commit.Author.Email == "" {
			return AttributionResult{Branch: branch}, fmt.Errorf("%w: %s", ErrUnsignedCommit, commit.ID)
		}
		id, err := rewriteCommit(rc, commit, rewritten)
		if err != nil {
			return AttributionResult{Branch: branch}, err
		}
		if id != commit.ID {
			rewritten[commit.ID] = id
			changed++
		}
	}
	result := AttributionResult{Branch: branch, Old: tip, New: tip, Rewritten: changed}
	if changed == 0 {
		return result, nil
	}
	result.New = rewritten[tip]
	tx := rc.refs.Begin()
	tx.SetMessage(attributionReflogMessage)
	if err := txUpdate(tx, branch, result.New, tip); err != nil {
		tx.Rollback()
		return AttributionResult{Branch: branch}, err
	}
	if err := tx.Commit(); err != nil {
		return AttributionResult{Branch: branch}, err
	}
	return result, nil
}

func branchTip(rc *repoContext, branch refs.Name) (hash.ObjectID, error) {
	ref, err := rc.refs.Lookup(branch)
	if errors.Is(err, refs.ErrNotFound) {
		return hash.Zero, nil
	}
	if err != nil {
		return hash.Zero, err
	}
	return ref.Target, nil
}

func publishedTips(rc *repoContext) ([]hash.ObjectID, error) {
	var tips []hash.ObjectID
	for ref, err := range rc.refs.Prefix(refs.RemotesPrefix) {
		if err != nil {
			return nil, err
		}
		if !ref.Target.IsZero() {
			tips = append(tips, ref.Target)
		}
	}
	return tips, nil
}

func rewriteCommit(rc *repoContext, commit *revision.Commit, rewritten map[hash.ObjectID]hash.ObjectID) (hash.ObjectID, error) {
	message := trailer.WithoutAttribution(commit.Message)
	parents := make([]hash.ObjectID, len(commit.Parents))
	moved := false
	for i, parent := range commit.Parents {
		parents[i] = parent
		if replacement, ok := rewritten[parent]; ok {
			parents[i] = replacement
			moved = true
		}
	}
	if !moved && message == commit.Message {
		return commit.ID, nil
	}
	rebuilt := &object.Commit{
		Tree:      commit.Tree,
		Parents:   parents,
		Author:    commit.Author,
		Committer: commit.Committer,
		Encoding:  commit.Encoding,
		Extra:     commit.Extra,
		Message:   message,
	}
	return dbPutObject(rc.db, rebuilt)
}
