package ops

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/gitcore/revision"
)

type CreateTagOptions struct {
	Message string
	Force   bool
	When    time.Time
	Tagger  *object.Signature
}

type TagResult struct {
	Name   string
	Target hash.ObjectID
	Tag    hash.ObjectID
}

func (t TagResult) Annotated() bool { return !t.Tag.IsZero() }

type TagInfo struct {
	Name      string
	Target    hash.ObjectID
	Commit    hash.ObjectID
	Annotated bool
	Subject   string
	Tagger    *object.Signature
}

func validateTagName(name string) error {
	if err := refs.TagName(name).Validate(); err != nil {
		return fmt.Errorf("%w: %s: %w", ErrInvalidTagName, name, err)
	}
	return nil
}

func CreateTag(ctx context.Context, r *repo.Repository, name, target string, opts CreateTagOptions) (TagResult, error) {
	if err := ctx.Err(); err != nil {
		return TagResult{}, err
	}
	if err := validateTagName(name); err != nil {
		return TagResult{}, err
	}
	rc, err := openRepoContext(r)
	if err != nil {
		return TagResult{}, err
	}
	defer func() { _ = rc.close() }()

	pointee, err := resolveTagTarget(rc, target)
	if err != nil {
		return TagResult{}, err
	}
	result := TagResult{Name: name, Target: pointee}
	if opts.Message != "" {
		if result.Tag, err = writeTagObject(rc, name, pointee, opts); err != nil {
			return TagResult{}, err
		}
	}
	return result, publishTag(rc, result, opts.Force)
}

func resolveTagTarget(rc *repoContext, target string) (hash.ObjectID, error) {
	if target == "" {
		target = oursLabel
	}
	rev, err := revision.Parse(target, revision.Context{Objects: rc.db, Refs: rc.refs, Head: refs.HEAD})
	if err != nil {
		return hash.Zero, fmt.Errorf("%w: %s: %w", ErrTargetNotFound, target, err)
	}
	return rev.ID, nil
}

func writeTagObject(rc *repoContext, name string, target hash.ObjectID, opts CreateTagOptions) (hash.ObjectID, error) {
	kind, _, err := dbGet(rc.db, target)
	if err != nil {
		return hash.Zero, err
	}
	tagger := opts.Tagger
	if tagger == nil {
		if err := rc.requireIdentity(); err != nil {
			return hash.Zero, err
		}
		when := opts.When
		if when.IsZero() {
			when = time.Now()
		}
		signature := rc.sig
		signature.When = when
		tagger = new(signature)
	}
	tag := &object.Tag{Object: target, ObjectType: kind, Name: name, Tagger: tagger, Message: normalizeMessage(opts.Message)}
	return dbPutObject(rc.db, tag)
}

func publishTag(rc *repoContext, result TagResult, force bool) error {
	ref := refs.TagName(result.Name)
	stored := result.Target
	if result.Annotated() {
		stored = result.Tag
	}
	tx := rc.refs.Begin()
	tx.SetMessage("tag: updating " + ref.String())
	var err error
	if force {
		err = txSet(tx, ref, stored)
	} else {
		err = txUpdate(tx, ref, stored, hash.Zero)
	}
	if err != nil {
		tx.Rollback()
		return err
	}
	if err := txCommit(tx); err != nil {
		if errors.Is(err, refs.ErrOldValueMismatch) {
			return fmt.Errorf("%w: %s", ErrTagExists, result.Name)
		}
		return err
	}
	return nil
}

func DeleteTag(ctx context.Context, r *repo.Repository, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateTagName(name); err != nil {
		return err
	}
	rc, err := openRepoContext(r)
	if err != nil {
		return err
	}
	defer func() { _ = rc.close() }()

	ref := refs.TagName(name)
	stored, err := refsLookup(rc.refs, ref)
	if errors.Is(err, refs.ErrNotFound) {
		return fmt.Errorf("%w: %s", ErrTagNotFound, name)
	}
	if err != nil {
		return err
	}
	tx := rc.refs.Begin()
	if err := txDelete(tx, ref, stored.Target); err != nil {
		tx.Rollback()
		return err
	}
	return txCommit(tx)
}

func Tags(ctx context.Context, r *repo.Repository) ([]TagInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	rc, err := openRepoContext(r)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.close() }()

	var tags []TagInfo
	for ref, err := range rc.refs.Prefix(refs.TagsPrefix) {
		if err != nil {
			return nil, err
		}
		info, err := tagInfo(rc, ref)
		if err != nil {
			return nil, err
		}
		tags = append(tags, info)
	}
	slices.SortFunc(tags, func(a, b TagInfo) int { return strings.Compare(a.Name, b.Name) })
	return tags, nil
}

func tagInfo(rc *repoContext, ref refs.Ref) (TagInfo, error) {
	info := TagInfo{Name: strings.TrimPrefix(string(ref.Name), refs.TagsPrefix), Target: ref.Target, Commit: ref.Target}
	kind, data, err := dbGet(rc.db, ref.Target)
	if err != nil {
		return TagInfo{}, err
	}
	if kind != object.TypeTag {
		return info, nil
	}
	tag, err := object.ParseTag(data)
	if err != nil {
		return TagInfo{}, err
	}
	info.Annotated, info.Commit, info.Tagger = true, tag.Object, tag.Tagger
	body, _ := tag.SplitMessage()
	info.Subject = firstLine(strings.TrimRight(body, "\n"))
	return info, nil
}
