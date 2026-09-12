package ops

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/repo"
)

type ReflogOptions struct {
	MaxCount int
}

type ReflogRecord struct {
	Ref       refs.Name
	Index     int
	Old       hash.ObjectID
	New       hash.ObjectID
	Committer object.Signature
	Message   string
	Subject   string
}

func (r ReflogRecord) Selector() string {
	return r.Ref.Short() + "@{" + fmt.Sprint(r.Index) + "}"
}

func Reflog(ctx context.Context, r *repo.Repository, name string, opts ReflogOptions) ([]ReflogRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	rc, err := openRepoContext(r)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.close() }()

	ref, err := reflogRef(rc, name)
	if err != nil {
		return nil, err
	}
	var records []ReflogRecord
	for entry, err := range rc.refs.Reflog(ref) {
		if err != nil {
			return nil, err
		}
		records = append(records, ReflogRecord{
			Ref:       ref,
			Old:       entry.Old,
			New:       entry.New,
			Committer: entry.Committer,
			Message:   entry.Message,
			Subject:   reflogSubject(entry.Message),
		})
	}
	slices.Reverse(records)
	for i := range records {
		records[i].Index = i
	}
	if opts.MaxCount > 0 && len(records) > opts.MaxCount {
		records = records[:opts.MaxCount]
	}
	return records, nil
}

func reflogSubject(message string) string {
	_, subject, found := strings.Cut(message, ": ")
	if !found {
		return message
	}
	return subject
}

func reflogRef(rc *repoContext, name string) (refs.Name, error) {
	switch {
	case name == "" || name == oursLabel:
		return refs.HEAD, nil
	case strings.HasPrefix(name, "refs/"):
		return refs.Name(name), nil
	}
	candidates := []refs.Name{refs.BranchName(name), refs.Name(refs.RemotesPrefix + name), refs.TagName(name)}
	for _, candidate := range candidates {
		switch _, err := refsLookup(rc.refs, candidate); {
		case err == nil:
			return candidate, nil
		case !errors.Is(err, refs.ErrNotFound):
			return "", err
		}
	}
	return "", fmt.Errorf("%w: %s", ErrTargetNotFound, name)
}
