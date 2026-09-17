package ops

import (
	"context"
	"iter"

	"github.com/oops1/gogit/internal/gitcore/config"
	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/linelog"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/repo"
)

type LineHistoryOptions struct {
	NoRenames bool
	Progress  func(done, total int)
}

type LineHistoryEntry struct {
	Commit    hash.ObjectID
	Parents   []hash.ObjectID
	Author    object.Signature
	Committer object.Signature
	Subject   string
	Files     []linelog.File
}

type LineSelection struct {
	Path  string
	First int
	Last  int
	Shown []byte
}

func LineHistory(ctx context.Context, r *repo.Repository, rev string, specs []linelog.Spec, opts LineHistoryOptions) iter.Seq2[LineHistoryEntry, error] {
	return func(yield func(LineHistoryEntry, error) bool) {
		if err := lineHistory(ctx, r, rev, specs, opts, yield); err != nil {
			yield(LineHistoryEntry{}, err)
		}
	}
}

func lineHistory(ctx context.Context, r *repo.Repository, rev string, specs []linelog.Spec, opts LineHistoryOptions, yield func(LineHistoryEntry, error) bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	rc, err := openRepoContext(r)
	if err != nil {
		return err
	}
	defer func() { _ = rc.close() }()

	cleaned := make([]linelog.Spec, 0, len(specs))
	for _, spec := range specs {
		clean, err := cleanRepoPath(spec.Path)
		if err != nil {
			return err
		}
		cleaned = append(cleaned, linelog.Spec{Range: spec.Range, Path: clean})
	}
	start, err := resolveCommittish(rc, rev)
	if err != nil {
		return err
	}
	shallow, err := r.Shallow()
	if err != nil {
		return err
	}
	funcNames, err := lineRangeFuncNames(r)
	if err != nil {
		return err
	}
	logOpts := linelog.Options{NoRenames: opts.NoRenames || renamesOff(r), Shallow: shallow, FuncNames: funcNames}
	if opts.Progress != nil {
		logOpts.Progress = func(p linelog.Progress) { opts.Progress(p.Done, p.Total) }
	}
	for entry, err := range linelog.Log(ctx, mergeStore{db: rc.db}, start, cleaned, logOpts) {
		if err != nil {
			return err
		}
		if !yield(lineHistoryEntry(entry), nil) {
			return nil
		}
	}
	return nil
}

func renamesOff(r *repo.Repository) bool {
	value, set := r.Config().Get("diff.renames")
	if !set {
		return false
	}
	on, err := config.ParseBool(value)
	return err == nil && !on
}

func lineHistoryEntry(entry *linelog.Entry) LineHistoryEntry {
	return LineHistoryEntry{
		Commit:    entry.Commit.ID,
		Parents:   entry.Commit.Parents,
		Author:    entry.Commit.Author,
		Committer: entry.Commit.Committer,
		Subject:   firstLine(entry.Commit.Message),
		Files:     entry.Files,
	}
}

func LineHistorySpecs(ctx context.Context, r *repo.Repository, rev string, selection LineSelection) ([]linelog.Spec, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	clean, err := cleanRepoPath(selection.Path)
	if err != nil {
		return nil, err
	}
	if selection.First < 1 {
		return []linelog.Spec{{Range: ",", Path: clean}}, nil
	}
	if selection.Shown == nil {
		return []linelog.Spec{linelog.LinesSpec(clean, selection.First, max(selection.First, selection.Last))}, nil
	}
	rc, err := openRepoContext(r)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.close() }()
	start, err := resolveCommittish(rc, rev)
	if err != nil {
		return nil, err
	}
	stored, err := linelog.FileAt(mergeStore{db: rc.db}, start, clean)
	if err != nil {
		return nil, err
	}
	var specs []linelog.Spec
	for _, span := range linelog.MapSpans(stored, selection.Shown, []linelog.Span{{Start: selection.First - 1, End: max(selection.First, selection.Last)}}) {
		specs = append(specs, linelog.LinesSpec(clean, span.Start+1, span.End))
	}
	return specs, nil
}
