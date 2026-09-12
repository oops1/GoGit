package ops

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/refs"
)

var tagTime = time.Unix(1700010000, 0).UTC()

func tagOptions(message string) CreateTagOptions {
	return CreateTagOptions{Message: message, When: tagTime}
}

func (r *testRepo) tagTarget(name string) hash.ObjectID {
	r.t.Helper()
	ref, err := r.refs().Lookup(refs.TagName(name))
	if err != nil {
		r.t.Fatalf("Lookup returned error %v", err)
	}
	return ref.Target
}

func (r *testRepo) storedTag(name string) *object.Tag {
	r.t.Helper()
	kind, data, err := r.db().Get(r.tagTarget(name))
	if err != nil {
		r.t.Fatalf("Get returned error %v", err)
	}
	if kind != object.TypeTag {
		r.t.Fatalf("%s is a %s", name, kind)
	}
	tag, err := object.ParseTag(data)
	if err != nil {
		r.t.Fatalf("ParseTag returned error %v", err)
	}
	return tag
}

func TestALightweightTagPointsStraightAtTheCommit(t *testing.T) {
	tr := newTestRepo(t)
	head := tr.commitFiles("base", map[string]string{"f": "f\n"})

	result, err := CreateTag(t.Context(), tr.repo, "v1", "", tagOptions(""))
	if err != nil {
		t.Fatalf("CreateTag returned error %v", err)
	}

	if result.Annotated() || result.Target != head || tr.tagTarget("v1") != head {
		t.Fatalf("result = %+v", result)
	}
}

func TestAnAnnotatedTagCarriesTheMessageAndTagger(t *testing.T) {
	tr := newTestRepo(t)
	head := tr.commitFiles("base", map[string]string{"f": "f\n"})

	result, err := CreateTag(t.Context(), tr.repo, "v1", "HEAD", tagOptions("first release\n\nwith a body"))
	if err != nil {
		t.Fatalf("CreateTag returned error %v", err)
	}

	if !result.Annotated() || result.Target != head || tr.tagTarget("v1") != result.Tag {
		t.Fatalf("result = %+v", result)
	}
	tag := tr.storedTag("v1")
	if tag.Object != head || tag.ObjectType != object.TypeCommit || tag.Name != "v1" {
		t.Fatalf("tag = %+v", tag)
	}
	if tag.Message != "first release\n\nwith a body\n" || tag.Tagger.Name != "ann" || !tag.Tagger.When.Equal(tagTime) {
		t.Fatalf("tag = %+v, tagger = %+v", tag, tag.Tagger)
	}
}

func TestAnAnnotatedTagCanPointAtAnotherTag(t *testing.T) {
	tr := newTestRepo(t)
	tr.commitFiles("base", map[string]string{"f": "f\n"})
	first, err := CreateTag(t.Context(), tr.repo, "v1", "", tagOptions("first"))
	if err != nil {
		t.Fatalf("CreateTag returned error %v", err)
	}

	second, err := CreateTag(t.Context(), tr.repo, "v2", "v1", tagOptions("second"))
	if err != nil {
		t.Fatalf("CreateTag returned error %v", err)
	}

	if second.Target != first.Tag || tr.storedTag("v2").ObjectType != object.TypeTag {
		t.Fatalf("second = %+v", second)
	}
}

func TestATagWithAnExplicitTaggerSkipsTheConfiguredIdentity(t *testing.T) {
	tr := newTestRepo(t)
	head := tr.commitFiles("base", map[string]string{"f": "f\n"})
	opts := tagOptions("tagged")
	opts.Tagger = new(testSignature())

	result, err := CreateTag(t.Context(), tr.repo, "v1", head.String(), opts)
	if err != nil {
		t.Fatalf("CreateTag returned error %v", err)
	}

	if tr.storedTag("v1").Tagger.Email != testSignature().Email || result.Target != head {
		t.Fatalf("tagger = %+v", tr.storedTag("v1").Tagger)
	}
}

func TestAnAnnotatedTagNeedsAnIdentity(t *testing.T) {
	tr := newTestRepo(t)
	head := tr.commitFiles("base", map[string]string{"f": "f\n"})
	tr.appendConfig("[user]\n\tname =\n\temail =\n")
	tr.repo = tr.reopen()

	if _, err := CreateTag(t.Context(), tr.repo, "v1", head.String(), tagOptions("tagged")); !errors.Is(err, ErrMissingIdentity) {
		t.Fatalf("err = %v", err)
	}
}

func TestATagTakesTheCurrentTimeWhenNoneIsGiven(t *testing.T) {
	tr := newTestRepo(t)
	tr.commitFiles("base", map[string]string{"f": "f\n"})
	before := time.Now().Add(-time.Minute)

	if _, err := CreateTag(t.Context(), tr.repo, "v1", "", CreateTagOptions{Message: "now"}); err != nil {
		t.Fatalf("CreateTag returned error %v", err)
	}

	if when := tr.storedTag("v1").Tagger.When; when.Before(before) {
		t.Fatalf("tagged at %v", when)
	}
}

func TestATagRefusesToReplaceAnExistingOneUnlessForced(t *testing.T) {
	tr := newTestRepo(t)
	base := tr.commitFiles("base", map[string]string{"f": "f\n"})
	next := tr.commitFiles("next", map[string]string{"f": "next\n"})
	if _, err := CreateTag(t.Context(), tr.repo, "v1", base.String(), tagOptions("")); err != nil {
		t.Fatalf("CreateTag returned error %v", err)
	}

	if _, err := CreateTag(t.Context(), tr.repo, "v1", next.String(), tagOptions("")); !errors.Is(err, ErrTagExists) {
		t.Fatalf("err = %v", err)
	}
	opts := tagOptions("")
	opts.Force = true
	if _, err := CreateTag(t.Context(), tr.repo, "v1", next.String(), opts); err != nil {
		t.Fatalf("forced CreateTag returned error %v", err)
	}

	if tr.tagTarget("v1") != next {
		t.Fatal("the forced tag did not move")
	}
}

func TestATagNameIsValidated(t *testing.T) {
	tr := newTestRepo(t)
	tr.commitFiles("base", map[string]string{"f": "f\n"})

	if _, err := CreateTag(t.Context(), tr.repo, "bad..name", "", tagOptions("")); !errors.Is(err, ErrInvalidTagName) {
		t.Fatalf("create: err = %v", err)
	}
	if err := DeleteTag(t.Context(), tr.repo, "bad..name"); !errors.Is(err, ErrInvalidTagName) {
		t.Fatalf("delete: err = %v", err)
	}
}

func TestATagOfAnUnknownTargetFails(t *testing.T) {
	tr := newTestRepo(t)
	tr.commitFiles("base", map[string]string{"f": "f\n"})

	if _, err := CreateTag(t.Context(), tr.repo, "v1", "nope", tagOptions("")); !errors.Is(err, ErrTargetNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestDeletingATagRemovesTheRef(t *testing.T) {
	tr := newTestRepo(t)
	tr.commitFiles("base", map[string]string{"f": "f\n"})
	if _, err := CreateTag(t.Context(), tr.repo, "v1", "", tagOptions("tagged")); err != nil {
		t.Fatalf("CreateTag returned error %v", err)
	}

	if err := DeleteTag(t.Context(), tr.repo, "v1"); err != nil {
		t.Fatalf("DeleteTag returned error %v", err)
	}

	if _, err := tr.refs().Lookup(refs.TagName("v1")); !errors.Is(err, refs.ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
	if err := DeleteTag(t.Context(), tr.repo, "v1"); !errors.Is(err, ErrTagNotFound) {
		t.Fatalf("second delete: err = %v", err)
	}
}

func TestTagsListBothKindsInOrder(t *testing.T) {
	tr := newTestRepo(t)
	base := tr.commitFiles("base", map[string]string{"f": "f\n"})
	next := tr.commitFiles("next", map[string]string{"f": "next\n"})
	if _, err := CreateTag(t.Context(), tr.repo, "v2", next.String(), tagOptions("second release\n\nbody")); err != nil {
		t.Fatalf("CreateTag returned error %v", err)
	}
	if _, err := CreateTag(t.Context(), tr.repo, "v1", base.String(), tagOptions("")); err != nil {
		t.Fatalf("CreateTag returned error %v", err)
	}

	tags, err := Tags(t.Context(), tr.repo)
	if err != nil {
		t.Fatalf("Tags returned error %v", err)
	}

	if len(tags) != 2 || tags[0].Name != "v1" || tags[1].Name != "v2" {
		t.Fatalf("tags = %+v", tags)
	}
	if tags[0].Annotated || tags[0].Commit != base || tags[0].Subject != "" || tags[0].Tagger != nil {
		t.Fatalf("lightweight = %+v", tags[0])
	}
	if !tags[1].Annotated || tags[1].Commit != next || tags[1].Subject != "second release" || tags[1].Tagger.Name != "ann" {
		t.Fatalf("annotated = %+v", tags[1])
	}
}

func TestTagsReportsABrokenTagObject(t *testing.T) {
	tr := newTestRepo(t)
	tr.commitFiles("base", map[string]string{"f": "f\n"})
	broken, err := tr.db().Put(object.TypeTag, []byte("not a tag\n"))
	if err != nil {
		t.Fatalf("Put returned error %v", err)
	}
	tx := tr.refs().Begin()
	if err := tx.Set(refs.TagName("v1"), broken); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	if _, err := Tags(t.Context(), tr.repo); err == nil {
		t.Fatal("a broken tag object was accepted")
	}
}

func TestTagOperationsHonourACancelledContext(t *testing.T) {
	tr := newTestRepo(t)
	tr.commitFiles("base", map[string]string{"f": "f\n"})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, err := CreateTag(ctx, tr.repo, "v1", "", tagOptions("")); !errors.Is(err, context.Canceled) {
		t.Fatalf("create: err = %v", err)
	}
	if err := DeleteTag(ctx, tr.repo, "v1"); !errors.Is(err, context.Canceled) {
		t.Fatalf("delete: err = %v", err)
	}
	if _, err := Tags(ctx, tr.repo); !errors.Is(err, context.Canceled) {
		t.Fatalf("list: err = %v", err)
	}
}

func TestTagsReportsATagObjectItCannotParse(t *testing.T) {
	tr := newTestRepo(t)
	tr.commitFiles("base", map[string]string{"f": "f\n"})
	if _, err := CreateTag(t.Context(), tr.repo, "v1", "", tagOptions("tagged")); err != nil {
		t.Fatalf("CreateTag returned error %v", err)
	}
	prev := dbGet
	dbGet = func(*odb.DB, hash.ObjectID) (object.Type, []byte, error) {
		return object.TypeTag, []byte("nonsense\n"), nil
	}
	t.Cleanup(func() { dbGet = prev })

	if _, err := Tags(t.Context(), tr.repo); err == nil {
		t.Fatal("an unparsable tag object was accepted")
	}
}
