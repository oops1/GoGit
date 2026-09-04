package ops

import (
	"context"
	"strings"
	"time"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/repo"
)

func Commit(ctx context.Context, r *repo.Repository, opts CommitOptions) (hash.ObjectID, error) {
	if err := ctx.Err(); err != nil {
		return hash.Zero, err
	}
	message := normalizeMessage(opts.Message)
	if message == "" {
		return hash.Zero, ErrEmptyMessage
	}

	rc, err := openRepoContext(r)
	if err != nil {
		return hash.Zero, err
	}
	defer func() { _ = rc.close() }()
	if err := rc.requireIdentity(); err != nil {
		return hash.Zero, err
	}

	lock, err := lockIndex(r)
	if err != nil {
		return hash.Zero, err
	}

	treeID, err := lock.idx.WriteTree(rc.db)
	if err != nil {
		lock.abort()
		return hash.Zero, err
	}

	target, err := resolveHeadTarget(rc.refs)
	if err != nil {
		lock.abort()
		return hash.Zero, err
	}

	parents, err := commitParents(rc.db, target.old, opts.Amend)
	if err != nil {
		lock.abort()
		return hash.Zero, err
	}

	empty, err := isEmptyCommit(rc.db, treeID, parents)
	if err != nil {
		lock.abort()
		return hash.Zero, err
	}
	if empty && !opts.AllowEmpty {
		lock.abort()
		return hash.Zero, ErrNothingToCommit
	}

	when := opts.When
	if when.IsZero() {
		when = time.Now()
	}
	author := rc.sig
	author.When = when
	if opts.Author != nil {
		author = *opts.Author
	}
	committer := rc.sig
	committer.When = when

	commit := &object.Commit{Tree: treeID, Parents: parents, Author: author, Committer: committer, Message: message}
	id, err := dbPutObject(rc.db, commit)
	if err != nil {
		lock.abort()
		return hash.Zero, err
	}

	tx := rc.refs.Begin()
	tx.SetMessage(commitReflogMessage(opts.Amend, parents, message))
	if err := txUpdate(tx, target.ref, id, target.old); err != nil {
		tx.Rollback()
		lock.abort()
		return hash.Zero, err
	}
	if err := tx.Commit(); err != nil {
		lock.abort()
		return hash.Zero, err
	}

	if err := lock.commit(); err != nil {
		return hash.Zero, err
	}
	return id, nil
}

func commitParents(db *odb.DB, headCommit hash.ObjectID, amend bool) ([]hash.ObjectID, error) {
	if !amend {
		if headCommit.IsZero() {
			return nil, nil
		}
		return []hash.ObjectID{headCommit}, nil
	}
	if headCommit.IsZero() {
		return nil, ErrUnbornHead
	}
	commit, err := db.Commit(headCommit)
	if err != nil {
		return nil, err
	}
	return commit.Parents, nil
}

func isEmptyCommit(db *odb.DB, treeID hash.ObjectID, parents []hash.ObjectID) (bool, error) {
	if len(parents) == 0 {
		tree, err := dbTree(db, treeID)
		if err != nil {
			return false, err
		}
		return len(tree.Entries) == 0, nil
	}
	parentCommit, err := db.Commit(parents[0])
	if err != nil {
		return false, err
	}
	return parentCommit.Tree == treeID, nil
}

func commitReflogMessage(amend bool, parents []hash.ObjectID, message string) string {
	kind := "commit"
	switch {
	case amend:
		kind = "commit (amend)"
	case len(parents) == 0:
		kind = "commit (initial)"
	case len(parents) > 1:
		kind = "commit (merge)"
	}
	return kind + ": " + firstLine(message)
}

func firstLine(message string) string {
	if idx := strings.IndexByte(message, '\n'); idx >= 0 {
		return message[:idx]
	}
	return message
}

func normalizeMessage(raw string) string {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	lines := strings.Split(raw, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.HasPrefix(line, "#") {
			continue
		}
		kept = append(kept, strings.TrimRight(line, " \t"))
	}
	out := make([]string, 0, len(kept))
	blank := true
	for _, line := range kept {
		if line == "" {
			if blank {
				continue
			}
			blank = true
			out = append(out, "")
			continue
		}
		blank = false
		out = append(out, line)
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	text := strings.Join(out, "\n")
	if text == "" {
		return ""
	}
	return text + "\n"
}
